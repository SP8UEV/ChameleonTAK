// Package server wires everything together: listeners, Marti API, web panel and the setup wizard.
package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"chameleontak/internal/config"
	"chameleontak/internal/pki"
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
	"chameleontak/internal/system"
)

type Options struct {
	DataDir   string
	Version   string
	HTTPAddr  string // overrides the configured panel HTTP port
	HTTPSAddr string // overrides the configured panel HTTPS port
	Dev       bool
}

type ListenerState struct {
	Name  string `json:"name"`
	Port  int    `json:"port"`
	State string `json:"state"`
	Error string `json:"error"`
}

type App struct {
	opts    Options
	log     *slog.Logger
	store   *store.Store
	cfg     *config.Manager
	pki     *pki.PKI
	router  *stream.Router
	hub     *Hub
	pos     *Positions
	helper  *system.Helper
	limiter *limiter

	started time.Time
	setup   *setupJob
	cliMode bool

	mu        sync.Mutex
	closers   []func()
	states    []ListenerState
	httpSrv   *http.Server
	httpAddr  string
	acme      *autocert.Manager
	acmeHost  string
	missionMu sync.Mutex
	svcMu     sync.Mutex // serializes start/stop of the TAK listeners
}

func New(opts Options, log *slog.Logger) (*App, error) {
	if err := os.MkdirAll(opts.DataDir, 0o750); err != nil {
		return nil, err
	}
	for _, d := range []string{"files", "pki"} {
		if err := os.MkdirAll(filepath.Join(opts.DataDir, d), 0o750); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(filepath.Join(opts.DataDir, "chameleontak.db"))
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(st)
	if err != nil {
		return nil, err
	}
	p, err := pki.New(filepath.Join(opts.DataDir, "pki"))
	if err != nil {
		return nil, err
	}
	a := &App{
		opts:    opts,
		log:     log,
		store:   st,
		cfg:     cfg,
		pki:     p,
		hub:     NewHub(),
		helper:  system.NewHelper(),
		limiter: newLimiter(),
		started: time.Now(),
		setup:   &setupJob{},
	}
	a.pos = NewPositions(st, a.hub)
	a.router = stream.NewRouter(log, a)
	return a, nil
}

func (a *App) Close() error { return a.store.Close() }

// Run serves until ctx is cancelled.
func (a *App) Run(ctx context.Context) error {
	if err := a.startHTTP(); err != nil {
		return err
	}
	cfg := a.cfg.Get()
	if cfg.SetupDone {
		if err := a.ensureServerCert(false); err != nil {
			a.log.Error("certyfikat serwera", "err", err)
		}
		a.startServices()
	} else {
		a.log.Info("serwer nie jest skonfigurowany — otwórz panel WWW, aby uruchomić kreator", "addr", a.httpAddr)
	}

	go a.pos.Run(ctx)
	go a.maintenance(ctx)

	<-ctx.Done()
	a.stopServices()
	a.router.CloseAll()
	a.mu.Lock()
	srv := a.httpSrv
	a.mu.Unlock()
	if srv != nil {
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}
	a.pos.Flush()
	return nil
}

func (a *App) DataPath(parts ...string) string {
	return filepath.Join(append([]string{a.opts.DataDir}, parts...)...)
}

// ---------- listeners ----------

func (a *App) panelHTTPAddr() string {
	if a.opts.HTTPAddr != "" {
		return a.opts.HTTPAddr
	}
	return ":" + strconv.Itoa(a.cfg.Get().Ports.HTTP)
}

func (a *App) panelHTTPSAddr() string {
	if a.opts.HTTPSAddr != "" {
		return a.opts.HTTPSAddr
	}
	return ":" + strconv.Itoa(a.cfg.Get().Ports.HTTPS)
}

func (a *App) startHTTP() error {
	addr := a.panelHTTPAddr()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("panel HTTP %s: %w", addr, err)
	}
	base := a.panelHandler(false)
	// The ACME manager is looked up per request, so enabling Let's Encrypt or renaming the server
	// takes effect without a restart.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m, _ := a.acmeFor(); m != nil {
			m.HTTPHandler(base).ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 15 * time.Second}
	a.mu.Lock()
	old := a.httpSrv
	a.httpSrv, a.httpAddr = srv, addr
	a.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("panel HTTP", "err", err)
		}
	}()
	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = old.Shutdown(ctx)
		cancel()
	}
	a.log.Info("panel WWW (HTTP)", "addr", addr)
	return nil
}

// restartHTTP moves the plain-HTTP panel to a new port (called after the response was sent).
// The new port is bound before the old server stops, so a busy port leaves the panel reachable.
func (a *App) restartHTTP() {
	if err := a.startHTTP(); err != nil {
		a.log.Error("restart panelu HTTP — panel zostaje na starym porcie", "err", err)
	}
}

func (a *App) tlsConfig(auth tls.ClientAuthType, acme bool) *tls.Config {
	getCert := a.pki.GetCertificate
	protos := []string{"http/1.1"}
	if acme {
		protos = append(protos, acmeALPN)
		getCert = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if m, host := a.acmeFor(); m != nil && strings.EqualFold(hello.ServerName, host) {
				c, err := m.GetCertificate(hello)
				if err == nil {
					return c, nil
				}
				a.log.Warn("Let's Encrypt", "err", err)
			}
			return a.pki.GetCertificate(hello)
		}
	}
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		CipherSuites:   tlsCipherSuites,
		GetCertificate: getCert,
		ClientAuth:     auth,
		ClientCAs:      a.pki.CAPool(),
		NextProtos:     protos,
	}
}

// tlsCipherSuites (TLS 1.2 only; 1.3 suites are fixed) adds RSA key exchange to Go's defaults:
// ATAK's TLS stack uses "DEFAULT:!ECDH", which leaves no ECDHE suite for a TLS 1.2 handshake.
var tlsCipherSuites = []uint16{
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_RSA_WITH_AES_128_CBC_SHA,
	tls.TLS_RSA_WITH_AES_256_CBC_SHA,
}

// startServices (re)starts every TAK listener; calls are serialized so two quick settings saves
// cannot leak listeners.
func (a *App) startServices() {
	a.svcMu.Lock()
	defer a.svcMu.Unlock()
	a.stopServicesLocked()
	cfg := a.cfg.Get()
	var closers []func()
	var states []ListenerState
	add := func(name string, port int, err error, closer func()) {
		st := ListenerState{Name: name, Port: port, State: "running"}
		if err != nil {
			st.State, st.Error = "error", err.Error()
			a.log.Error("nie można uruchomić usługi", "name", name, "port", port, "err", err)
		} else {
			closers = append(closers, closer)
			a.log.Info("usługa uruchomiona", "name", name, "port", port)
		}
		states = append(states, st)
	}

	ln, err := a.router.ListenTLS(":"+strconv.Itoa(cfg.Ports.CoTTLS), a.tlsConfig(tls.RequireAndVerifyClientCert, false), a.authenticateStream)
	add("CoT TLS", cfg.Ports.CoTTLS, err, func() { ln.Close() })

	if cfg.Ports.CoTTCPEnabled {
		tl, err := a.router.ListenTCP(":" + strconv.Itoa(cfg.Ports.CoTTCP))
		add("CoT TCP", cfg.Ports.CoTTCP, err, func() { tl.Close() })
	}

	marti := a.martiHandler()
	stop, err := a.serveTLS(":"+strconv.Itoa(cfg.Ports.Marti), marti, a.tlsConfig(tls.VerifyClientCertIfGiven, false))
	add("Marti API", cfg.Ports.Marti, err, stop)

	stop, err = a.serveTLS(":"+strconv.Itoa(cfg.Ports.Enroll), marti, a.tlsConfig(tls.VerifyClientCertIfGiven, true))
	add("Enrollment", cfg.Ports.Enroll, err, stop)

	httpsPort := cfg.Ports.HTTPS
	if _, p, perr := net.SplitHostPort(a.panelHTTPSAddr()); perr == nil {
		httpsPort, _ = strconv.Atoi(p)
	}
	stop, err = a.serveTLS(a.panelHTTPSAddr(), a.panelHandler(true), a.tlsConfig(tls.NoClientCert, true))
	add("Panel HTTPS", httpsPort, err, stop)

	a.mu.Lock()
	a.closers, a.states = closers, states
	a.mu.Unlock()
}

func (a *App) stopServices() {
	a.svcMu.Lock()
	defer a.svcMu.Unlock()
	a.stopServicesLocked()
}

func (a *App) stopServicesLocked() {
	a.mu.Lock()
	closers := a.closers
	a.closers, a.states = nil, nil
	a.mu.Unlock()
	for _, c := range closers {
		c()
	}
}

func (a *App) listenerStates() []ListenerState {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ListenerState(nil), a.states...)
}

func (a *App) serveTLS(addr string, h http.Handler, cfg *tls.Config) (func(), error) {
	ln, err := tls.Listen("tcp", addr, cfg)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 20 * time.Second,
		ErrorLog:          slog.NewLogLogger(a.log.Handler(), slog.LevelDebug),
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.log.Error("https", "addr", addr, "err", err)
		}
	}()
	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}, nil
}

// acmeManager returns a Let's Encrypt manager when it is enabled and the server name is a public domain.
// acmeFor returns the Let's Encrypt manager and its host name when LE is enabled for a public domain.
func (a *App) acmeFor() (*autocert.Manager, string) {
	cfg := a.cfg.Get()
	if !cfg.SetupDone || !cfg.LetsEncrypt.Enabled || !IsPublicDomain(cfg.ServerName) {
		return nil, ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acme == nil || a.acmeHost != cfg.ServerName {
		a.acme = &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(a.DataPath("acme")),
			HostPolicy: autocert.HostWhitelist(cfg.ServerName),
			Email:      cfg.LetsEncrypt.Email,
		}
		a.acmeHost = cfg.ServerName
	}
	return a.acme, a.acmeHost
}

// IsPublicDomain reports whether name is a DNS name that Let's Encrypt could issue for.
func IsPublicDomain(name string) bool {
	if net.ParseIP(name) != nil || !strings.Contains(name, ".") {
		return false
	}
	n := strings.ToLower(name)
	for _, suffix := range []string{".local", ".lan", ".home", ".internal", ".localdomain", ".test", ".localhost"} {
		if strings.HasSuffix(n, suffix) {
			return false
		}
	}
	return true
}

// ---------- certificates ----------

// serverNames lists every name and address the server certificate should cover.
func (a *App) serverNames() ([]string, []net.IP) {
	cfg := a.cfg.Get()
	host, _ := os.Hostname()
	dns := []string{"localhost"}
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP(system.HotspotGateway)}
	if ip := net.ParseIP(cfg.ServerName); ip != nil {
		ips = append(ips, ip)
	} else if cfg.ServerName != "" {
		dns = append(dns, cfg.ServerName)
	}
	if host != "" {
		dns = append(dns, host, host+".local")
	}
	for _, ad := range system.Addresses() {
		ips = append(ips, net.ParseIP(ad.IP))
	}
	return dns, ips
}

// ensureServerCert reissues the server certificate when forced, missing, close to expiry or
// when it no longer covers the configured server name.
func (a *App) ensureServerCert(force bool) error {
	cfg := a.cfg.Get()
	if !a.pki.HasCA() {
		return pki.ErrNoCA
	}
	cur := a.pki.ServerX509()
	if !force && cur != nil && time.Until(cur.NotAfter) > 60*24*time.Hour && cur.VerifyHostname(cfg.ServerName) == nil {
		return nil
	}
	dns, ips := a.serverNames()
	cert, err := a.pki.IssueServer(cfg.ServerName, dns, ips)
	if err != nil {
		return err
	}
	a.log.Info("wydano certyfikat serwera", "cn", cfg.ServerName, "do", cert.NotAfter.Format(time.DateOnly))
	return a.store.AddCert(&store.CertRecord{
		Serial: pki.SerialHex(cert), Kind: "server", CN: cert.Subject.CommonName,
		NotBefore: cert.NotBefore, NotAfter: cert.NotAfter, PEM: string(pki.CertPEM(cert)),
	})
}

// authenticateStream maps a client certificate to a TAK identity; used by 8089 and Marti.
func (a *App) authenticateStream(state tls.ConnectionState) (stream.Identity, error) {
	if len(state.PeerCertificates) == 0 {
		return stream.Identity{}, errors.New("brak certyfikatu klienta")
	}
	return a.identityFromCert(state.PeerCertificates[0].SerialNumber.Bytes())
}

func (a *App) identityFromCert(serial []byte) (stream.Identity, error) {
	hexSerial := strings.ToUpper(fmt.Sprintf("%x", serial))
	rec, err := a.store.GetCert(hexSerial)
	if err != nil {
		return stream.Identity{}, fmt.Errorf("nieznany certyfikat %s", hexSerial)
	}
	if !rec.RevokedAt.IsZero() {
		return stream.Identity{}, fmt.Errorf("certyfikat %s unieważniony", hexSerial)
	}
	if rec.Kind != "client" {
		return stream.Identity{}, errors.New("to nie jest certyfikat klienta")
	}
	u, err := a.store.GetUser(rec.UserID)
	if err != nil {
		return stream.Identity{}, errors.New("użytkownik certyfikatu nie istnieje")
	}
	if u.Disabled {
		return stream.Identity{}, errors.New("konto zablokowane")
	}
	return stream.Identity{UserID: u.ID, Username: u.Username, DeviceID: rec.DeviceID, CertSerial: hexSerial, Groups: u.Groups}, nil
}

// maintenance renews the server certificate and prunes old data.
func (a *App) maintenance(ctx context.Context) {
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if a.cfg.Get().SetupDone {
				if err := a.ensureServerCert(false); err != nil {
					a.log.Error("odnowienie certyfikatu serwera", "err", err)
				}
			}
			_ = a.store.DeleteSession("")
			_ = a.store.PrunePositions(time.Now().Add(-30 * 24 * time.Hour))
		}
	}
}

// acmeALPN lets Let's Encrypt validate over TLS (tls-alpn-01) on the HTTPS/enrollment ports.
const acmeALPN = acme.ALPNProto
