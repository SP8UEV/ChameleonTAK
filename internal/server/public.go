package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"

	"chameleontak/internal/config"
)

// Public access: phones reach the server from the internet under a public domain or a public IP
// address (port forwarding on the router). With Let's Encrypt enabled the panel (443) and the
// enrollment port (8446) get a publicly trusted certificate, so phones trust the join page, the
// package download and in-app QR enrollment. 8089 and 8443 always keep the server's own CA.
//
// Domains use autocert. autocert cannot issue for IP addresses, so the public-IP mode orders the
// certificate itself with x/crypto/acme: IP identifiers require the "shortlived" profile (about
// 6 days), validation is HTTP-01 on port 80, and the certificate is renewed at half its lifetime.

const ipCertProfile = "shortlived"

// accessMode is how phones reach the server: "domain", "ip" (public address) or "local".
func accessMode(cfg config.Config) string {
	switch {
	case IsPublicDomain(cfg.ServerName):
		return "domain"
	case IsPublicIP(cfg.ServerName):
		return "ip"
	}
	return "local"
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// IsPublicIP reports whether name is an IP address reachable from the internet (not private,
// loopback, link-local or carrier-grade NAT).
func IsPublicIP(name string) bool {
	ip := net.ParseIP(name)
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !cgnat.Contains(ip)
}

// certStatus is the state of the public certificate shown in the panel.
type certStatus struct {
	State       string     `json:"state"` // off | pending | ok | error
	Host        string     `json:"host"`
	NotAfter    *time.Time `json:"notAfter"`
	Error       string     `json:"error"`
	NextAttempt *time.Time `json:"nextAttempt"`
}

type publicCerts struct {
	app  *App
	kick chan struct{}

	mu     sync.Mutex
	ipCert *tls.Certificate  // certificate for a public IP address (Leaf set)
	tokens map[string]string // HTTP-01 token -> key authorization
	status certStatus
	fails  int
	retry  time.Time
}

func newPublicCerts(a *App) *publicCerts {
	p := &publicCerts{app: a, kick: make(chan struct{}, 1), tokens: map[string]string{}, status: certStatus{State: "off"}}
	if c, err := tls.LoadX509KeyPair(p.path("cert.pem"), p.path("key.pem")); err == nil && c.Leaf != nil {
		p.ipCert = &c
	}
	return p
}

func (p *publicCerts) path(name string) string { return p.app.DataPath("acme-ip", name) }

// wake makes the renewal loop look at the configuration again (after a settings change).
func (p *publicCerts) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *publicCerts) Status() certStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// ready reports whether a publicly trusted certificate for host is in use right now.
func (p *publicCerts) ready(host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status.Host == host && p.status.NotAfter != nil && time.Now().Before(*p.status.NotAfter) &&
		(p.status.State == "ok" || p.status.State == "error")
}

// forIP returns the certificate for the public IP address ip, or nil.
func (p *publicCerts) forIP(ip string) *tls.Certificate {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.ipCert; c != nil && time.Now().Before(c.Leaf.NotAfter) && c.Leaf.VerifyHostname(ip) == nil {
		return c
	}
	return nil
}

// challenge answers Let's Encrypt's HTTP-01 request for token.
func (p *publicCerts) challenge(token string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.tokens[token]
	return v, ok
}

func (p *publicCerts) setToken(token, keyAuth string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if keyAuth == "" {
		delete(p.tokens, token)
	} else {
		p.tokens[token] = keyAuth
	}
}

func (p *publicCerts) setStatus(s certStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s.Host != p.status.Host {
		p.fails, p.retry = 0, time.Time{}
	}
	p.status = s
}

func (p *publicCerts) run(ctx context.Context) {
	for {
		wait := p.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-p.kick:
		case <-time.After(wait):
		}
	}
}

// step brings the public certificate up to date and returns when to look again.
func (p *publicCerts) step(ctx context.Context) time.Duration {
	cfg := p.app.cfg.Get()
	if !cfg.SetupDone || !cfg.LetsEncrypt.Enabled {
		p.setStatus(certStatus{State: "off"})
		return time.Hour
	}
	switch accessMode(cfg) {
	case "domain":
		return p.stepDomain()
	case "ip":
		return p.stepIP(ctx, cfg)
	}
	p.setStatus(certStatus{State: "off"})
	return time.Hour
}

// stepDomain fetches the certificate through autocert right away instead of on the first
// connection, so the panel can show its state; autocert renews it on its own afterwards.
func (p *publicCerts) stepDomain() time.Duration {
	m, host := p.app.acmeFor()
	if m == nil {
		return time.Hour
	}
	if wait := p.backoff(host); wait > 0 {
		return wait
	}
	p.markPending(host)
	// An empty ClientHello asks for an RSA certificate, the one ATAK's Quick Connect accepts.
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: host})
	if err != nil {
		return p.failed(host, nil, err)
	}
	return p.succeeded(host, c.Leaf, 24*time.Hour)
}

func (p *publicCerts) stepIP(ctx context.Context, cfg config.Config) time.Duration {
	ip := cfg.ServerName
	if c := p.forIP(ip); c != nil && !renewalDue(c.Leaf) {
		return p.succeeded(ip, c.Leaf, time.Until(renewalTime(c.Leaf)))
	}
	if wait := p.backoff(ip); wait > 0 {
		return wait
	}
	p.markPending(ip)
	c, err := p.obtainIP(ctx, ip, cfg.LetsEncrypt.Email)
	if err != nil {
		var leaf *x509.Certificate
		if old := p.forIP(ip); old != nil {
			leaf = old.Leaf // keep serving the current certificate until it expires
		}
		return p.failed(ip, leaf, err)
	}
	p.mu.Lock()
	p.ipCert = c
	p.mu.Unlock()
	p.app.log.Info("Let's Encrypt: certyfikat dla adresu IP", "ip", ip, "do", c.Leaf.NotAfter.Format(time.DateTime))
	return p.succeeded(ip, c.Leaf, time.Until(renewalTime(c.Leaf)))
}

func (p *publicCerts) markPending(host string) {
	s := p.Status()
	if s.Host != host || s.State == "off" {
		p.setStatus(certStatus{State: "pending", Host: host})
	}
}

func (p *publicCerts) succeeded(host string, leaf *x509.Certificate, next time.Duration) time.Duration {
	var notAfter *time.Time
	if leaf != nil {
		notAfter = &leaf.NotAfter
	}
	p.setStatus(certStatus{State: "ok", Host: host, NotAfter: notAfter})
	p.mu.Lock()
	p.fails = 0
	p.mu.Unlock()
	if next < time.Minute {
		next = time.Minute
	}
	return next
}

// failed records an error and backs off (Let's Encrypt allows 5 failed validations per hour).
func (p *publicCerts) failed(host string, leaf *x509.Certificate, err error) time.Duration {
	p.app.log.Warn("Let's Encrypt", "host", host, "err", err)
	p.mu.Lock()
	p.fails++
	wait := 10 * time.Minute << min(p.fails-1, 5)
	if wait > 6*time.Hour {
		wait = 6 * time.Hour
	}
	p.retry = time.Now().Add(wait)
	retry := p.retry
	p.mu.Unlock()
	s := certStatus{State: "error", Host: host, Error: err.Error(), NextAttempt: &retry}
	if leaf != nil {
		s.NotAfter = &leaf.NotAfter
	}
	p.setStatus(s)
	return wait
}

// backoff returns how long to wait before retrying host after a failure.
func (p *publicCerts) backoff(host string) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.status.Host != host || p.status.State != "error" {
		return 0
	}
	return time.Until(p.retry)
}

func renewalTime(c *x509.Certificate) time.Time {
	return c.NotBefore.Add(c.NotAfter.Sub(c.NotBefore) / 2)
}

func renewalDue(c *x509.Certificate) bool { return time.Now().After(renewalTime(c)) }

// acmeDirectory is Let's Encrypt, or e.g. its staging server for tests.
func acmeDirectory() string {
	if d := os.Getenv("CHAMELEONTAK_ACME_DIRECTORY"); d != "" {
		return d
	}
	return acme.LetsEncryptURL
}

// obtainIP orders a certificate for a public IP address and stores it in acme-ip/.
func (p *publicCerts) obtainIP(ctx context.Context, ip, email string) (*tls.Certificate, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	key, err := p.accountKey()
	if err != nil {
		return nil, err
	}
	cl := &acme.Client{Key: key, DirectoryURL: acmeDirectory(), UserAgent: "chameleontak"}
	acct := &acme.Account{}
	if email != "" {
		acct.Contact = []string{"mailto:" + email}
	}
	if _, err := cl.Register(ctx, acct, acme.AcceptTOS); err != nil && !errors.Is(err, acme.ErrAccountAlreadyExists) {
		return nil, fmt.Errorf("rejestracja konta Let's Encrypt: %w", err)
	}
	order, err := cl.AuthorizeOrder(ctx, acme.IPIDs(ip), acme.WithOrderIssuanceProfile(ipCertProfile))
	if err != nil {
		return nil, fmt.Errorf("zamówienie certyfikatu: %w", err)
	}
	for _, u := range order.AuthzURLs {
		z, err := cl.GetAuthorization(ctx, u)
		if err != nil {
			return nil, err
		}
		if z.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range z.Challenges {
			if c.Type == "http-01" {
				chal = c
			}
		}
		if chal == nil {
			return nil, errors.New("Let's Encrypt nie zaproponował weryfikacji HTTP-01")
		}
		keyAuth, err := cl.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return nil, err
		}
		p.setToken(chal.Token, keyAuth)
		defer p.setToken(chal.Token, "")
		if _, err := cl.Accept(ctx, chal); err != nil {
			return nil, err
		}
		if _, err := cl.WaitAuthorization(ctx, z.URI); err != nil {
			return nil, fmt.Errorf("Let's Encrypt nie połączył się z http://%s (port 80 przekierowany na Raspberry?): %w", ip, err)
		}
	}
	if order, err = cl.WaitOrder(ctx, order.URI); err != nil {
		return nil, err
	}
	// RSA: its chain ends at ISRG Root X1, which every TAK client trusts.
	certKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{IPAddresses: []net.IP{net.ParseIP(ip)}}, certKey)
	if err != nil {
		return nil, err
	}
	der, _, err := cl.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("wystawienie certyfikatu: %w", err)
	}
	leaf, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, err
	}
	var chain []byte
	for _, b := range der {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: b})...)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return nil, err
	}
	if err := writePrivate(p.path("key.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		return nil, err
	}
	if err := writePrivate(p.path("cert.pem"), chain); err != nil {
		return nil, err
	}
	return &tls.Certificate{Certificate: der, PrivateKey: certKey, Leaf: leaf}, nil
}

// accountKey loads or creates the ACME account key.
func (p *publicCerts) accountKey() (*ecdsa.PrivateKey, error) {
	if b, err := os.ReadFile(p.path("account.pem")); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			return x509.ParseECPrivateKey(blk.Bytes)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return key, writePrivate(p.path("account.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// publicCert reports whether phones get a publicly trusted certificate on the panel for the
// configured server name right now.
func (a *App) publicCert() bool {
	if a.parent != nil {
		// An additional server shares the main server's certificate when it has the same address.
		return a.parent.publicCert() && strings.EqualFold(a.cfg.Get().ServerName, a.parent.cfg.Get().ServerName)
	}
	cfg := a.cfg.Get()
	return cfg.LetsEncrypt.Enabled && accessMode(cfg) != "local" && a.public.ready(cfg.ServerName)
}

// publicIPHost is the public IP address the panel and 8446 serve a Let's Encrypt certificate
// for, or "".
func (a *App) publicIPHost() string {
	cfg := a.cfg.Get()
	if cfg.SetupDone && cfg.LetsEncrypt.Enabled && accessMode(cfg) == "ip" {
		return cfg.ServerName
	}
	return ""
}

// ---------- API ----------

func (a *App) hPublicAccess(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	writeJSON(w, 200, map[string]any{
		"mode": accessMode(cfg), "host": cfg.ServerName, "letsEncrypt": cfg.LetsEncrypt.Enabled,
		"cert": a.host().public.Status(), "httpPort": cfg.Ports.HTTP, "ddns": a.ddns.Status(), "main": a.parent == nil,
	})
}

// hDetectPublicIP asks an external service which address the server's internet traffic comes from.
func (a *App) hDetectPublicIP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/setup/public-ip" && a.cfg.Get().SetupDone {
		httpError(w, http.StatusConflict, "Serwer jest już skonfigurowany.")
		return
	}
	ip, err := detectPublicIP(r.Context())
	if err != nil {
		httpError(w, http.StatusBadGateway, "Nie udało się ustalić publicznego adresu IP (brak internetu?).")
		return
	}
	writeJSON(w, 200, map[string]any{"ip": ip, "public": IsPublicIP(ip)})
}

var publicIPServices = []string{"https://api.ipify.org", "https://ipv4.icanhazip.com", "https://ifconfig.me/ip"}

func detectPublicIP(ctx context.Context) (string, error) {
	cl := &http.Client{Timeout: 6 * time.Second}
	var last error
	for _, u := range publicIPServices {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "curl/8")
		resp, err := cl.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		ip := strings.TrimSpace(string(b))
		if err == nil && resp.StatusCode == 200 && net.ParseIP(ip) != nil {
			return ip, nil
		}
		last = fmt.Errorf("%s: odpowiedź %d", u, resp.StatusCode)
	}
	return "", last
}
