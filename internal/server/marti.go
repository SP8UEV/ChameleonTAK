package server

import (
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/datapkg"
	"chameleontak/internal/pki"
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
)

type martiCtxKey struct{}

// martiCaller is the authenticated client of a Marti request.
type martiCaller struct {
	User     *store.User
	DeviceID int64
	Token    *store.Token // set when authenticated with an enrollment token
	ViaCert  bool
}

func callerOf(r *http.Request) *martiCaller {
	c, _ := r.Context().Value(martiCtxKey{}).(*martiCaller)
	return c
}

// martiAuthenticate accepts a verified client certificate, or Basic auth with the user's
// password or a still-valid enrollment token of one of the user's devices.
func (a *App) martiAuthenticate(r *http.Request) (*martiCaller, error) {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		id, err := a.identityFromCert(r.TLS.PeerCertificates[0].SerialNumber.Bytes())
		if err != nil {
			return nil, err
		}
		u, err := a.store.GetUser(id.UserID)
		if err != nil {
			return nil, err
		}
		return &martiCaller{User: u, DeviceID: id.DeviceID, ViaCert: true}, nil
	}
	username, password, ok := r.BasicAuth()
	if !ok {
		return nil, errors.New("brak danych logowania")
	}
	ip := clientIP(r)
	if a.limiter.blocked(ip) {
		return nil, errors.New("zbyt wiele nieudanych prób")
	}
	u, err := a.store.GetUserByName(strings.TrimSpace(username))
	if err != nil || u.Disabled {
		a.limiter.fail(ip)
		return nil, errors.New("nieprawidłowy login lub hasło")
	}
	if u.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
		return &martiCaller{User: u}, nil
	}
	if t, err := a.store.GetToken(strings.ToUpper(strings.TrimSpace(password))); err == nil && tokenUsable(t) {
		if d, err := a.store.GetDevice(t.DeviceID); err == nil && d.UserID == u.ID &&
			subtle.ConstantTimeCompare([]byte(t.Token), []byte(strings.ToUpper(strings.TrimSpace(password)))) == 1 {
			return &martiCaller{User: u, DeviceID: d.ID, Token: t}, nil
		}
	}
	a.limiter.fail(ip)
	return nil, errors.New("nieprawidłowy login lub hasło")
}

func (a *App) martiHandler() http.Handler {
	m := http.NewServeMux()

	// Certificate enrollment
	m.HandleFunc("GET /Marti/api/tls/config", a.mTLSConfig)
	m.HandleFunc("POST /Marti/api/tls/signClient/v2", a.mSignClient)
	m.HandleFunc("POST /Marti/api/tls/signClient", a.mSignClient)
	m.HandleFunc("GET /Marti/api/device/profile/enrollment", a.mProfileEnrollment)
	m.HandleFunc("GET /Marti/api/device/profile/connection", a.mProfileConnection)
	m.HandleFunc("GET /Marti/api/tls/profile/enrollment", a.mProfileEnrollment)

	// Server info
	m.HandleFunc("GET /Marti/api/version", a.mVersion)
	m.HandleFunc("GET /Marti/api/version/config", a.mVersionConfig)
	m.HandleFunc("GET /Marti/api/clientEndPoints", a.mClientEndpoints)
	m.HandleFunc("GET /Marti/api/contacts/all", a.mClientEndpoints)
	m.HandleFunc("GET /Marti/api/groups/all", a.mGroups)
	m.HandleFunc("GET /Marti/api/groups/groupCacheEnabled", a.mGroupCacheEnabled)
	m.HandleFunc("PUT /Marti/api/groups/active", a.mGroupsActive)
	m.HandleFunc("PUT /Marti/api/groups/activebits", a.mGroupsActive)
	m.HandleFunc("GET /Marti/api/util/user/roles", a.mRoles)

	// File / data package sync
	m.HandleFunc("POST /Marti/sync/missionupload", a.mMissionUpload)
	m.HandleFunc("POST /Marti/sync/upload", a.mSyncUpload)
	m.HandleFunc("GET /Marti/sync/content", a.mSyncContent)
	m.HandleFunc("HEAD /Marti/sync/content", a.mSyncContent)
	m.HandleFunc("GET /Marti/sync/missionquery", a.mMissionQuery)
	m.HandleFunc("HEAD /Marti/sync/missionquery", a.mMissionQuery)
	m.HandleFunc("GET /Marti/sync/search", a.mSyncSearch)
	m.HandleFunc("GET /Marti/api/sync/search", a.mSyncSearch)
	m.HandleFunc("PUT /Marti/api/sync/metadata/{hash}/tool", a.mSyncMetadataTool)
	m.HandleFunc("PUT /Marti/api/sync/metadata/{hash}/keywords", a.mSyncMetadataKeywords)
	m.HandleFunc("DELETE /Marti/api/files/{hash}", a.mDeleteFile)
	m.HandleFunc("DELETE /Marti/sync/delete", a.mDeleteFileQuery)

	a.registerMissionRoutes(m)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller, err := a.martiAuthenticate(r)
		if err != nil {
			a.log.Debug("marti: odmowa", "path", r.URL.Path, "ip", clientIP(r), "err", err)
			w.Header().Set("WWW-Authenticate", `Basic realm="ChameleonTAK"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		a.log.Debug("marti", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery, "user", caller.User.Username)
		r = r.WithContext(withValue(r.Context(), martiCtxKey{}, caller))
		m.ServeHTTP(w, r)
	})
}

// martiJSON writes the standard Marti response envelope.
func (a *App) martiJSON(w http.ResponseWriter, status int, typ string, data any) {
	writeJSON(w, status, map[string]any{"version": "3", "type": typ, "data": data, "nodeId": a.cfg.Get().NodeID})
}

// ---------- enrollment ----------

func (a *App) mTLSConfig(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	w.Header().Set("Content-Type", "application/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<ns2:certificateConfig xmlns="http://bbn.com/marti/xml/config" xmlns:ns2="com.bbn.marti.config">`+
		`<nameEntries><nameEntry name="O" value="%s"/><nameEntry name="OU" value="TAK"/></nameEntries>`+
		`</ns2:certificateConfig>`, xmlEscape(cfg.Organization))
}

// parseCSR accepts PEM with headers or bare base64 DER, as different clients send either.
func parseCSR(body []byte) (*x509.CertificateRequest, error) {
	if block, _ := pem.Decode(body); block != nil {
		return x509.ParseCertificateRequest(block.Bytes)
	}
	clean := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, string(body))
	der, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("CSR nie jest PEM ani base64: %w", err)
	}
	return x509.ParseCertificateRequest(der)
}

// b64DER encodes a certificate the way TAK Server does: base64 DER without PEM armour, wrapped at
// 64 characters with no trailing newline (TAKAware rejects any other line length).
func b64DER(c *x509.Certificate) string {
	s := base64.StdEncoding.EncodeToString(c.Raw)
	var b strings.Builder
	for len(s) > 64 {
		b.WriteString(s[:64])
		b.WriteByte('\n')
		s = s[64:]
	}
	b.WriteString(s)
	return b.String()
}

func (a *App) mSignClient(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	cfg := a.cfg.Get()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	csr, err := parseCSR(body)
	if err != nil {
		a.log.Warn("enrollment: niepoprawny CSR", "user", caller.User.Username, "err", err)
		http.Error(w, "invalid CSR", 400)
		return
	}
	deviceID, err := a.enrollmentDevice(caller, r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	cert, err := a.pki.SignCSR(csr, caller.User.Username, time.Duration(cfg.CertValidityDays)*24*time.Hour)
	if err != nil {
		a.log.Warn("enrollment: podpis CSR", "user", caller.User.Username, "err", err)
		http.Error(w, "cannot sign CSR", 400)
		return
	}
	if err := a.recordClientCert(cert, caller.User.ID, deviceID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if caller.Token != nil {
		_ = a.store.MarkTokenUsed(caller.Token.Token)
	}
	if uid := r.URL.Query().Get("clientUid"); uid != "" && deviceID > 0 {
		_ = a.store.TouchDevice(deviceID, uid)
	}
	a.log.Info("wydano certyfikat (enrollment)", "user", caller.User.Username, "device", deviceID, "serial", pki.SerialHex(cert), "client", r.URL.Query().Get("version"))

	ca := a.pki.CACert()

	// v1 (ATAK ≤ 4.6): a PKCS#12 with entries signedCert / ca0, password "atakatak".
	if !strings.HasSuffix(r.URL.Path, "/v2") {
		p12, err := a.pki.EnrollmentP12(cert, "atakatak")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/x-pkcs12")
		_, _ = w.Write(p12)
		return
	}

	// v2: XML only when explicitly asked for, JSON otherwise (iTAK wants it labelled text/plain).
	accept := strings.ToLower(r.Header.Get("Accept"))
	if strings.Contains(accept, "application/xml") && !strings.Contains(accept, "application/json") {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><enrollment><signedCert>%s</signedCert><ca>%s</ca></enrollment>`,
			b64DER(cert), b64DER(ca))
		return
	}
	ct := "application/json"
	if accept == "text/plain" {
		ct = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"signedCert": b64DER(cert), "ca0": b64DER(ca)})
}

// enrollmentDevice picks the device a newly enrolled certificate belongs to. Token logins already
// identify the device; password logins get a device created from the client's platform.
func (a *App) enrollmentDevice(c *martiCaller, r *http.Request) (int64, error) {
	if c.DeviceID > 0 {
		return c.DeviceID, nil
	}
	platform := guessPlatform(r.URL.Query().Get("version") + " " + r.UserAgent())
	uid := r.URL.Query().Get("clientUid")
	devs, err := a.store.ListDevices(c.User.ID)
	if err != nil {
		return 0, err
	}
	for _, d := range devs {
		if uid != "" && d.ClientUID == uid {
			return d.ID, nil
		}
	}
	for _, d := range devs {
		if d.Platform == platform && d.ClientUID == "" {
			return d.ID, nil
		}
	}
	d := &store.Device{UserID: c.User.ID, Platform: platform, Name: "Zarejestrowane automatycznie"}
	if err := a.store.CreateDevice(d); err != nil {
		return 0, err
	}
	return d.ID, nil
}

func guessPlatform(s string) string {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "takaware"):
		return "takaware"
	case strings.Contains(s, "itak") || strings.Contains(s, "ios") || strings.Contains(s, "iphone") || strings.Contains(s, "darwin") || strings.Contains(s, "cfnetwork"):
		return "itak"
	case strings.Contains(s, "wintak") || strings.Contains(s, "windows"):
		return "wintak"
	}
	return "atak"
}

// mProfileEnrollment returns the device profile pushed right after enrollment (callsign, team, role, …).
func (a *App) mProfileEnrollment(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	cfg := a.cfg.Get()
	platform := "atak"
	if caller.DeviceID > 0 {
		if d, err := a.store.GetDevice(caller.DeviceID); err == nil {
			platform = d.Platform
		}
	}
	zip, err := datapkg.Profile(datapkg.Params{
		Platform: platform, Config: cfg, Username: caller.User.Username, Callsign: caller.User.Callsign,
		Team: caller.User.Team, Role: caller.User.Role,
	})
	if err != nil || zip == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	sendZip(w, "profile.zip", zip)
}

func (a *App) mProfileConnection(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// ---------- server info ----------

func (a *App) mVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	// ATAK only trusts this fallback if the body mentions "TAK Server".
	fmt.Fprintf(w, "TAK Server %s (ChameleonTAK %s)", takCompatVersion, a.opts.Version)
}

func (a *App) mVersionConfig(w http.ResponseWriter, r *http.Request) {
	cfg := a.cfg.Get()
	a.martiJSON(w, 200, "ServerConfig", map[string]string{
		"version": takCompatVersion + "-RELEASE", "api": "3", "hostname": cfg.ServerName,
	})
}

func (a *App) mClientEndpoints(w http.ResponseWriter, r *http.Request) {
	var out []map[string]any
	for _, c := range a.router.Clients() {
		if c.UID == "" {
			continue
		}
		out = append(out, map[string]any{
			"callsign": c.Callsign, "uid": c.UID, "username": c.Username,
			"lastEventTime": time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "lastStatus": "Connected",
		})
	}
	if out == nil {
		out = []map[string]any{}
	}
	a.martiJSON(w, 200, "com.bbn.marti.remote.ClientEndpoint", out)
}

func (a *App) callerGroups(c *martiCaller) []string {
	if len(c.User.Groups) == 0 {
		return []string{stream.AnonGroup}
	}
	return c.User.Groups
}

// takCompatVersion is the TAK Server API level we emulate; ATAK derives feature support from it.
const takCompatVersion = "5.4"

func (a *App) mGroups(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	out := []map[string]any{}
	for i, g := range a.callerGroups(caller) {
		for _, dir := range []string{"IN", "OUT"} {
			out = append(out, map[string]any{
				"name": g, "direction": dir, "created": "2026-01-01", "type": "SYSTEM", "bitpos": i + 2, "active": true,
				"description": "",
			})
		}
	}
	if r.URL.Query().Get("sendLatestSA") == "true" {
		if c := a.router.ClientByUID(r.URL.Query().Get("clientUid")); c != nil {
			a.router.ReplaySA(c)
		}
	}
	a.martiJSON(w, 200, "com.bbn.marti.remote.groups.Group", out)
}

func (a *App) mGroupCacheEnabled(w http.ResponseWriter, r *http.Request) {
	a.martiJSON(w, 200, "java.lang.Boolean", false)
}

func (a *App) mGroupsActive(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 64<<10))
	w.WriteHeader(http.StatusOK)
}

func (a *App) mRoles(w http.ResponseWriter, r *http.Request) {
	roles := []string{"ROLE_ANONYMOUS"}
	if callerOf(r).User.IsAdmin {
		roles = append(roles, "ROLE_ADMIN")
	}
	a.martiJSON(w, 200, "java.lang.String", roles)
}

// ---------- data package / file sync ----------

func (a *App) fileURL(r *http.Request, hash string) string {
	cfg := a.cfg.Get()
	return fmt.Sprintf("https://%s:%d/Marti/sync/content?hash=%s", cfg.ServerName, cfg.Ports.Marti, hash)
}

func (a *App) mMissionUpload(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	q := r.URL.Query()
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	var src io.Reader
	name := q.Get("filename")
	mimeType := ""
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		f, hdr, err := r.FormFile("assetfile")
		if err != nil {
			http.Error(w, "missing assetfile", 400)
			return
		}
		defer f.Close()
		src, mimeType = f, hdr.Header.Get("Content-Type")
		if name == "" {
			name = hdr.Filename
		}
	} else {
		src = r.Body
	}
	hash, size, err := a.saveContent(src)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if want := strings.ToLower(q.Get("hash")); want != "" && want != hash {
		a.log.Warn("missionupload: niezgodny hash", "deklarowany", want, "obliczony", hash)
	}
	name = cleanName(name)
	rec := &store.File{
		Hash: hash, UID: randomUUID(), Name: name, MIME: guessMIME(name, mimeType), Size: size,
		Keywords: []string{"missionpackage"}, CreatorUID: q.Get("creatorUid"), Submitter: caller.User.Username, Tool: "public",
	}
	if old, err := a.store.GetFile(hash); err == nil {
		rec.UID, rec.Tool, rec.CreatedAt = old.UID, old.Tool, old.CreatedAt
	}
	if err := a.store.UpsertFile(rec); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, a.fileURL(r, hash))
}

// mSyncUpload handles generic uploads (e.g. images attached to markers).
func (a *App) mSyncUpload(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	q := r.URL.Query()
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+(1<<20))
	var src io.Reader = r.Body
	name, mimeType := q.Get("name"), r.Header.Get("Content-Type")
	if strings.HasPrefix(mimeType, "multipart/") {
		f, hdr, err := r.FormFile("assetfile")
		if err != nil {
			http.Error(w, "missing assetfile", 400)
			return
		}
		defer f.Close()
		src, mimeType = f, hdr.Header.Get("Content-Type")
		if name == "" {
			name = hdr.Filename
		}
	}
	hash, size, err := a.saveContent(src)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name = cleanName(name)
	var kw []string
	for _, k := range q["keywords"] {
		kw = append(kw, strings.Split(k, ",")...)
	}
	rec := &store.File{
		Hash: hash, UID: randomUUID(), Name: name, MIME: guessMIME(name, mimeType), Size: size, Keywords: kw,
		CreatorUID: firstNonEmpty(q.Get("creatorUid"), q.Get("CreatorUid")), Submitter: caller.User.Username, Tool: "public",
	}
	if err := a.store.UpsertFile(rec); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, 200, map[string]any{
		"UID": rec.UID, "SubmissionDateTime": rec.CreatedAt.Format(time.RFC3339), "Keywords": kw, "MIMEType": rec.MIME,
		"SubmissionUser": rec.Submitter, "PrimaryKey": rec.UID, "Hash": hash, "CreatorUid": rec.CreatorUID, "Name": name,
		"Size": size,
	})
}

func (a *App) mSyncContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	hash := strings.ToLower(q.Get("hash"))
	var f *store.File
	var err error
	if hash != "" {
		f, err = a.store.GetFile(hash)
	} else if uid := q.Get("uid"); uid != "" {
		f, err = a.fileByUID(uid)
	} else {
		err = store.ErrNotFound
	}
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	a.serveStoredFile(w, r, f)
}

func (a *App) fileByUID(uid string) (*store.File, error) {
	files, err := a.store.ListFiles()
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.UID == uid {
			return f, nil
		}
	}
	return nil, store.ErrNotFound
}

func (a *App) mMissionQuery(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	if _, err := a.store.GetFile(hash); err != nil {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = io.WriteString(w, a.fileURL(r, hash))
}

func (a *App) mSyncSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	keyword := strings.ToLower(q.Get("keywords"))
	tool := q.Get("tool")
	files, err := a.store.ListFiles()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	results := []map[string]any{}
	for _, f := range files {
		if tool != "" && f.Tool != tool {
			continue
		}
		if tool == "" && f.Tool == "private" {
			continue
		}
		if keyword != "" && !hasKeyword(f.Keywords, keyword) && !strings.Contains(strings.ToLower(f.Name), keyword) {
			continue
		}
		results = append(results, map[string]any{
			"UID": f.UID, "Name": f.Name, "Hash": f.Hash, "PrimaryKey": f.UID, "SubmissionDateTime": f.CreatedAt.Format("2006-01-02T15:04:05.000Z"),
			"SubmissionUser": f.Submitter, "CreatorUid": f.CreatorUID, "Keywords": strings.Join(f.Keywords, ","), "MIMEType": f.MIME,
			"Size": f.Size, "Tool": f.Tool, "Expiration": -1,
		})
	}
	writeJSON(w, 200, map[string]any{"resultCount": len(results), "results": results})
}

func hasKeyword(kw []string, k string) bool {
	for _, x := range kw {
		if strings.EqualFold(x, k) {
			return true
		}
	}
	return false
}

func (a *App) mSyncMetadataTool(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	body, _ := io.ReadAll(io.LimitReader(r.Body, 256))
	tool := strings.TrimSpace(string(body))
	if tool == "" {
		tool = r.URL.Query().Get("tool")
	}
	if tool != "private" {
		tool = "public"
	}
	if err := a.store.SetFileTool(hash, tool); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *App) mSyncMetadataKeywords(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	var kw []string
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err := remarshalBytes(body, &kw); err != nil {
		for _, k := range strings.Split(string(body), ",") {
			if k = strings.TrimSpace(k); k != "" {
				kw = append(kw, k)
			}
		}
	}
	if err := a.store.SetFileKeywords(hash, kw); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (a *App) mDeleteFile(w http.ResponseWriter, r *http.Request) {
	a.deleteOwnFile(w, r, strings.ToLower(r.PathValue("hash")))
}

func (a *App) mDeleteFileQuery(w http.ResponseWriter, r *http.Request) {
	a.deleteOwnFile(w, r, strings.ToLower(r.URL.Query().Get("hash")))
}

// deleteOwnFile lets a client delete files it uploaded (admins may delete any).
func (a *App) deleteOwnFile(w http.ResponseWriter, r *http.Request, hash string) {
	caller := callerOf(r)
	f, err := a.store.GetFile(hash)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if !caller.User.IsAdmin && f.Submitter != caller.User.Username {
		http.Error(w, "forbidden", 403)
		return
	}
	if err := a.deleteStoredFile(hash); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusOK)
}
