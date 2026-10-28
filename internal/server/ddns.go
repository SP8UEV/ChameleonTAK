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
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/config"
)

// Dynamic DNS: with a domain, home routers usually get a public address that changes now and
// then. The updater looks up the public address every few minutes and, when the domain's A
// record does not match it, tells the DNS provider the new address (DuckDNS, Cloudflare or any
// provider with an update URL: No-IP, Dynu, FreeDNS, OVH DynHost...).

const ddnsInterval = 5 * time.Minute

var ddnsProviders = []string{"duckdns", "cloudflare", "url"}

type ddnsStatus struct {
	State      string     `json:"state"` // off | pending | ok | error
	Provider   string     `json:"provider"`
	Domain     string     `json:"domain"`
	PublicIP   string     `json:"publicIp"`
	DNSAddrs   []string   `json:"dnsAddrs"`
	LastUpdate *time.Time `json:"lastUpdate"`
	Checked    *time.Time `json:"checked"`
	Message    string     `json:"message"`
}

type ddnsUpdater struct {
	app  *App
	kick chan struct{}
	run1 sync.Mutex // one step at a time (loop and "update now")

	mu      sync.Mutex
	status  ddnsStatus
	sentIP  string // address last sent to the provider, for this provider and domain
	sentKey string
	sentAt  time.Time
}

func newDDNS(a *App) *ddnsUpdater {
	return &ddnsUpdater{app: a, kick: make(chan struct{}, 1), status: ddnsStatus{State: "off"}}
}

func (d *ddnsUpdater) wake() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

func (d *ddnsUpdater) Status() ddnsStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.status
	s.DNSAddrs = slices.Clone(s.DNSAddrs)
	return s
}

func (d *ddnsUpdater) run(ctx context.Context) {
	for {
		d.step(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-d.kick:
		case <-time.After(ddnsInterval):
		}
	}
}

// step checks the domain against the public address and updates the record when needed (or
// always, with force).
func (d *ddnsUpdater) step(ctx context.Context, force bool) ddnsStatus {
	d.run1.Lock()
	defer d.run1.Unlock()
	cfg := d.app.cfg.Get()
	if !cfg.SetupDone || cfg.DDNS.Provider == "" || accessMode(cfg) != "domain" {
		d.set(ddnsStatus{State: "off"})
		return d.Status()
	}
	domain := cfg.ServerName
	now := time.Now()
	s := ddnsStatus{Provider: cfg.DDNS.Provider, Domain: domain, Checked: &now}
	d.mu.Lock()
	key := cfg.DDNS.Provider + "|" + domain + "|" + cfg.DDNS.Token + "|" + cfg.DDNS.URL
	if d.sentKey != key {
		d.sentKey, d.sentIP, d.sentAt = key, "", time.Time{}
	}
	sentIP, sentAt := d.sentIP, d.sentAt
	if !sentAt.IsZero() {
		s.LastUpdate = &sentAt
	}
	d.mu.Unlock()

	ip, err := detectPublicIP(ctx)
	if err != nil {
		s.State, s.Message = "error", "Nie udało się ustalić publicznego adresu IP (brak internetu?)."
		d.set(s)
		return d.Status()
	}
	s.PublicIP = ip
	if !IsPublicIP(ip) {
		s.State, s.Message = "error", fmt.Sprintf("Wykryty adres %s nie jest publiczny (CGNAT) — z internetu nie da się do niego połączyć.", ip)
		d.set(s)
		return d.Status()
	}
	s.DNSAddrs = lookupA(ctx, domain)
	inDNS := slices.Contains(s.DNSAddrs, ip)
	// Send when forced, when the address changed, when DNS still shows something else some time
	// after the last update (propagation takes a moment) and once a day, so free providers
	// such as No-IP do not expire the host.
	due := force || ip != sentIP || (!inDNS && time.Since(sentAt) > 10*time.Minute) || time.Since(sentAt) > 24*time.Hour
	if !due {
		s.State = "ok"
		if !inDNS {
			s.State, s.Message = "pending", "Adres wysłany do dostawcy DNS — czekam, aż zmiana się rozejdzie."
		}
		d.set(s)
		return d.Status()
	}
	msg, err := ddnsSend(ctx, cfg.DDNS, domain, ip)
	if err != nil {
		d.app.log.Warn("DDNS", "dostawca", cfg.DDNS.Provider, "domena", domain, "err", err)
		s.State, s.Message = "error", err.Error()
		d.set(s)
		return d.Status()
	}
	sent := time.Now()
	d.mu.Lock()
	d.sentIP, d.sentAt = ip, sent
	d.mu.Unlock()
	if ip != sentIP {
		d.app.log.Info("DDNS: zaktualizowano adres domeny", "domena", domain, "ip", ip)
	}
	s.LastUpdate, s.State, s.Message = &sent, "ok", msg
	if !inDNS {
		s.State = "pending"
		s.Message = strings.TrimSpace(msg + " Zmiana w DNS może potrwać kilka minut.")
	}
	d.set(s)
	return d.Status()
}

func (d *ddnsUpdater) set(s ddnsStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.status = s
}

// lookupA resolves the domain's IPv4 addresses through public resolvers first, so a router's
// DNS cache does not hide a fresh change, then through the system resolver.
func lookupA(ctx context.Context, domain string) []string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolvers := []*net.Resolver{}
	for _, srv := range []string{"1.1.1.1:53", "8.8.8.8:53"} {
		resolvers = append(resolvers, &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var dl net.Dialer
			return dl.DialContext(ctx, network, srv)
		}})
	}
	resolvers = append(resolvers, net.DefaultResolver)
	for _, r := range resolvers {
		ips, err := r.LookupIP(ctx, "ip4", domain)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return nil
			}
			continue
		}
		out := make([]string, 0, len(ips))
		for _, ip := range ips {
			out = append(out, ip.String())
		}
		slices.Sort(out)
		return out
	}
	return nil
}

func validateDDNS(serverName string, d config.DDNS) error {
	if d.Provider == "" {
		return nil
	}
	if !slices.Contains(ddnsProviders, d.Provider) {
		return invalid("Nieznany dostawca dynamicznego DNS.")
	}
	if accessMode(config.Config{ServerName: serverName}) != "domain" {
		return invalid("Automatyczna aktualizacja DNS działa tylko z własną domeną.")
	}
	switch d.Provider {
	case "duckdns":
		if duckDNSName(serverName) == "" {
			return invalid("Dla DuckDNS nazwa serwera musi kończyć się na .duckdns.org (np. mojtak.duckdns.org).")
		}
		if strings.TrimSpace(d.Token) == "" {
			return invalid("Podaj token DuckDNS (widoczny po zalogowaniu na duckdns.org).")
		}
	case "cloudflare":
		if strings.TrimSpace(d.Token) == "" {
			return invalid("Podaj token API Cloudflare z uprawnieniem Zone → DNS → Edit.")
		}
	case "url":
		u, err := url.Parse(strings.TrimSpace(d.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return invalid("Adres aktualizacji musi zaczynać się od https:// (np. adres podany przez dostawcę DNS).")
		}
	}
	return nil
}

// duckDNSName is the DuckDNS subdomain of a name under duckdns.org ("" otherwise).
func duckDNSName(domain string) string {
	rest, ok := strings.CutSuffix(strings.ToLower(domain), ".duckdns.org")
	if !ok || rest == "" {
		return ""
	}
	labels := strings.Split(rest, ".")
	return labels[len(labels)-1]
}

var ddnsClient = &http.Client{Timeout: 15 * time.Second}

// ddnsGet fetches u and returns the body. Errors never include the URL, which carries the token.
func ddnsGet(ctx context.Context, method, u, auth string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, errors.New("niepoprawny adres aktualizacji")
	}
	req.Header.Set("User-Agent", "ChameleonTAK-DDNS/1.0")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ddnsClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return 0, nil, fmt.Errorf("brak połączenia z %s: %v", req.URL.Hostname(), err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, b, nil
}

// ddnsSend points the domain at ip and returns a short confirmation.
func ddnsSend(ctx context.Context, d config.DDNS, domain, ip string) (string, error) {
	switch d.Provider {
	case "duckdns":
		q := url.Values{"domains": {duckDNSName(domain)}, "token": {strings.TrimSpace(d.Token)}, "ip": {ip}}
		code, b, err := ddnsGet(ctx, http.MethodGet, "https://www.duckdns.org/update?"+q.Encode(), "", nil)
		if err != nil {
			return "", err
		}
		if code != 200 || !strings.HasPrefix(strings.TrimSpace(string(b)), "OK") {
			return "", errors.New("DuckDNS odrzucił aktualizację — sprawdź token i nazwę subdomeny")
		}
		return "DuckDNS przyjął adres.", nil
	case "cloudflare":
		return cloudflareUpdate(ctx, strings.TrimSpace(d.Token), domain, ip)
	case "url":
		u := strings.NewReplacer("{ip}", url.QueryEscape(ip), "{domain}", url.QueryEscape(domain)).Replace(strings.TrimSpace(d.URL))
		code, b, err := ddnsGet(ctx, http.MethodGet, u, "", nil)
		if err != nil {
			return "", err
		}
		reply := strings.TrimSpace(string(b))
		if len(reply) > 120 {
			reply = reply[:120] + "…"
		}
		low := strings.ToLower(reply)
		for _, bad := range []string{"badauth", "nohost", "notfqdn", "abuse", "badagent", "911", "error", "fail", "ko"} {
			if low == bad || strings.HasPrefix(low, bad+" ") || strings.HasPrefix(low, bad+":") {
				return "", fmt.Errorf("dostawca DNS odpowiedział: %s", reply)
			}
		}
		if code < 200 || code > 299 {
			return "", fmt.Errorf("dostawca DNS odpowiedział kodem %d: %s", code, reply)
		}
		if reply == "" {
			return "Dostawca DNS przyjął adres.", nil
		}
		return "Odpowiedź dostawcy: " + reply, nil
	}
	return "", errors.New("nieznany dostawca dynamicznego DNS")
}

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

type cfResponse struct {
	Success bool                       `json:"success"`
	Errors  []struct{ Message string } `json:"errors"`
	Result  json.RawMessage            `json:"result"`
}

func cloudflareCall(ctx context.Context, method, path, token string, body any, out any) error {
	code, b, err := ddnsGet(ctx, method, cloudflareAPI+path, token, body)
	if err != nil {
		return err
	}
	var r cfResponse
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("Cloudflare: nieczytelna odpowiedź (kod %d)", code)
	}
	if !r.Success {
		msg := fmt.Sprintf("kod %d", code)
		if len(r.Errors) > 0 {
			msg = r.Errors[0].Message
		}
		return fmt.Errorf("Cloudflare: %s", msg)
	}
	if out != nil {
		return json.Unmarshal(r.Result, out)
	}
	return nil
}

// cloudflareUpdate sets the domain's A record. The record is never proxied: Cloudflare's proxy
// passes only HTTP(S), not TAK's TLS streams.
func cloudflareUpdate(ctx context.Context, token, domain, ip string) (string, error) {
	type zone struct{ ID string }
	var zoneID string
	labels := strings.Split(domain, ".")
	for i := 0; i < len(labels)-1 && zoneID == ""; i++ {
		var zs []zone
		if err := cloudflareCall(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(strings.Join(labels[i:], ".")), token, nil, &zs); err != nil {
			return "", err
		}
		if len(zs) > 0 {
			zoneID = zs[0].ID
		}
	}
	if zoneID == "" {
		return "", fmt.Errorf("Cloudflare: token nie ma dostępu do strefy domeny %s", domain)
	}
	var recs []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Proxied bool   `json:"proxied"`
	}
	q := url.Values{"type": {"A"}, "name": {domain}}
	if err := cloudflareCall(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records?"+q.Encode(), token, nil, &recs); err != nil {
		return "", err
	}
	rec := map[string]any{"type": "A", "name": domain, "content": ip, "ttl": 60, "proxied": false}
	if len(recs) == 0 {
		if err := cloudflareCall(ctx, http.MethodPost, "/zones/"+zoneID+"/dns_records", token, rec, nil); err != nil {
			return "", err
		}
		return "Utworzono rekord A w Cloudflare.", nil
	}
	if recs[0].Content == ip && !recs[0].Proxied {
		return "Rekord A w Cloudflare jest aktualny.", nil
	}
	if err := cloudflareCall(ctx, http.MethodPatch, "/zones/"+zoneID+"/dns_records/"+recs[0].ID, token, rec, nil); err != nil {
		return "", err
	}
	if recs[0].Proxied {
		return "Zaktualizowano rekord A w Cloudflare i wyłączono proxy (TAK go nie obsługuje).", nil
	}
	return "Zaktualizowano rekord A w Cloudflare.", nil
}

// ---------- API ----------

func (a *App) hDDNSUpdate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.ddns.step(r.Context(), true))
}
