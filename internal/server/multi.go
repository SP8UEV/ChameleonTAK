package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/store"
	"chameleontak/internal/system"
)

// Several TAK servers on one device: the main server owns the web panel (ports 80/443), the
// administrator accounts, the network, the firewall and ADS-B. Every additional server lives in
// servers/<id>/ under the main data directory with its own database, CA, users, groups and TAK
// ports (CoT, Marti, enrollment), and runs as a second App inside the same process. The panel
// picks a server with the X-CTAK-Server header (or ?server= for links and the websocket); the
// main server checks the session and hands the request to that server's API.

const (
	mainServerID = "main"
	serversDir   = "servers"
	serverHeader = "X-CTAK-Server"
)

var reServerID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

type serverSet struct {
	mu   sync.RWMutex
	list []*runningServer
	ctx  context.Context
}

type runningServer struct {
	app    *App
	cancel context.CancelFunc
	done   chan struct{}
}

// ID is the server's identifier in the panel ("main" for the main server).
func (a *App) ID() string {
	if a.parent == nil {
		return mainServerID
	}
	return a.id
}

// host is the main server (the one with the panel), for a main or an additional server.
func (a *App) host() *App {
	if a.parent != nil {
		return a.parent
	}
	return a
}

// servers lists the main server and every additional one.
func (a *App) servers() []*App {
	h := a.host()
	out := []*App{h}
	if h.others == nil {
		return out
	}
	h.others.mu.RLock()
	defer h.others.mu.RUnlock()
	for _, s := range h.others.list {
		out = append(out, s.app)
	}
	return out
}

// serverByID finds a server; nil when there is none.
func (a *App) serverByID(id string) *App {
	for _, s := range a.servers() {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

// startOthers opens and runs every additional server found on disk (main server only).
func (a *App) startOthers() {
	entries, err := os.ReadDir(a.DataPath(serversDir))
	if err != nil {
		return
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() && reServerID.MatchString(e.Name()) && e.Name() != mainServerID {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, id := range names {
		if _, err := a.openOther(id); err != nil {
			a.log.Error("dodatkowy serwer", "id", id, "err", err)
		}
	}
}

// openOther opens servers/<id> and starts it.
func (a *App) openOther(id string) (*App, error) {
	s, err := New(Options{DataDir: a.DataPath(serversDir, id), Version: a.opts.Version, HTTPAddr: a.opts.HTTPAddr, HTTPSAddr: a.opts.HTTPSAddr, Dev: a.opts.Dev},
		a.baseLog.With("serwer", id))
	if err != nil {
		return nil, err
	}
	s.parent, s.id = a, id
	ctx, cancel := context.WithCancel(a.others.ctx)
	rs := &runningServer{app: s, cancel: cancel, done: make(chan struct{})}
	a.others.mu.Lock()
	a.others.list = append(a.others.list, rs)
	a.others.mu.Unlock()
	go func() {
		defer close(rs.done)
		s.runAdditional(ctx)
	}()
	return s, nil
}

// waitOthers waits (briefly) for the additional servers to stop after the context ended.
func (a *App) waitOthers() {
	if a.others == nil {
		return
	}
	a.others.mu.RLock()
	list := slices.Clone(a.others.list)
	a.others.mu.RUnlock()
	deadline := time.After(8 * time.Second)
	for _, rs := range list {
		select {
		case <-rs.done:
		case <-deadline:
			return
		}
	}
}

// runAdditional serves an additional server: only its TAK listeners, the panel stays with the
// main server.
func (a *App) runAdditional(ctx context.Context) {
	a.syncPanelPorts()
	if a.cfg.Get().SetupDone {
		if err := a.ensureServerCert(false); err != nil {
			a.log.Error("certyfikat serwera", "err", err)
		}
		a.startServices()
	}
	go a.pos.Run(ctx)
	go a.maintenance(ctx)
	go a.ddns.run(ctx)
	<-ctx.Done()
	a.stopServices()
	a.router.CloseAll()
	a.pos.Flush()
	_ = a.store.Close()
}

// syncPanelPorts copies the panel ports of the main server, which join links of an additional
// server point at.
func (a *App) syncPanelPorts() {
	if a.parent == nil {
		return
	}
	hp := a.parent.cfg.Get().Ports
	_, _ = a.cfg.Update(func(c *config.Config) error {
		c.Ports.HTTP, c.Ports.HTTPS = hp.HTTP, hp.HTTPS
		c.ForceHTTPS = a.parent.cfg.Get().ForceHTTPS
		return nil
	})
}

// usedPorts maps every TCP port taken by a server other than except (and the panel) to a label.
func (a *App) usedPorts(except *App) map[int]string {
	h := a.host()
	hp := h.cfg.Get().Ports
	used := map[int]string{
		h.httpPort(): "panel WWW (HTTP)", addrPort(h.panelHTTPSAddr(), hp.HTTPS): "panel WWW (HTTPS)",
		22: "SSH", 53: "DNS",
	}
	for _, s := range a.servers() {
		if s == except {
			continue
		}
		c := s.cfg.Get()
		name := c.DisplayName
		for _, p := range takPorts(c.Ports) {
			used[p] = "serwer „" + name + "”"
		}
	}
	return used
}

// takPorts are the TAK ports a server listens on.
func takPorts(p config.Ports) []int {
	ps := []int{p.CoTTLS, p.Marti, p.Enroll}
	if p.CoTTCPEnabled {
		ps = append(ps, p.CoTTCP)
	}
	return ps
}

// checkPortConflicts rejects TAK ports that a server other than except (or the panel) already
// uses; except is nil for a server being created.
func (a *App) checkPortConflicts(p config.Ports, except *App) error {
	used := a.usedPorts(except)
	for _, port := range takPorts(p) {
		if who, ok := used[port]; ok {
			return invalid(fmt.Sprintf("Port %d jest już zajęty: %s.", port, who))
		}
	}
	return nil
}

// suggestPorts proposes a free set of TAK ports for a new server: 8189/8543/8546, 8289/…
func (a *App) suggestPorts() config.Ports {
	used := a.usedPorts(nil)
	base := config.DefaultPorts()
	for n := 1; n < 50; n++ {
		p := base
		p.CoTTLS, p.Marti, p.Enroll, p.CoTTCP = base.CoTTLS+100*n, base.Marti+100*n, base.Enroll+100*n, base.CoTTCP+100*n
		free := true
		for _, port := range []int{p.CoTTLS, p.Marti, p.Enroll} {
			if _, ok := used[port]; ok {
				free = false
			}
		}
		if free {
			return p
		}
	}
	return base
}

// ---------- routing panel requests ----------

// hostOnly are the API paths that always belong to the main server: login, administrator
// accounts, the device itself (network, firewall, backups) and ADS-B.
func hostOnly(p string) bool {
	switch {
	case p == "/api/system/public-access", p == "/api/system/ddns/update":
		return false // per server: its own address and certificate
	case strings.HasPrefix(p, "/api/system/"), strings.HasPrefix(p, "/api/setup"), strings.HasPrefix(p, "/api/me"),
		strings.HasPrefix(p, "/api/adsb"), strings.HasPrefix(p, "/api/servers"), strings.HasPrefix(p, "/api/video/hls/"):
		return true
	}
	switch p {
	case "/api/status", "/api/login", "/api/logout", "/api/video/auth":
		return true
	}
	return false
}

// requestServer picks the server an API request is for; nil means the main server.
func (a *App) requestServer(r *http.Request) (*App, error) {
	if a.parent != nil || hostOnly(r.URL.Path) {
		return nil, nil
	}
	id := r.Header.Get(serverHeader)
	if id == "" {
		id = r.URL.Query().Get("server")
	}
	if id == "" {
		// Phones open join links without choosing a server: the token tells which one.
		if rest, ok := strings.CutPrefix(r.URL.Path, "/api/join/"); ok {
			tok, _, _ := strings.Cut(rest, "/")
			for _, s := range a.servers()[1:] {
				if _, err := s.store.GetToken(tok); err == nil {
					return s, nil
				}
			}
		}
		return nil, nil
	}
	if id == mainServerID {
		return nil, nil
	}
	s := a.serverByID(id)
	if s == nil {
		return nil, invalid("Nie ma takiego serwera — odśwież panel.")
	}
	return s, nil
}

// delegateKey marks a request the main server already authenticated.
const delegateKey ctxKey = 2

// serveDelegated hands an API request to an additional server, with the administrator the
// main server's session belongs to.
func (a *App) serveDelegated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if u := a.parent.sessionUser(r); u != nil {
		// A different ID than any user of this server, so "you cannot delete yourself" checks
		// never match one of its users.
		admin := &store.User{ID: -1, Username: u.Username, IsAdmin: true}
		ctx = context.WithValue(ctx, delegateKey, admin)
	}
	a.api.ServeHTTP(w, r.WithContext(ctx))
}

// ---------- API ----------

type serverView struct {
	ID          string          `json:"id"`
	Main        bool            `json:"main"`
	DisplayName string          `json:"displayName"`
	ServerName  string          `json:"serverName"`
	Ports       config.Ports    `json:"ports"`
	Platforms   []string        `json:"platforms"`
	Clients     int             `json:"clients"`
	Users       int             `json:"users"`
	Listeners   []ListenerState `json:"listeners"`
}

func (a *App) serverInfo() serverView {
	c := a.cfg.Get()
	users, _ := a.store.ListUsers()
	return serverView{
		ID: a.ID(), Main: a.parent == nil, DisplayName: c.DisplayName, ServerName: c.ServerName, Ports: c.Ports,
		Platforms: c.Platforms, Clients: a.router.Count(), Users: len(users), Listeners: a.listenerStates(),
	}
}

func (a *App) hListServers(w http.ResponseWriter, r *http.Request) {
	out := []serverView{}
	for _, s := range a.servers() {
		out = append(out, s.serverInfo())
	}
	writeJSON(w, 200, map[string]any{"servers": out, "suggestedPorts": a.suggestPorts()})
}

type newServerRequest struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	ServerName  string       `json:"serverName"`
	Ports       config.Ports `json:"ports"`
	Platforms   []string     `json:"platforms"`
}

func (a *App) hCreateServer(w http.ResponseWriter, r *http.Request) {
	var req newServerRequest
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	req.ID = strings.ToLower(strings.TrimSpace(req.ID))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.ServerName = strings.TrimSpace(req.ServerName)
	switch {
	case !reServerID.MatchString(req.ID) || req.ID == mainServerID:
		fail(w, invalid("Identyfikator: 1–24 znaki, małe litery, cyfry i myślniki (np. druzyna-b)."))
		return
	case a.serverByID(req.ID) != nil || fileExists(a.DataPath(serversDir, req.ID)):
		fail(w, invalid("Serwer o takim identyfikatorze już istnieje."))
		return
	case req.DisplayName == "":
		fail(w, invalid("Podaj nazwę serwera."))
		return
	case len(req.Platforms) == 0:
		fail(w, invalid("Wybierz co najmniej jedną aplikację."))
		return
	}
	for _, p := range req.Platforms {
		if !config.ValidPlatform(p) {
			fail(w, invalid("Nieznana platforma "+p))
			return
		}
	}
	if req.ServerName == "" {
		req.ServerName = a.cfg.Get().ServerName
	}
	if err := validateServerName(req.ServerName); err != nil {
		fail(w, err)
		return
	}
	hp := a.cfg.Get().Ports
	ports := req.Ports
	ports.HTTP, ports.HTTPS = hp.HTTP, hp.HTTPS
	if ports.CoTTCP == 0 {
		ports.CoTTCP = ports.CoTTLS - 2
	}
	if err := validatePorts(ports); err != nil {
		fail(w, err)
		return
	}
	if err := a.checkPortConflicts(ports, nil); err != nil {
		fail(w, err)
		return
	}

	s, err := a.createOther(req, ports)
	if err != nil {
		fail(w, err)
		return
	}
	a.log.Info("dodano serwer", "id", req.ID, "nazwa", req.DisplayName)
	if err := a.applyFirewall(); err != nil && !errors.Is(err, system.ErrUnsupported) {
		a.log.Warn("zapora", "err", err)
	}
	time.Sleep(300 * time.Millisecond) // let the listeners start before reporting them
	writeJSON(w, 201, s.serverInfo())
}

// createOther prepares servers/<id> (configuration, CA, server certificate) and starts it.
func (a *App) createOther(req newServerRequest, ports config.Ports) (*App, error) {
	dir := a.DataPath(serversDir, req.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	main := a.cfg.Get()
	prep, err := New(Options{DataDir: dir, Version: a.opts.Version}, a.baseLog.With("serwer", req.ID))
	if err != nil {
		return nil, err
	}
	_, err = prep.cfg.Update(func(c *config.Config) error {
		c.ServerName, c.DisplayName, c.Organization = req.ServerName, req.DisplayName, main.Organization
		c.Ports, c.Platforms = ports, req.Platforms
		c.MapTileURL, c.CertPassword = main.MapTileURL, main.CertPassword
		c.Network = main.Network
		return nil
	})
	if err == nil {
		_, err = prep.pki.InitCA(req.DisplayName, main.Organization)
	}
	if err == nil {
		err = prep.ensureServerCert(true)
	}
	if err == nil {
		_, err = prep.cfg.Update(func(c *config.Config) error { c.SetupDone = true; return nil })
	}
	_ = prep.Close()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	return a.openOther(req.ID)
}

func (a *App) hDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == mainServerID {
		fail(w, invalid("Serwera głównego nie można usunąć."))
		return
	}
	a.others.mu.Lock()
	i := slices.IndexFunc(a.others.list, func(s *runningServer) bool { return s.app.id == id })
	if i < 0 {
		a.others.mu.Unlock()
		httpError(w, http.StatusNotFound, "Nie ma takiego serwera.")
		return
	}
	rs := a.others.list[i]
	a.others.list = slices.Delete(a.others.list, i, i+1)
	a.others.mu.Unlock()
	rs.cancel()
	select {
	case <-rs.done:
	case <-time.After(10 * time.Second):
	}
	// Kept for a while instead of deleted, in case it was the wrong server.
	trash := a.DataPath("servers-deleted", fmt.Sprintf("%s-%s", id, time.Now().Format("20060102-150405")))
	_ = os.MkdirAll(filepath.Dir(trash), 0o750)
	// The database may take a moment to release its files after the server stopped.
	var err error
	for try := 0; try < 10; try++ {
		if err = os.Rename(a.DataPath(serversDir, id), trash); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		a.log.Error("usuwanie serwera", "id", id, "err", err)
		fail(w, fmt.Errorf("serwer zatrzymano, ale nie udało się przenieść jego danych: %w", err))
		return
	}
	a.log.Info("usunięto serwer", "id", id, "dane", trash)
	if err := a.applyFirewall(); err != nil && !errors.Is(err, system.ErrUnsupported) {
		a.log.Warn("zapora", "err", err)
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
