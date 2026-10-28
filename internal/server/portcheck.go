package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/system"
)

// Port self-test: for every port the server uses, and for anything else listening on the
// Raspberry Pi, it checks whether the service runs, whether the Pi's firewall lets it in and
// whether it is reachable from the internet, then says what to open or close. The internet
// side is checked by portchecker.io, which connects to the public address from outside (a
// connection from the Pi itself would depend on the router's NAT loopback).

const portCheckerURL = "https://portchecker.io/api/v1/query"

type portResult struct {
	Port       int    `json:"port"`
	External   int    `json:"external"` // port number on the router's public side
	Name       string `json:"name"`
	Ours       bool   `json:"ours"`
	Listening  bool   `json:"listening"`
	Firewall   string `json:"firewall"` // open | blocked | unknown
	Internet   string `json:"internet"` // open | closed | unknown
	Want       string `json:"want"`     // open | closed | any: what the internet side should be
	Level      string `json:"level"`    // ok | info | warn | error
	Suggestion string `json:"suggestion"`
}

type portCheckResult struct {
	Mode          string       `json:"mode"`
	PublicIP      string       `json:"publicIp"`
	LanIP         string       `json:"lanIp"`
	Firewall      string       `json:"firewall"` // managed | disabled | unknown
	ExternalError string       `json:"externalError"`
	DNS           *dnsCheck    `json:"dns"`
	Ports         []portResult `json:"ports"`
	Hints         []string     `json:"hints"`
	Checked       time.Time    `json:"checked"`
}

type dnsCheck struct {
	Host    string   `json:"host"`
	Addrs   []string `json:"addrs"`
	Matches bool     `json:"matches"`
}

type portChecker struct {
	mu   sync.Mutex
	last *portCheckResult
}

// knownPort describes a port the server opens.
type knownPort struct {
	port int
	ext  int
	name string
	want string
	why  string // why it has to be reachable from the internet
}

// addrPort is the port of a listen address such as ":8090", or def.
func addrPort(addr string, def int) int {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	return def
}

func serverPorts(cfg config.Config, httpPort, httpsPort int) []knownPort {
	internet := accessMode(cfg) != "local"
	want := func(needed bool) string {
		switch {
		case !internet:
			return "closed"
		case needed:
			return "open"
		}
		return "any"
	}
	ps := []knownPort{
		// The router maps the standard web ports to the panel's ports (see PortForwarding in the panel).
		{httpPort, 80, "Panel WWW (HTTP)", want(true), "strona dołączania i weryfikacja Let’s Encrypt"},
		{httpsPort, 443, "Panel WWW (HTTPS)", want(cfg.LetsEncrypt.Enabled), "strona dołączania przez HTTPS"},
		{cfg.Ports.CoTTLS, cfg.Ports.CoTTLS, "Strumień danych TAK (TLS)", want(true), "połączenie aplikacji TAK"},
		{cfg.Ports.Marti, cfg.Ports.Marti, "API serwera TAK", want(true), "pliki, misje i paczki danych"},
		{cfg.Ports.Enroll, cfg.Ports.Enroll, "Rejestracja urządzeń", want(true), "rejestracja kodem QR w aplikacji"},
	}
	if cfg.Ports.CoTTCPEnabled {
		ps = append(ps, knownPort{cfg.Ports.CoTTCP, cfg.Ports.CoTTCP, "Strumień TAK bez szyfrowania", "closed", ""})
	}
	return ps
}

func (a *App) runPortCheck(ctx context.Context) *portCheckResult {
	cfg := a.cfg.Get()
	mode := accessMode(cfg)
	res := &portCheckResult{Mode: mode, Checked: time.Now().UTC()}
	if addrs := system.Addresses(); len(addrs) > 0 {
		res.LanIP = addrs[0].IP
	}
	fwState, fwPorts := system.FirewallState()
	res.Firewall = fwState
	listening := system.ListeningTCP()

	known := serverPorts(cfg, addrPort(a.panelHTTPAddr(), cfg.Ports.HTTP), addrPort(a.panelHTTPSAddr(), cfg.Ports.HTTPS))
	ports := make([]portResult, 0, len(known)+len(listening))
	for _, k := range known {
		ports = append(ports, portResult{Port: k.port, External: k.ext, Name: k.name, Ours: true, Want: k.want})
	}
	for _, p := range listening {
		if slices.ContainsFunc(ports, func(r portResult) bool { return r.Port == p }) {
			continue
		}
		name, want := "Inna usługa na Raspberry Pi", "closed"
		switch p {
		case 22:
			name = "SSH (zdalna konsola)"
		case 53:
			name = "DNS hotspotu"
		}
		ports = append(ports, portResult{Port: p, External: p, Name: name, Want: want, Listening: true})
	}
	for i := range ports {
		p := &ports[i]
		if p.Ours {
			p.Listening = slices.Contains(listening, p.Port) || dialLocal(p.Port)
		}
		switch fwState {
		case "managed":
			p.Firewall = "blocked"
			if slices.Contains(fwPorts, p.Port) {
				p.Firewall = "open"
			}
		default:
			p.Firewall = "unknown"
		}
		p.Internet = "unknown"
	}

	ip, err := detectPublicIP(ctx)
	switch {
	case err != nil:
		res.ExternalError = "Nie udało się ustalić publicznego adresu IP (brak internetu?), więc nie sprawdzono portów z zewnątrz."
	case !IsPublicIP(ip):
		res.PublicIP = ip
		res.ExternalError = fmt.Sprintf("Wykryty adres %s nie jest publiczny (CGNAT) — z internetu nie da się połączyć z żadnym portem.", ip)
	default:
		res.PublicIP = ip
		nums := make([]int, 0, len(ports))
		for _, p := range ports {
			nums = append(nums, p.External)
		}
		open, err := externalPorts(ctx, ip, nums)
		if err != nil {
			res.ExternalError = "Zewnętrzny test portów (portchecker.io) nie odpowiedział: " + err.Error()
		} else {
			for i := range ports {
				if v, ok := open[ports[i].External]; ok {
					ports[i].Internet = map[bool]string{true: "open", false: "closed"}[v]
				}
			}
		}
	}

	switch mode {
	case "domain":
		addrs := lookupA(ctx, cfg.ServerName)
		res.DNS = &dnsCheck{Host: cfg.ServerName, Addrs: addrs, Matches: res.PublicIP != "" && slices.Contains(addrs, res.PublicIP)}
		if res.PublicIP != "" && !res.DNS.Matches {
			if len(addrs) == 0 {
				res.Hints = append(res.Hints, fmt.Sprintf("Domena %s nie ma rekordu A. Ustaw go na %s albo włącz automatyczną aktualizację DNS.", cfg.ServerName, res.PublicIP))
			} else {
				res.Hints = append(res.Hints, fmt.Sprintf("Domena %s wskazuje na %s, a publiczny adres serwera to %s. Popraw rekord A albo włącz automatyczną aktualizację DNS.", cfg.ServerName, strings.Join(addrs, ", "), res.PublicIP))
			}
		}
	case "ip":
		if res.PublicIP != "" && res.PublicIP != cfg.ServerName {
			res.Hints = append(res.Hints, fmt.Sprintf("Publiczny adres routera to teraz %s, a w ustawieniach jest %s. Telefony nie połączą się — zmień adres serwera albo przejdź na domenę z automatyczną aktualizacją DNS.", res.PublicIP, cfg.ServerName))
		}
	}

	for i := range ports {
		suggest(&ports[i], res, known)
	}
	if mode != "local" && res.ExternalError == "" {
		reachable := false
		for _, p := range ports {
			if p.Ours && p.Want == "open" && p.Internet == "open" {
				reachable = true
			}
		}
		if !reachable {
			res.Hints = append(res.Hints, "Żaden port serwera nie jest osiągalny z internetu. Sprawdź przekierowanie portów na routerze. Jeśli jest ustawione, a nadal nic nie działa, dostawca internetu może dawać adres za CGNAT (częste w sieciach komórkowych i światłowodach osiedlowych) — wtedy poproś go o publiczny adres IP.")
		}
	}
	res.Ports = ports
	return res
}

func suggest(p *portResult, res *portCheckResult, known []knownPort) {
	fwd := fmt.Sprintf("Przekieruj na routerze port %d TCP", p.External)
	if res.LanIP != "" {
		fwd += " na " + net.JoinHostPort(res.LanIP, strconv.Itoa(p.Port))
	}
	why := ""
	for _, k := range known {
		if k.port == p.Port {
			why = k.why
		}
	}
	switch {
	case p.Ours && !p.Listening:
		p.Level, p.Suggestion = "error", "Usługa nie działa. Sprawdź log zdarzeń albo uruchom serwer ponownie."
	case p.Want == "open" && p.Firewall == "blocked":
		p.Level, p.Suggestion = "error", "Zapora Raspberry Pi blokuje ten port. Uruchom usługę ponownie (sudo systemctl restart chameleontak) — zapora ustawi się sama."
	case p.Want == "open" && p.Internet == "closed":
		p.Level, p.Suggestion = "error", fwd+" — potrzebny: "+why+"."
	case p.Want == "open" && p.Internet == "open":
		p.Level, p.Suggestion = "ok", "Osiągalny z internetu."
	case p.Want == "any" && p.Internet == "open":
		p.Level, p.Suggestion = "info", "Panel jest dostępny z internetu. Jeśli go nie potrzebujesz poza siecią lokalną, zamknij przekierowanie albo ogranicz panel do wybranych adresów."
	case p.Want == "any":
		p.Level, p.Suggestion = "ok", "Niepotrzebny z internetu."
	case p.Want == "closed" && p.Internet == "open":
		p.Level = "warn"
		switch {
		case p.External == 22:
			p.Suggestion = "SSH jest wystawione do internetu — boty będą zgadywać hasło. Usuń przekierowanie portu 22 na routerze (do konsoli łącz się z sieci lokalnej albo przez VPN)."
		case p.Ours && res.Mode == "local":
			p.Suggestion = "Serwer działa w trybie sieci lokalnej, a port jest otwarty z internetu. Usuń przekierowanie na routerze albo przełącz tryb dostępu na internet."
		case p.Ours:
			p.Suggestion = "Ruch na tym porcie nie jest szyfrowany. Usuń przekierowanie na routerze albo wyłącz strumień bez szyfrowania w ustawieniach portów."
		default:
			p.Suggestion = "Nieznana usługa jest osiągalna z internetu. Usuń przekierowanie tego portu na routerze."
		}
	case p.Want == "closed" && !p.Ours && p.Firewall == "unknown":
		p.Level, p.Suggestion = "info", "Usługa słucha w sieci. Zapora ChameleonTAK jest wyłączona — upewnij się, że Twoja zapora jej nie wystawia."
	default:
		p.Level, p.Suggestion = "ok", "Zamknięty od strony internetu."
	}
}

func dialLocal(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// externalPorts asks portchecker.io which of the ports accept connections at ip.
func externalPorts(ctx context.Context, ip string, ports []int) (map[int]bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"host": ip, "ports": ports})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, portCheckerURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ChameleonTAK")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("brak połączenia")
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var out struct {
		Error bool   `json:"error"`
		Msg   string `json:"msg"`
		Check []struct {
			Port   int  `json:"port"`
			Status bool `json:"status"`
		} `json:"check"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("odpowiedź %d", resp.StatusCode)
	}
	if out.Error || resp.StatusCode != 200 {
		if out.Msg != "" {
			return nil, errors.New(out.Msg)
		}
		return nil, fmt.Errorf("odpowiedź %d", resp.StatusCode)
	}
	m := map[int]bool{}
	for _, c := range out.Check {
		m[c.Port] = c.Status
	}
	return m, nil
}

// ---------- API ----------

// hPortCheck returns the last result unless it is older than two minutes or a fresh one is
// asked for (?fresh=1); the external service should not be hammered on every page view.
func (a *App) hPortCheck(w http.ResponseWriter, r *http.Request) {
	a.ports.mu.Lock()
	defer a.ports.mu.Unlock()
	if last := a.ports.last; last != nil && r.URL.Query().Get("fresh") == "" && time.Since(last.Checked) < 2*time.Minute {
		writeJSON(w, 200, last)
		return
	}
	res := a.runPortCheck(r.Context())
	a.ports.last = res
	writeJSON(w, 200, res)
}
