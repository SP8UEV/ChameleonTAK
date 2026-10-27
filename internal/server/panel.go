package server

import (
	"context"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/panel"
	"chameleontak/internal/store"
)

const (
	sessionCookie = "ctak_session"
	sessionTTL    = 7 * 24 * time.Hour
)

type ctxKey int

const userKey ctxKey = 1

// panelHandler serves the SPA and /api on the panel ports.
func (a *App) panelHandler(isTLS bool) http.Handler {
	api := a.apiMux()
	static := http.FileServerFS(panel.FS())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := a.cfg.Get()
		if !cfg.SetupDone && a.captiveRedirect(w, r) {
			return
		}
		if !isTLS && cfg.SetupDone && cfg.ForceHTTPS && !phonePath(r.URL.Path) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if cfg.Ports.HTTPS != 443 {
				host = net.JoinHostPort(host, strconv.Itoa(cfg.Ports.HTTPS))
			}
			http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !sameOriginWrite(r) {
				httpError(w, http.StatusForbidden, "Żądanie z innej strony zostało zablokowane.")
				return
			}
			api.ServeHTTP(w, r)
			return
		}
		// SPA: serve real assets, fall back to index.html for client-side routes.
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if st, err := fs.Stat(panel.FS(), p); err != nil || st.IsDir() {
			if _, err := fs.Stat(panel.FS(), "index.html"); err != nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>ChameleonTAK</title><p>Panel WWW nie został wbudowany. Zbuduj go poleceniem <code>npm run build</code> w katalogu <code>web/</code> i skompiluj serwer ponownie.</p>`))
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			w.Header().Set("Cache-Control", "no-cache")
			static.ServeHTTP(w, r2)
			return
		}
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		static.ServeHTTP(w, r)
	})
}

// captiveRedirect sends phones that joined the setup hotspot straight to the wizard.
func (a *App) captiveRedirect(w http.ResponseWriter, r *http.Request) bool {
	probes := map[string]bool{
		"/generate_204": true, "/gen_204": true, "/hotspot-detect.html": true, "/library/test/success.html": true,
		"/ncsi.txt": true, "/connecttest.txt": true, "/redirect": true, "/canonical.html": true, "/success.txt": true,
	}
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	localIP := ""
	if local != nil {
		localIP, _, _ = net.SplitHostPort(local.String())
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	foreignHost := localIP != "" && host != localIP && host != "localhost" && !strings.HasSuffix(host, ".local") && net.ParseIP(host) == nil
	if !probes[r.URL.Path] && !foreignHost {
		return false
	}
	target := "http://" + localIP + "/"
	if localIP == "" || strings.Contains(localIP, ":") {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusFound)
	return true
}

func (a *App) apiMux() *http.ServeMux {
	m := http.NewServeMux()
	pub := func(pattern string, h http.HandlerFunc) { m.HandleFunc(pattern, h) }
	adm := func(pattern string, h http.HandlerFunc) { m.Handle(pattern, a.requireAdmin(h)) }

	pub("GET /api/status", a.hStatus)
	pub("GET /api/setup/defaults", a.hSetupDefaults)
	pub("POST /api/setup", a.hSetup)
	pub("GET /api/setup/progress", a.hSetupProgress)
	pub("POST /api/login", a.hLogin)
	pub("POST /api/logout", a.hLogout)
	pub("GET /api/me", a.hMe)
	pub("GET /api/setup/public-ip", a.hDetectPublicIP)
	pub("GET /api/join/{token}", a.hJoin)
	pub("GET /api/join/{token}/package/{platform}", a.hJoinPackage)
	pub("GET /api/join/{token}/{file}", a.hJoinPackage)
	pub("GET /api/ca.pem", a.hCAPEM)
	pub("GET /api/truststore.p12", a.hTrustStore)

	adm("GET /api/dashboard", a.hDashboard)
	adm("GET /api/settings", a.hGetSettings)
	adm("PUT /api/settings", a.hPutSettings)
	adm("GET /api/system", a.hSystem)
	adm("GET /api/system/public-access", a.hPublicAccess)
	adm("GET /api/system/public-ip", a.hDetectPublicIP)
	adm("POST /api/system/reboot", a.hReboot)
	adm("POST /api/system/network", a.hNetwork)
	adm("GET /api/system/backup", a.hBackup)
	adm("POST /api/system/restore", a.hRestore)
	adm("POST /api/system/factory-reset", a.hFactoryReset)

	adm("GET /api/users", a.hListUsers)
	adm("POST /api/users", a.hCreateUser)
	adm("POST /api/users/bulk", a.hBulkUsers)
	adm("GET /api/users/{id}", a.hGetUser)
	adm("PUT /api/users/{id}", a.hUpdateUser)
	adm("DELETE /api/users/{id}", a.hDeleteUser)
	adm("POST /api/users/{id}/devices", a.hAddDevice)
	adm("DELETE /api/devices/{id}", a.hDeleteDevice)
	adm("POST /api/devices/{id}/token", a.hNewToken)
	adm("GET /api/devices/{id}/package", a.hDevicePackage)

	adm("GET /api/groups", a.hListGroups)
	adm("POST /api/groups", a.hCreateGroup)
	adm("PUT /api/groups/{id}", a.hUpdateGroup)
	adm("DELETE /api/groups/{id}", a.hDeleteGroup)

	adm("GET /api/clients", a.hClients)
	adm("DELETE /api/clients/{id}", a.hKick)
	adm("GET /api/positions", a.hPositions)
	adm("GET /api/chat", a.hChat)
	adm("POST /api/chat", a.hSendChat)
	adm("GET /api/ws", a.hub.ServeWS)

	adm("GET /api/files", a.hListFiles)
	adm("POST /api/files", a.hUploadFile)
	adm("PUT /api/files/{hash}", a.hUpdateFile)
	adm("DELETE /api/files/{hash}", a.hDeleteFile)
	adm("GET /api/files/{hash}/download", a.hDownloadFile)

	adm("GET /api/missions", a.hListMissions)
	adm("POST /api/missions", a.hCreateMission)
	adm("GET /api/missions/{name}", a.hGetMission)
	adm("DELETE /api/missions/{name}", a.hDeleteMission)
	adm("POST /api/missions/{name}/files", a.hMissionAddFile)
	adm("DELETE /api/missions/{name}/files/{hash}", a.hMissionRemoveFile)

	adm("GET /api/certs", a.hListCerts)
	adm("GET /api/logs", a.hLogs)
	adm("POST /api/certs/{serial}/revoke", a.hRevokeCert)

	pub("/api/", func(w http.ResponseWriter, r *http.Request) { httpError(w, http.StatusNotFound, "Nieznany adres API.") })
	return m
}

func (a *App) requireAdmin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.sessionUser(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "Zaloguj się.")
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

func (a *App) sessionUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	id, err := a.store.SessionUser(c.Value)
	if err != nil {
		return nil
	}
	u, err := a.store.GetUser(id)
	if err != nil || !u.IsAdmin || u.Disabled {
		return nil
	}
	return u
}

func (a *App) hStatus(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	writeJSON(w, 200, map[string]any{
		"setupDone":   cfg.SetupDone,
		"version":     a.opts.Version,
		"serverName":  cfg.ServerName,
		"displayName": cfg.DisplayName,
		"now":         time.Now().UTC(),
	})
}

func (a *App) hLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.limiter.blocked(ip) {
		httpError(w, http.StatusTooManyRequests, "Zbyt wiele nieudanych prób. Spróbuj za kilka minut.")
		return
	}
	var req struct{ Username, Password string }
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUserByName(strings.TrimSpace(req.Username))
	if err != nil || !u.IsAdmin || u.Disabled || u.PasswordHash == "" ||
		bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		a.limiter.fail(ip)
		httpError(w, http.StatusUnauthorized, "Nieprawidłowy login lub hasło.")
		return
	}
	a.startSession(w, r, u)
	writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username})
}

func (a *App) startSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	tok := randomToken()
	if err := a.store.CreateSession(tok, u.ID, sessionTTL); err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: r.TLS != nil, MaxAge: int(sessionTTL.Seconds()),
	})
}

func (a *App) hLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = a.store.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hMe(w http.ResponseWriter, r *http.Request) {
	u := a.sessionUser(r)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "Zaloguj się.")
		return
	}
	writeJSON(w, 200, map[string]any{"id": u.ID, "username": u.Username})
}

func (a *App) hCAPEM(w http.ResponseWriter, r *http.Request) {
	ca := a.pki.CAPEM()
	if ca == nil {
		httpError(w, 404, "CA jeszcze nie istnieje.")
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", `attachment; filename="chameleontak-ca.pem"`)
	_, _ = w.Write(ca)
}

func (a *App) hTrustStore(w http.ResponseWriter, r *http.Request) {
	p12, err := a.pki.TrustStoreP12(a.cfg.Get().CertPassword)
	if err != nil {
		httpError(w, 404, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/x-pkcs12")
	w.Header().Set("Content-Disposition", `attachment; filename="truststore-root.p12"`)
	_, _ = w.Write(p12)
}

// panelBaseURL is the address the administrator opens the panel at.
func (a *App) panelBaseURL() string {
	cfg := a.cfg.Get()
	if cfg.ForceHTTPS {
		return "https://" + hostPort(cfg.ServerName, cfg.Ports.HTTPS, 443)
	}
	return "http://" + hostPort(cfg.ServerName, a.httpPort(), 80)
}

// joinBaseURL is the address phones use for the join page and the package download (join QR
// codes, ATAK's import link). Phones trust only public CAs, and ATAK's downloader rejects our own
// CA, so HTTPS is used only with a Let's Encrypt certificate and plain HTTP otherwise; the
// package sits behind a single-use token. host is the name the phone already reached the server
// by ("" means the configured server name): a phone on the local network that opened the page
// by the LAN address keeps using it, since the router may not loop back to the public address.
func (a *App) joinBaseURL(host string) string {
	cfg := a.cfg.Get()
	if host == "" || strings.EqualFold(host, cfg.ServerName) {
		if a.publicCert() {
			return "https://" + hostPort(cfg.ServerName, cfg.Ports.HTTPS, 443)
		}
		host = cfg.ServerName
	}
	return "http://" + hostPort(host, a.httpPort(), 80)
}

func (a *App) httpPort() int {
	port := a.cfg.Get().Ports.HTTP
	if _, p, err := net.SplitHostPort(a.opts.HTTPAddr); err == nil {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	return port
}

// hostPort joins a host and a port for a URL, leaving out the scheme's default port.
func hostPort(host string, port, defaultPort int) string {
	if strings.Contains(host, ":") && net.ParseIP(host) != nil {
		host = "[" + host + "]"
	}
	if port != defaultPort {
		host += ":" + strconv.Itoa(port)
	}
	return host
}

// requestHost is the host the client addressed (without the port), or "" when it is not a plain
// IP address or DNS name.
func requestHost(r *http.Request) string {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if validateServerName(host) != nil {
		return ""
	}
	return host
}

// phonePath reports the paths a phone opens from a join QR code. They stay reachable over plain
// HTTP even with "force HTTPS", because phones do not trust the server's own CA.
func phonePath(p string) bool {
	for _, prefix := range []string{"/join/", "/api/join/", "/assets/"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	switch p {
	case "/api/status", "/favicon-64.png", "/apple-touch-icon.png":
		return true
	}
	return false
}

// sameOriginWrite blocks cross-site state changes (CSRF): browsers label the request with
// Sec-Fetch-Site and/or Origin, which must point at the panel itself.
func sameOriginWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) {
			return false
		}
	}
	return true
}
