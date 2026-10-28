package server

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/cot"
	"chameleontak/internal/pki"
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
	"chameleontak/internal/system"
)

type streamClient = stream.Client

func (a *App) recordClientCert(cert *x509.Certificate, userID, deviceID int64) error {
	return a.store.AddCert(&store.CertRecord{
		Serial: pki.SerialHex(cert), Kind: "client", CN: cert.Subject.CommonName, UserID: userID, DeviceID: deviceID,
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, PEM: string(pki.CertPEM(cert)),
	})
}

// ---------- dashboard / system ----------

func (a *App) hDashboard(w http.ResponseWriter, r *http.Request) {
	users, _ := a.store.ListUsers()
	devices, _ := a.store.ListDevices(0)
	groups, _ := a.store.ListGroups()
	files, _ := a.store.ListFiles()
	missions, _ := a.store.ListMissions()
	writeJSON(w, 200, map[string]any{
		"online":    a.router.Count(),
		"users":     len(users),
		"devices":   len(devices),
		"groups":    len(groups),
		"files":     len(files),
		"missions":  len(missions),
		"uptimeSec": int(time.Since(a.started).Seconds()),
		"listeners": a.listenerStates(),
		"system":    a.systemInfo(),
	})
}

func (a *App) systemInfo() map[string]any {
	info := system.Collect(a.opts.DataDir)
	cfg := a.cfg.Get()
	return map[string]any{
		"hostname": info.Hostname, "isPi": info.IsPi, "model": info.Model, "cpuTempC": info.CPUTempC,
		"load1": info.Load1, "memTotal": info.MemTotal, "memUsed": info.MemUsed, "diskTotal": info.DiskTotal,
		"diskUsed": info.DiskUsed, "addresses": info.Addresses, "time": info.Time, "ntpSynced": info.NTPSynced,
		"os": info.OS, "helper": a.helper.Available(),
		"network": map[string]any{"mode": cfg.Network.Mode, "ssid": networkSSID(cfg)},
	}
}

func networkSSID(cfg config.Config) string {
	if cfg.Network.Mode == "hotspot" {
		return cfg.Network.HotspotSSID
	}
	return cfg.Network.WifiSSID
}

func (a *App) hSystem(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.systemInfo()) }

func (a *App) hReboot(w http.ResponseWriter, r *http.Request) {
	if !a.helper.Available() {
		fail(w, invalid("Restart systemu jest dostępny tylko na Raspberry Pi / Linuksie."))
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
	go func() {
		time.Sleep(time.Second)
		a.pos.Flush()
		if err := a.helper.Run("", "reboot"); err != nil {
			a.log.Error("reboot", "err", err)
		}
	}()
}

func (a *App) hNetwork(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode            string `json:"mode"`
		WifiSSID        string `json:"wifiSsid"`
		WifiPassword    string `json:"wifiPassword"`
		HotspotSSID     string `json:"hotspotSsid"`
		HotspotPassword string `json:"hotspotPassword"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if !a.helper.Available() {
		fail(w, invalid("Konfiguracja sieci jest dostępna tylko na Raspberry Pi / Linuksie."))
		return
	}
	if !system.WifiAvailable() && (in.Mode == "hotspot" || in.WifiSSID != "") {
		fail(w, invalid("To urządzenie nie ma Wi-Fi zarządzanego przez NetworkManager."))
		return
	}
	switch in.Mode {
	case "hotspot":
		if in.HotspotSSID == "" || len(in.HotspotPassword) < 8 {
			fail(w, invalid("Podaj nazwę hotspotu i hasło (min. 8 znaków)."))
			return
		}
	case "lan":
	default:
		fail(w, invalid("Tryb sieci: lan albo hotspot."))
		return
	}
	_, err := a.cfg.Update(func(c *config.Config) error {
		c.Network.Mode = in.Mode
		if in.Mode == "hotspot" {
			c.Network.HotspotSSID, c.Network.HotspotPassword = in.HotspotSSID, in.HotspotPassword
		} else if in.WifiSSID != "" {
			c.Network.WifiSSID = in.WifiSSID
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Zmiana sieci zostanie zastosowana za kilka sekund — połączenie z panelem może zostać przerwane."})
	go func() {
		time.Sleep(3 * time.Second)
		if err := a.applyNetwork(in.Mode, in.WifiSSID, in.WifiPassword, in.HotspotSSID, in.HotspotPassword); err != nil {
			a.log.Error("zmiana sieci", "err", err)
		}
	}()
}

func (a *App) applyNetwork(mode, wifiSSID, wifiPass, hsSSID, hsPass string) error {
	if mode == "hotspot" {
		return a.helper.Run(hsPass, "hotspot", hsSSID)
	}
	if err := a.helper.Run("", "hotspot-off"); err != nil {
		a.log.Warn("hotspot-off", "err", err)
	}
	if wifiSSID != "" {
		return a.helper.Run(wifiPass, "wifi-client", wifiSSID)
	}
	return nil
}

// applyFirewall opens exactly the ports the server uses.
func (a *App) applyFirewall() error {
	if a.parent != nil {
		return a.parent.applyFirewall()
	}
	if !a.helper.Available() {
		return system.ErrUnsupported
	}
	cfg := a.cfg.Get()
	ports := []int{cfg.Ports.HTTP, cfg.Ports.HTTPS}
	if !cfg.SetupDone {
		// Setup mode: only the panel (the helper adds 22 itself while SSH is running).
		return a.helper.Run("", "firewall", fmt.Sprintf("%d,%d", cfg.Ports.HTTP, cfg.Ports.HTTPS))
	}
	ports = append(ports, cfg.Ports.CoTTLS, cfg.Ports.Marti, cfg.Ports.Enroll)
	if cfg.Ports.CoTTCPEnabled {
		ports = append(ports, cfg.Ports.CoTTCP)
	}
	if cfg.SSHEnabled {
		ports = append(ports, 22)
	}
	for _, s := range a.servers()[1:] {
		ports = append(ports, takPorts(s.cfg.Get().Ports)...)
	}
	if cfg.Video.Enabled {
		ports = append(ports, system.VideoRTMPPort, system.VideoRTSPPort)
	}
	var s []string
	for _, p := range ports {
		s = append(s, fmt.Sprint(p))
	}
	return a.helper.Run("", "firewall", strings.Join(s, ","))
}

// ---------- settings ----------

type settingsJSON struct {
	ServerName         string             `json:"serverName"`
	DisplayName        string             `json:"displayName"`
	Organization       string             `json:"organization"`
	Ports              config.Ports       `json:"ports"`
	Platforms          []string           `json:"platforms"`
	CertValidityDays   int                `json:"certValidityDays"`
	TokenValidityHours int                `json:"tokenValidityHours"`
	CertPassword       string             `json:"certPassword"`
	ForceHTTPS         bool               `json:"forceHttps"`
	LetsEncrypt        config.LetsEncrypt `json:"letsEncrypt"`
	Profile            config.Profile     `json:"profile"`
	MapTileURL         string             `json:"mapTileUrl"`
	Network            map[string]any     `json:"network"`
	ServerID           string             `json:"serverId"`
	Main               bool               `json:"main"`
	ZeroTier           zerotierView       `json:"zerotier"`
	DDNS               config.DDNS        `json:"ddns"`
	PanelAccess        config.PanelAccess `json:"panelAccess"`
}

func settingsView(c config.Config) settingsJSON {
	return settingsJSON{
		ServerName: c.ServerName, DisplayName: c.DisplayName, Organization: c.Organization, Ports: c.Ports,
		Platforms: c.Platforms, CertValidityDays: c.CertValidityDays, TokenValidityHours: c.TokenValidityHours,
		CertPassword: c.CertPassword, ForceHTTPS: c.ForceHTTPS, LetsEncrypt: c.LetsEncrypt, Profile: c.Profile,
		MapTileURL: c.MapTileURL, DDNS: c.DDNS, PanelAccess: c.PanelAccess,
		Network: map[string]any{"mode": c.Network.Mode, "staticIp": c.Network.StaticIP, "wifiSsid": c.Network.WifiSSID, "hotspotSsid": c.Network.HotspotSSID},
	}
}

func (a *App) hGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.settingsOut(a.cfg.Get()))
}

// settingsOut is settingsView with the server it belongs to.
func (a *App) settingsOut(c config.Config) settingsJSON {
	v := settingsView(c)
	v.ServerID, v.Main = a.ID(), a.parent == nil
	v.ZeroTier = zerotierView{Network: a.host().cfg.Get().ZeroTier.Network, IPs: system.ZeroTierIPs()}
	return v
}

// zerotierView tells the panel whether the server address is a ZeroTier one.
type zerotierView struct {
	Network string   `json:"network"`
	IPs     []string `json:"ips"`
}

func validatePorts(p config.Ports) error {
	all := map[string]int{"CoT TLS": p.CoTTLS, "Marti": p.Marti, "Enrollment": p.Enroll, "HTTP": p.HTTP, "HTTPS": p.HTTPS}
	if p.CoTTCPEnabled {
		all["CoT TCP"] = p.CoTTCP
	}
	seen := map[int]string{}
	for name, v := range all {
		if v < 1 || v > 65535 {
			return invalid(fmt.Sprintf("Port %s musi być w zakresie 1–65535.", name))
		}
		if other, ok := seen[v]; ok {
			return invalid(fmt.Sprintf("Porty %s i %s nie mogą być takie same (%d).", other, name, v))
		}
		seen[v] = name
	}
	return nil
}

func validateServerName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return invalid("Podaj nazwę serwera (adres IP lub domenę).")
	}
	if net.ParseIP(s) != nil {
		return nil
	}
	if len(s) > 253 {
		return invalid("Nazwa serwera jest za długa.")
	}
	for _, label := range strings.Split(s, ".") {
		if !reHostLabel.MatchString(label) {
			return invalid("Nazwa serwera musi być adresem IP albo poprawną nazwą domenową (np. tak.example.com).")
		}
	}
	return nil
}

// validateLetsEncrypt checks that Let's Encrypt can issue for the server name.
func validateLetsEncrypt(serverName string, le config.LetsEncrypt) error {
	if !le.Enabled {
		return nil
	}
	if accessMode(config.Config{ServerName: serverName}) == "local" {
		return invalid("Let's Encrypt wymaga publicznej domeny albo publicznego adresu IP (nie adresu z sieci lokalnej).")
	}
	if le.Email != "" && !strings.Contains(le.Email, "@") {
		return invalid("Niepoprawny adres e-mail dla Let's Encrypt.")
	}
	return nil
}

func (a *App) hPutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	old := a.cfg.Get()
	// Decode the partial update on top of the current settings.
	cur := settingsView(old)
	if err := remarshal(in, &cur); err != nil {
		fail(w, invalid("Niepoprawne ustawienia."))
		return
	}
	cur.ServerName = strings.TrimSpace(cur.ServerName)
	if a.parent != nil {
		// The panel, its ports and Let's Encrypt belong to the main server.
		cur.Ports.HTTP, cur.Ports.HTTPS, cur.ForceHTTPS, cur.LetsEncrypt = old.Ports.HTTP, old.Ports.HTTPS, old.ForceHTTPS, old.LetsEncrypt
		cur.PanelAccess = old.PanelAccess
	}
	if err := validateServerName(cur.ServerName); err != nil {
		fail(w, err)
		return
	}
	if err := validatePorts(cur.Ports); err != nil {
		fail(w, err)
		return
	}
	if err := a.checkPortConflicts(cur.Ports, a); err != nil {
		fail(w, err)
		return
	}
	for _, p := range cur.Platforms {
		if !config.ValidPlatform(p) {
			fail(w, invalid("Nieznana platforma "+p))
			return
		}
	}
	if len(cur.Platforms) == 0 {
		fail(w, invalid("Włącz co najmniej jedną platformę."))
		return
	}
	if cur.CertValidityDays < 1 || cur.CertValidityDays > 3650 {
		fail(w, invalid("Ważność certyfikatów: 1–3650 dni."))
		return
	}
	if cur.TokenValidityHours < 1 || cur.TokenValidityHours > 24*90 {
		fail(w, invalid("Ważność linków: 1–2160 godzin."))
		return
	}
	if len(cur.CertPassword) < 4 {
		fail(w, invalid("Hasło paczek certyfikatów musi mieć min. 4 znaki."))
		return
	}
	if err := validateLetsEncrypt(cur.ServerName, cur.LetsEncrypt); err != nil {
		fail(w, err)
		return
	}
	cur.DDNS.Provider, cur.DDNS.Token, cur.DDNS.URL = strings.TrimSpace(cur.DDNS.Provider), strings.TrimSpace(cur.DDNS.Token), strings.TrimSpace(cur.DDNS.URL)
	if err := validateDDNS(cur.ServerName, cur.DDNS); err != nil {
		fail(w, err)
		return
	}
	allow, err := normalizeAccessList(cur.PanelAccess.Allow)
	if err != nil {
		fail(w, err)
		return
	}
	cur.PanelAccess.Allow = allow
	if cur.PanelAccess.Restrict {
		if len(allow) == 0 {
			fail(w, invalid("Dodaj co najmniej jeden adres, z którego panel ma być dostępny."))
			return
		}
		// Refuse a list that would lock out the browser saving it.
		if !accessAllowed(cur.PanelAccess, clientIP(r), system.NeighborMAC) {
			fail(w, invalid(fmt.Sprintf("Twój obecny adres (%s) nie jest na liście — po zapisaniu straciłbyś dostęp do panelu. Dodaj go najpierw.", clientIP(r))))
			return
		}
	}
	newCfg, err := a.cfg.Update(func(c *config.Config) error {
		c.ServerName, c.DisplayName, c.Organization = cur.ServerName, strings.TrimSpace(cur.DisplayName), strings.TrimSpace(cur.Organization)
		c.Ports, c.Platforms = cur.Ports, cur.Platforms
		c.CertValidityDays, c.TokenValidityHours, c.CertPassword = cur.CertValidityDays, cur.TokenValidityHours, cur.CertPassword
		if !reflect.DeepEqual(normalizeProfile(c.Profile), normalizeProfile(cur.Profile)) {
			c.ProfileUpdatedAt = time.Now().UTC()
		}
		c.ForceHTTPS, c.LetsEncrypt, c.Profile, c.MapTileURL = cur.ForceHTTPS, cur.LetsEncrypt, cur.Profile, strings.TrimSpace(cur.MapTileURL)
		c.DDNS, c.PanelAccess = cur.DDNS, cur.PanelAccess
		if c.DisplayName == "" {
			c.DisplayName = "ChameleonTAK"
		}
		if c.Profile.ExtraPrefs == nil {
			c.Profile.ExtraPrefs = map[string]string{}
		}
		return nil
	})
	if err != nil {
		fail(w, err)
		return
	}
	regen := newCfg.ServerName != old.ServerName
	if regen && newCfg.SetupDone {
		if err := a.ensureServerCert(true); err != nil {
			fail(w, err)
			return
		}
	}
	portsChanged := newCfg.Ports != old.Ports
	restart := newCfg.SetupDone && (portsChanged || newCfg.LetsEncrypt != old.LetsEncrypt || newCfg.ServerName != old.ServerName)
	if newCfg.LetsEncrypt != old.LetsEncrypt || newCfg.ServerName != old.ServerName {
		a.public.wake()
	}
	if newCfg.DDNS != old.DDNS || newCfg.ServerName != old.ServerName {
		a.ddns.wake()
	}
	writeJSON(w, 200, map[string]any{"settings": a.settingsOut(newCfg), "restarted": restart, "certsRegenerated": regen})
	if restart {
		go func() {
			time.Sleep(500 * time.Millisecond)
			a.startServices()
			if newCfg.Ports.HTTP != old.Ports.HTTP && a.opts.HTTPAddr == "" {
				a.restartHTTP()
			}
			if a.parent == nil && (newCfg.Ports.HTTP != old.Ports.HTTP || newCfg.Ports.HTTPS != old.Ports.HTTPS || newCfg.ForceHTTPS != old.ForceHTTPS) {
				for _, s := range a.servers()[1:] {
					s.syncPanelPorts()
				}
			}
			if portsChanged {
				if err := a.applyFirewall(); err != nil && err != system.ErrUnsupported {
					a.log.Error("firewall", "err", err)
				}
			}
		}()
	}
}

// ---------- groups ----------

func (a *App) hListGroups(w http.ResponseWriter, r *http.Request) {
	gs, err := a.store.ListGroups()
	if err != nil {
		fail(w, err)
		return
	}
	out := []map[string]any{}
	for _, g := range gs {
		out = append(out, map[string]any{"id": g.ID, "name": g.Name, "description": g.Description, "members": g.Members, "createdAt": g.CreatedAt})
	}
	writeJSON(w, 200, out)
}

func (a *App) hCreateGroup(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Description string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 64 || strings.HasPrefix(in.Name, "__") {
		fail(w, invalid("Nazwa grupy: 1–64 znaki, nie może zaczynać się od __."))
		return
	}
	g := &store.Group{Name: in.Name, Description: strings.TrimSpace(in.Description)}
	if err := a.store.CreateGroup(g); err != nil {
		fail(w, invalid("Grupa o tej nazwie już istnieje."))
		return
	}
	writeJSON(w, 201, map[string]any{"id": g.ID, "name": g.Name, "description": g.Description, "members": 0, "createdAt": g.CreatedAt})
}

func (a *App) hUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	g, err := a.store.GetGroup(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	oldName := ""
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 64 || strings.HasPrefix(n, "__") {
			fail(w, invalid("Niepoprawna nazwa grupy."))
			return
		}
		oldName = g.Name
		g.Name = n
	}
	if in.Description != nil {
		g.Description = strings.TrimSpace(*in.Description)
	}
	if err := a.store.UpdateGroup(g); err != nil {
		fail(w, invalid("Grupa o tej nazwie już istnieje."))
		return
	}
	if oldName != "" && !strings.EqualFold(oldName, g.Name) {
		if err := a.store.RenameGroupInMissions(oldName, g.Name); err != nil {
			a.log.Error("zmiana nazwy grupy w misjach", "err", err)
		}
	}
	a.refreshAllGroups()
	writeJSON(w, 200, map[string]any{"id": g.ID, "name": g.Name, "description": g.Description})
}

func (a *App) hDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	if err := a.store.DeleteGroup(id); err != nil {
		fail(w, err)
		return
	}
	a.refreshAllGroups()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// refreshAllGroups pushes current memberships to every live connection.
func (a *App) refreshAllGroups() {
	users, err := a.store.ListUsers()
	if err != nil {
		return
	}
	for _, u := range users {
		a.router.UpdateUser(u.ID, u.Groups, u.Disabled)
	}
}

// ---------- live data ----------

func (a *App) hClients(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.router.Clients())
}

func (a *App) hKick(w http.ResponseWriter, r *http.Request) {
	if !a.router.Kick(r.PathValue("id")) {
		fail(w, store.ErrNotFound)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hPositions(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.pos.List()) }

func (a *App) hChat(w http.ResponseWriter, r *http.Request) {
	msgs, err := a.store.ListChat(200)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, msgs)
}

const serverChatRoom = "All Chat Rooms"

func (a *App) serverUID() string { return "ChameleonTAK-" + a.cfg.Get().NodeID[:8] }

func (a *App) hSendChat(w http.ResponseWriter, r *http.Request) {
	var in struct{ Text, To string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > 2000 {
		fail(w, invalid("Wiadomość nie może być pusta (maks. 2000 znaków)."))
		return
	}
	cfg := a.cfg.Get()
	sender := cfg.DisplayName
	msgID := randomToken()[:32]
	to := strings.TrimSpace(in.To)
	if to == "" || to == serverChatRoom {
		a.router.Broadcast(cot.ServerChat(a.serverUID(), sender, serverChatRoom, "", msgID, in.Text), nil)
		to = serverChatRoom
	} else {
		// The panel sends the recipient's UID; a callsign is accepted too.
		target := a.router.ClientByUID(to)
		if target == nil {
			target = a.router.ClientByCallsign(to)
		}
		if target == nil {
			fail(w, invalid("Odbiorca "+to+" nie jest połączony."))
			return
		}
		to = target.Callsign()
		target.Send(cot.ServerChat(a.serverUID(), sender, to, target.UID(), msgID, in.Text))
	}
	msg := &store.ChatMessage{Time: time.Now().UTC(), From: sender, FromUID: a.serverUID(), To: to, Text: in.Text}
	if err := a.store.AddChat(msg); err != nil {
		fail(w, err)
		return
	}
	a.hub.Publish("chat", msg)
	writeJSON(w, 201, msg)
}

// ---------- certificates ----------

func (a *App) hListCerts(w http.ResponseWriter, r *http.Request) {
	certs, err := a.store.ListCerts()
	if err != nil {
		fail(w, err)
		return
	}
	out := []map[string]any{}
	if ca := a.pki.CACert(); ca != nil {
		out = append(out, map[string]any{"serial": pki.SerialHex(ca), "cn": ca.Subject.CommonName, "kind": "ca", "userId": 0, "deviceId": 0,
			"notBefore": ca.NotBefore, "notAfter": ca.NotAfter, "revoked": false})
	}
	for _, c := range certs {
		out = append(out, map[string]any{"serial": c.Serial, "cn": c.CN, "kind": c.Kind, "userId": c.UserID, "deviceId": c.DeviceID,
			"notBefore": c.NotBefore, "notAfter": c.NotAfter, "revoked": !c.RevokedAt.IsZero()})
	}
	writeJSON(w, 200, out)
}

func (a *App) hRevokeCert(w http.ResponseWriter, r *http.Request) {
	serial := strings.ToUpper(r.PathValue("serial"))
	c, err := a.store.GetCert(serial)
	if err != nil {
		fail(w, err)
		return
	}
	if c.Kind != "client" {
		fail(w, invalid("Można unieważniać tylko certyfikaty klientów."))
		return
	}
	if err := a.store.RevokeCert(serial); err != nil {
		fail(w, err)
		return
	}
	a.router.DisconnectWhere(func(cl *streamClient) bool { return cl.Identity.CertSerial == serial })
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// normalizeProfile treats a nil and an empty extraPrefs map as equal when comparing profiles.
func normalizeProfile(p config.Profile) config.Profile {
	if p.ExtraPrefs == nil {
		p.ExtraPrefs = map[string]string{}
	}
	return p
}
