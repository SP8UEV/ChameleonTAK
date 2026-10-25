package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/config"
	"chameleontak/internal/store"
	"chameleontak/internal/system"
)

type SetupRequest struct {
	ServerName   string       `json:"serverName"`
	DisplayName  string       `json:"displayName"`
	Organization string       `json:"organization"`
	Ports        config.Ports `json:"ports"`
	Admin        struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"admin"`
	Platforms   []string           `json:"platforms"`
	ClientTime  string             `json:"clientTime"`
	LetsEncrypt config.LetsEncrypt `json:"letsEncrypt"`
	Network     struct {
		Mode            string `json:"mode"`
		StaticIP        bool   `json:"staticIp"`
		WifiSSID        string `json:"wifiSsid"`
		WifiPassword    string `json:"wifiPassword"`
		HotspotSSID     string `json:"hotspotSsid"`
		HotspotPassword string `json:"hotspotPassword"`
	} `json:"network"`
	SSH struct {
		Enabled bool `json:"enabled"`
	} `json:"ssh"`
}

type setupStep struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	State   string `json:"state"` // pending | running | done | error | skipped
	Message string `json:"message"`
}

type setupJob struct {
	mu       sync.Mutex
	running  bool
	done     bool
	err      string
	panelURL string
	steps    []setupStep
}

func (j *setupJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return map[string]any{
		"running": j.running, "done": j.done, "error": j.err, "panelUrl": j.panelURL,
		"steps": append([]setupStep(nil), j.steps...),
	}
}

func (j *setupJob) set(key, state, msg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := range j.steps {
		if j.steps[i].Key == key {
			j.steps[i].State, j.steps[i].Message = state, msg
		}
	}
}

func (a *App) hSetupDefaults(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Get().SetupDone {
		httpError(w, http.StatusConflict, "Serwer jest już skonfigurowany.")
		return
	}
	host, _ := os.Hostname()
	addrs := system.Addresses()
	suggested := ""
	if len(addrs) > 0 {
		suggested = addrs[0].IP
	}
	// When the operator is connected through the setup hotspot, phones will use the hotspot too.
	if local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if ip, _, err := net.SplitHostPort(local.String()); err == nil && ip == system.HotspotGateway {
			suggested = ip
		}
	}
	if suggested == "" {
		suggested = host
	}
	writeJSON(w, 200, map[string]any{
		"suggestedServerName": suggested,
		"hostname":            host,
		"addresses":           addrs,
		"ports":               config.DefaultPorts(),
		"platforms":           config.Platforms,
		"isPi":                system.IsPi(),
		"wifiAvailable":       system.WifiAvailable(),
		"ntpSynced":           system.NTPSynced(),
		"serverTime":          time.Now().UTC(),
		"helper":              a.helper.Available(),
	})
}

func (a *App) hSetupProgress(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.setup.snapshot())
}

func (a *App) hSetup(w http.ResponseWriter, r *http.Request) {
	if a.cfg.Get().SetupDone {
		httpError(w, http.StatusConflict, "Serwer jest już skonfigurowany.")
		return
	}
	var req SetupRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	if err := a.validateSetup(&req); err != nil {
		fail(w, err)
		return
	}
	a.setup.mu.Lock()
	if a.setup.running {
		a.setup.mu.Unlock()
		httpError(w, http.StatusConflict, "Konfiguracja już trwa.")
		return
	}
	a.setup.running, a.setup.done, a.setup.err = true, false, ""
	a.setup.steps = []setupStep{
		{Key: "time", Label: "Ustawienie zegara"},
		{Key: "config", Label: "Zapis konfiguracji"},
		{Key: "ca", Label: "Urząd certyfikacji (CA)"},
		{Key: "server", Label: "Certyfikat serwera"},
		{Key: "admin", Label: "Konto administratora"},
		{Key: "ssh", Label: "Dostęp SSH"},
		{Key: "firewall", Label: "Zapora sieciowa"},
		{Key: "services", Label: "Uruchomienie usług TAK"},
		{Key: "network", Label: "Konfiguracja sieci"},
	}
	for i := range a.setup.steps {
		a.setup.steps[i].State = "pending"
	}
	a.setup.mu.Unlock()

	go func() {
		err := a.RunSetup(req, a.setup)
		a.setup.mu.Lock()
		a.setup.running = false
		if err != nil {
			a.setup.err = err.Error()
		} else {
			a.setup.done = true
			a.setup.panelURL = a.panelBaseURL()
		}
		a.setup.mu.Unlock()
	}()
	writeJSON(w, 202, map[string]bool{"started": true})
}

func (a *App) validateSetup(req *SetupRequest) error {
	req.ServerName = strings.TrimSpace(req.ServerName)
	if req.Network.Mode == "" {
		req.Network.Mode = "lan"
	}
	if req.Network.Mode == "hotspot" && req.ServerName == "" {
		req.ServerName = system.HotspotGateway
	}
	if err := validateServerName(req.ServerName); err != nil {
		return err
	}
	if req.Ports == (config.Ports{}) {
		req.Ports = config.DefaultPorts()
	}
	if err := validatePorts(req.Ports); err != nil {
		return err
	}
	req.Admin.Username = strings.TrimSpace(req.Admin.Username)
	if !reUsername.MatchString(req.Admin.Username) {
		return invalid("Login administratora: 2–32 znaki (litery, cyfry, . _ -).")
	}
	if len(req.Admin.Password) < 8 {
		return invalid("Hasło administratora musi mieć co najmniej 8 znaków.")
	}
	if len(req.Platforms) == 0 {
		return invalid("Wybierz co najmniej jedną platformę urządzeń.")
	}
	for _, p := range req.Platforms {
		if !config.ValidPlatform(p) {
			return invalid("Nieznana platforma " + p)
		}
	}
	switch req.Network.Mode {
	case "lan":
		if req.Network.WifiSSID != "" && req.Network.WifiPassword != "" && len(req.Network.WifiPassword) < 8 {
			return invalid("Hasło Wi-Fi musi mieć co najmniej 8 znaków.")
		}
	case "hotspot":
		if strings.TrimSpace(req.Network.HotspotSSID) == "" {
			req.Network.HotspotSSID = "ChameleonTAK"
		}
		if len(req.Network.HotspotPassword) < 8 || len(req.Network.HotspotPassword) > 63 {
			return invalid("Hasło hotspotu musi mieć 8–63 znaki.")
		}
	default:
		return invalid("Tryb sieci musi być \"lan\" albo \"hotspot\".")
	}
	if a.helper.Available() && !system.WifiAvailable() && (req.Network.Mode == "hotspot" || req.Network.WifiSSID != "") {
		return invalid("Ten system nie ma Wi-Fi zarządzanego przez NetworkManager — wybierz sieć LAN bez Wi-Fi.")
	}
	if req.LetsEncrypt.Enabled && !IsPublicDomain(req.ServerName) {
		return invalid("Let's Encrypt wymaga publicznej nazwy domenowej (nie adresu IP).")
	}
	return nil
}

// RunSetup performs the whole first-time configuration. It is used by the web wizard and the CLI.
func (a *App) RunSetup(req SetupRequest, job *setupJob) error {
	if job == nil {
		job = &setupJob{}
	}
	step := func(key string, fn func() (string, error)) error {
		job.set(key, "running", "")
		msg, err := fn()
		switch {
		case errors.Is(err, system.ErrUnsupported):
			job.set(key, "skipped", "Pominięto — niedostępne na tym systemie.")
			return nil
		case err != nil:
			job.set(key, "error", err.Error())
			a.log.Error("kreator", "krok", key, "err", err)
			return fmt.Errorf("%s: %w", key, err)
		case msg == "skip":
			job.set(key, "skipped", "")
		default:
			job.set(key, "done", msg)
		}
		return nil
	}

	if err := step("time", func() (string, error) {
		t, err := time.Parse(time.RFC3339Nano, req.ClientTime)
		if err != nil || system.NTPSynced() {
			return "skip", nil
		}
		if d := time.Since(t); d < 2*time.Minute && d > -2*time.Minute {
			return "Zegar jest poprawny.", nil
		}
		if err := a.helper.Run("", "set-time", t.UTC().Format(time.RFC3339)); err != nil {
			return "", err
		}
		return "Ustawiono czas z przeglądarki: " + t.Local().Format("2006-01-02 15:04"), nil
	}); err != nil {
		return err
	}

	if err := step("config", func() (string, error) {
		_, err := a.cfg.Update(func(c *config.Config) error {
			c.ServerName = req.ServerName
			if d := strings.TrimSpace(req.DisplayName); d != "" {
				c.DisplayName = d
			}
			if o := strings.TrimSpace(req.Organization); o != "" {
				c.Organization = o
			}
			c.Ports = req.Ports
			c.Platforms = req.Platforms
			c.LetsEncrypt = req.LetsEncrypt
			c.Network.Mode = req.Network.Mode
			c.Network.StaticIP = req.Network.StaticIP
			c.Network.WifiSSID = req.Network.WifiSSID
			c.Network.HotspotSSID = req.Network.HotspotSSID
			c.Network.HotspotPassword = req.Network.HotspotPassword
			return nil
		})
		return "", err
	}); err != nil {
		return err
	}
	cfg := a.cfg.Get()

	if err := step("ca", func() (string, error) {
		ca, err := a.pki.InitCA(cfg.DisplayName, cfg.Organization)
		if err != nil {
			return "", err
		}
		return "Ważny do " + ca.NotAfter.Format("2006-01-02"), nil
	}); err != nil {
		return err
	}

	if err := step("server", func() (string, error) {
		if err := a.ensureServerCert(true); err != nil {
			return "", err
		}
		return "Dla " + cfg.ServerName, nil
	}); err != nil {
		return err
	}

	if err := step("admin", func() (string, error) {
		hash, err := bcrypt.GenerateFromPassword([]byte(req.Admin.Password), bcrypt.DefaultCost)
		if err != nil {
			return "", err
		}
		u, err := a.store.GetUserByName(req.Admin.Username)
		if err == nil {
			u.PasswordHash, u.IsAdmin, u.Disabled = string(hash), true, false
			return u.Username, a.store.UpdateUser(u)
		}
		u = &store.User{
			Username: req.Admin.Username, PasswordHash: string(hash), IsAdmin: true,
			Callsign: strings.ToUpper(req.Admin.Username), Team: cfg.Profile.DefaultTeam, Role: "HQ",
		}
		return u.Username, a.store.CreateUser(u)
	}); err != nil {
		return err
	}

	if err := step("ssh", func() (string, error) {
		if !req.SSH.Enabled {
			return "skip", nil
		}
		if err := a.helper.Run(req.Admin.Password, "set-password", "tak"); err != nil {
			return "", err
		}
		if err := a.helper.Run("", "ssh", "enable"); err != nil {
			return "", err
		}
		return "Konto systemowe: tak (hasło jak administratora)", nil
	}); err != nil {
		return err
	}

	if err := step("firewall", func() (string, error) {
		return "", a.applyFirewall(req.SSH.Enabled)
	}); err != nil {
		return err
	}

	if err := step("services", func() (string, error) {
		if _, err := a.cfg.Update(func(c *config.Config) error { c.SetupDone = true; return nil }); err != nil {
			return "", err
		}
		_ = os.WriteFile(a.DataPath(".setup-done"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
		if a.cliMode {
			return "Usługi wystartują przy następnym uruchomieniu.", nil
		}
		a.startServices()
		var failed []string
		for _, s := range a.listenerStates() {
			if s.State == "error" {
				failed = append(failed, fmt.Sprintf("%s (%d): %s", s.Name, s.Port, s.Error))
			}
		}
		if len(failed) > 0 {
			return "Część usług nie wystartowała: " + strings.Join(failed, "; "), nil
		}
		return "Wszystkie usługi działają.", nil
	}); err != nil {
		return err
	}

	// Network changes may cut off the browser running the wizard, so they run last and slightly delayed.
	if !a.helper.Available() || a.cliMode {
		job.set("network", "skipped", "Pominięto — niedostępne na tym systemie.")
		return nil
	}
	job.set("network", "done", networkSummary(req))
	go func() {
		time.Sleep(8 * time.Second)
		if err := a.applySetupNetwork(req); err != nil {
			a.log.Error("konfiguracja sieci", "err", err)
		}
	}()
	return nil
}

func networkSummary(req SetupRequest) string {
	if req.Network.Mode == "hotspot" {
		return fmt.Sprintf("Za chwilę Raspberry Pi utworzy sieć Wi-Fi %q. Połącz z nią telefony i otwórz http://%s", req.Network.HotspotSSID, system.HotspotGateway)
	}
	if req.Network.WifiSSID != "" {
		return fmt.Sprintf("Za chwilę Raspberry Pi połączy się z siecią %q — panel będzie pod adresem http://%s", req.Network.WifiSSID, req.ServerName)
	}
	return "Sieć LAN bez zmian."
}

func (a *App) applySetupNetwork(req SetupRequest) error {
	if req.Network.Mode == "hotspot" {
		return a.helper.Run(req.Network.HotspotPassword, "hotspot", req.Network.HotspotSSID)
	}
	if req.Network.WifiSSID != "" {
		if err := a.helper.Run(req.Network.WifiPassword, "wifi-client", req.Network.WifiSSID); err != nil {
			return err
		}
	}
	if err := a.helper.Run("", "setup-hotspot-off"); err != nil {
		a.log.Warn("wyłączenie hotspotu kreatora", "err", err)
	}
	if req.Network.StaticIP {
		iface, cidr, gw, ok := system.CurrentIPv4(req.ServerName)
		if !ok {
			return errors.New("nie udało się ustalić bieżącego adresu do zablokowania")
		}
		return a.helper.Run("", "static-ip", iface, cidr, gw, gw)
	}
	return nil
}
