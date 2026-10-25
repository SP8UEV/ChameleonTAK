package server

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/config"
	"chameleontak/internal/datapkg"
	"chameleontak/internal/store"
)

var reUsername = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{1,31}$`)

type enrollmentJSON struct {
	Token     string            `json:"token"`
	ExpiresAt time.Time         `json:"expiresAt"`
	Used      bool              `json:"used"`
	Valid     bool              `json:"valid"`
	JoinURL   string            `json:"joinUrl"`
	QR        map[string]string `json:"qr"`
}

type deviceJSON struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"userId"`
	Name        string          `json:"name"`
	Platform    string          `json:"platform"`
	ClientUID   string          `json:"clientUid"`
	Online      bool            `json:"online"`
	LastSeen    *time.Time      `json:"lastSeen"`
	CertSerial  string          `json:"certSerial"`
	CertExpires *time.Time      `json:"certExpires"`
	Enrollment  *enrollmentJSON `json:"enrollment"`
}

type userJSON struct {
	ID        int64        `json:"id"`
	Username  string       `json:"username"`
	Callsign  string       `json:"callsign"`
	Team      string       `json:"team"`
	Role      string       `json:"role"`
	IsAdmin   bool         `json:"isAdmin"`
	Disabled  bool         `json:"disabled"`
	Groups    []string     `json:"groups"`
	Devices   []deviceJSON `json:"devices"`
	Online    bool         `json:"online"`
	CreatedAt time.Time    `json:"createdAt"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// onlineDevices returns device IDs and user IDs with a live connection.
func (a *App) onlineSets() (devices map[int64]bool, users map[int64]bool) {
	devices, users = map[int64]bool{}, map[int64]bool{}
	for _, c := range a.router.Clients() {
		devices[c.DeviceID] = true
		users[c.UserID] = true
	}
	return
}

// tokenUsable reports whether an enrollment token can still be used. After the first use the
// token stays valid for a short grace period, because clients make several calls in a row.
func tokenUsable(t *store.Token) bool {
	now := time.Now()
	if now.After(t.ExpiresAt) {
		return false
	}
	return t.UsedAt.IsZero() || now.Before(t.UsedAt.Add(15*time.Minute))
}

func (a *App) deviceView(u *store.User, d *store.Device, online map[int64]bool) deviceJSON {
	cfg := a.cfg.Get()
	dj := deviceJSON{
		ID: d.ID, UserID: d.UserID, Name: d.Name, Platform: d.Platform, ClientUID: d.ClientUID,
		Online: online[d.ID], LastSeen: timePtr(d.LastSeen),
	}
	if c, err := a.store.LatestDeviceCert(d.ID); err == nil {
		dj.CertSerial, dj.CertExpires = c.Serial, timePtr(c.NotAfter)
	}
	if t, err := a.store.LatestToken(d.ID); err == nil {
		join := a.panelBaseURL() + "/join/" + t.Token
		dj.Enrollment = &enrollmentJSON{
			Token: t.Token, ExpiresAt: t.ExpiresAt, Used: !t.UsedAt.IsZero(), Valid: tokenUsable(t), JoinURL: join,
			QR: map[string]string{
				"join":   join,
				"native": datapkg.NativeQR(d.Platform, cfg, u.Username, t.Token),
			},
		}
	}
	return dj
}

func (a *App) userView(u *store.User, onlineDev, onlineUsers map[int64]bool) (userJSON, error) {
	uj := userJSON{
		ID: u.ID, Username: u.Username, Callsign: u.Callsign, Team: u.Team, Role: u.Role, IsAdmin: u.IsAdmin,
		Disabled: u.Disabled, Groups: u.Groups, Online: onlineUsers[u.ID], CreatedAt: u.CreatedAt, Devices: []deviceJSON{},
	}
	if uj.Groups == nil {
		uj.Groups = []string{}
	}
	devs, err := a.store.ListDevices(u.ID)
	if err != nil {
		return uj, err
	}
	for _, d := range devs {
		uj.Devices = append(uj.Devices, a.deviceView(u, d, onlineDev))
	}
	return uj, nil
}

func (a *App) hListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.ListUsers()
	if err != nil {
		fail(w, err)
		return
	}
	od, ou := a.onlineSets()
	out := []userJSON{}
	for _, u := range users {
		uj, err := a.userView(u, od, ou)
		if err != nil {
			fail(w, err)
			return
		}
		out = append(out, uj)
	}
	writeJSON(w, 200, out)
}

func (a *App) hGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUser(id)
	if err != nil {
		fail(w, err)
		return
	}
	od, ou := a.onlineSets()
	uj, err := a.userView(u, od, ou)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, uj)
}

type userInput struct {
	Username string   `json:"username"`
	Callsign string   `json:"callsign"`
	Team     string   `json:"team"`
	Role     string   `json:"role"`
	Password string   `json:"password"`
	IsAdmin  *bool    `json:"isAdmin"`
	Disabled *bool    `json:"disabled"`
	Groups   []string `json:"groups"`
	Devices  []struct {
		Platform string `json:"platform"`
		Name     string `json:"name"`
	} `json:"devices"`
}

func cleanGroups(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range in {
		g = strings.TrimSpace(g)
		if g == "" || seen[strings.ToLower(g)] || len(g) > 64 {
			continue
		}
		seen[strings.ToLower(g)] = true
		out = append(out, g)
	}
	return out
}

// createUser creates a TAK user with its devices; every device gets an enrollment token.
func (a *App) createUser(in userInput) (*store.User, error) {
	cfg := a.cfg.Get()
	in.Username = strings.TrimSpace(in.Username)
	if !reUsername.MatchString(in.Username) {
		return nil, invalid("Nazwa użytkownika: 2–32 znaki, litery, cyfry, kropka, myślnik lub podkreślnik.")
	}
	if _, err := a.store.GetUserByName(in.Username); err == nil {
		return nil, invalid(fmt.Sprintf("Użytkownik %q już istnieje.", in.Username))
	}
	u := &store.User{
		Username: in.Username,
		Callsign: strings.TrimSpace(in.Callsign),
		Team:     strings.TrimSpace(in.Team),
		Role:     strings.TrimSpace(in.Role),
	}
	if u.Callsign == "" {
		u.Callsign = strings.ToUpper(in.Username)
	}
	if u.Team == "" {
		u.Team = cfg.Profile.DefaultTeam
	}
	if u.Role == "" {
		u.Role = cfg.Profile.DefaultRole
	}
	if in.IsAdmin != nil {
		u.IsAdmin = *in.IsAdmin
	}
	if in.Password != "" {
		if len(in.Password) < 8 {
			return nil, invalid("Hasło musi mieć co najmniej 8 znaków.")
		}
		h, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = string(h)
	}
	if u.IsAdmin && u.PasswordHash == "" {
		return nil, invalid("Administrator musi mieć hasło.")
	}
	for _, d := range in.Devices {
		if !config.ValidPlatform(d.Platform) {
			return nil, invalid(fmt.Sprintf("Nieznana platforma %q.", d.Platform))
		}
	}
	if err := a.store.CreateUser(u); err != nil {
		return nil, err
	}
	if err := a.store.SetUserGroups(u.ID, cleanGroups(in.Groups)); err != nil {
		return nil, err
	}
	for _, d := range in.Devices {
		if _, err := a.addDevice(u, d.Platform, d.Name); err != nil {
			return nil, err
		}
	}
	return a.store.GetUser(u.ID)
}

func (a *App) addDevice(u *store.User, platform, name string) (*store.Device, error) {
	if !config.ValidPlatform(platform) {
		return nil, invalid(fmt.Sprintf("Nieznana platforma %q.", platform))
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = config.PlatformLabel(platform)
	}
	d := &store.Device{UserID: u.ID, Name: name, Platform: platform}
	if err := a.store.CreateDevice(d); err != nil {
		return nil, err
	}
	if _, err := a.newToken(d.ID); err != nil {
		return nil, err
	}
	return d, nil
}

func (a *App) newToken(deviceID int64) (*store.Token, error) {
	hours := a.cfg.Get().TokenValidityHours
	if hours <= 0 {
		hours = 168
	}
	_ = a.store.DeleteDeviceTokens(deviceID)
	t := &store.Token{Token: randomCode(8), DeviceID: deviceID, ExpiresAt: time.Now().Add(time.Duration(hours) * time.Hour).UTC()}
	return t, a.store.CreateToken(t)
}

func (a *App) respondUser(w http.ResponseWriter, status int, id int64) {
	u, err := a.store.GetUser(id)
	if err != nil {
		fail(w, err)
		return
	}
	od, ou := a.onlineSets()
	uj, err := a.userView(u, od, ou)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, status, uj)
}

func (a *App) hCreateUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	u, err := a.createUser(in)
	if err != nil {
		fail(w, err)
		return
	}
	a.respondUser(w, 201, u.ID)
}

func (a *App) hBulkUsers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Prefix   string   `json:"prefix"`
		Count    int      `json:"count"`
		StartAt  int      `json:"startAt"`
		Groups   []string `json:"groups"`
		Platform string   `json:"platform"`
		Team     string   `json:"team"`
		Role     string   `json:"role"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if in.Count < 1 || in.Count > 200 {
		fail(w, invalid("Liczba użytkowników: 1–200."))
		return
	}
	if in.StartAt <= 0 {
		in.StartAt = 1
	}
	in.Prefix = strings.TrimSpace(in.Prefix)
	od, ou := a.onlineSets()
	out := []userJSON{}
	for i := 0; i < in.Count; i++ {
		name := fmt.Sprintf("%s%d", in.Prefix, in.StartAt+i)
		ui := userInput{Username: name, Team: in.Team, Role: in.Role, Groups: in.Groups}
		if in.Platform != "" {
			ui.Devices = append(ui.Devices, struct {
				Platform string `json:"platform"`
				Name     string `json:"name"`
			}{Platform: in.Platform})
		}
		u, err := a.createUser(ui)
		if err != nil {
			fail(w, fmt.Errorf("%s: %w", name, err))
			return
		}
		uj, _ := a.userView(u, od, ou)
		out = append(out, uj)
	}
	writeJSON(w, 201, out)
}

func (a *App) hUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUser(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in userInput
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	me := r.Context().Value(userKey).(*store.User)
	if n := strings.TrimSpace(in.Username); n != "" && !strings.EqualFold(n, u.Username) {
		if !reUsername.MatchString(n) {
			fail(w, invalid("Niepoprawna nazwa użytkownika."))
			return
		}
		if _, err := a.store.GetUserByName(n); err == nil {
			fail(w, invalid("Taka nazwa już istnieje."))
			return
		}
		u.Username = n
	}
	if in.Callsign != "" {
		u.Callsign = strings.TrimSpace(in.Callsign)
	}
	if in.Team != "" {
		u.Team = in.Team
	}
	if in.Role != "" {
		u.Role = in.Role
	}
	if in.Password != "" {
		if len(in.Password) < 8 {
			fail(w, invalid("Hasło musi mieć co najmniej 8 znaków."))
			return
		}
		h, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		u.PasswordHash = string(h)
		_ = a.store.DeleteUserSessions(u.ID)
	}
	if in.IsAdmin != nil {
		if !*in.IsAdmin && u.ID == me.ID {
			fail(w, invalid("Nie możesz odebrać uprawnień administratora samemu sobie."))
			return
		}
		u.IsAdmin = *in.IsAdmin
		if u.IsAdmin && u.PasswordHash == "" {
			fail(w, invalid("Administrator musi mieć hasło."))
			return
		}
	}
	if in.Disabled != nil {
		if *in.Disabled && u.ID == me.ID {
			fail(w, invalid("Nie możesz zablokować własnego konta."))
			return
		}
		u.Disabled = *in.Disabled
	}
	if err := a.store.UpdateUser(u); err != nil {
		fail(w, err)
		return
	}
	if in.Groups != nil {
		if err := a.store.SetUserGroups(u.ID, cleanGroups(in.Groups)); err != nil {
			fail(w, err)
			return
		}
	}
	groups, _ := a.store.UserGroups(u.ID)
	a.router.UpdateUser(u.ID, groups, u.Disabled)
	a.respondUser(w, 200, u.ID)
}

func (a *App) hDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	me := r.Context().Value(userKey).(*store.User)
	if id == me.ID {
		fail(w, invalid("Nie możesz usunąć własnego konta."))
		return
	}
	if _, err := a.store.GetUser(id); err != nil {
		fail(w, err)
		return
	}
	_ = a.store.RevokeUserCerts(id)
	a.router.UpdateUser(id, nil, true)
	if err := a.store.DeleteUser(id); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hAddDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUser(id)
	if err != nil {
		fail(w, err)
		return
	}
	var in struct{ Platform, Name string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	d, err := a.addDevice(u, in.Platform, in.Name)
	if err != nil {
		fail(w, err)
		return
	}
	od, _ := a.onlineSets()
	writeJSON(w, 201, a.deviceView(u, d, od))
}

func (a *App) hDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := a.store.GetDevice(id); err != nil {
		fail(w, err)
		return
	}
	_ = a.store.RevokeDeviceCerts(id)
	a.router.DisconnectWhere(func(c *streamClient) bool { return c.Identity.DeviceID == id })
	if err := a.store.DeleteDevice(id); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hNewToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	d, err := a.store.GetDevice(id)
	if err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUser(d.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := a.newToken(d.ID); err != nil {
		fail(w, err)
		return
	}
	od, _ := a.onlineSets()
	writeJSON(w, 200, a.deviceView(u, d, od))
}

// buildPackage issues a fresh client certificate for the device and returns its data package.
func (a *App) buildPackage(u *store.User, d *store.Device, platform string) ([]byte, string, error) {
	cfg := a.cfg.Get()
	if !cfg.SetupDone {
		return nil, "", errors.New("serwer nie jest jeszcze skonfigurowany")
	}
	cert, key, err := a.pki.IssueClient(u.Username, time.Duration(cfg.CertValidityDays)*24*time.Hour)
	if err != nil {
		return nil, "", err
	}
	if err := a.recordClientCert(cert, u.ID, d.ID); err != nil {
		return nil, "", err
	}
	clientP12, err := a.pki.ClientP12(cert, key, cfg.CertPassword)
	if err != nil {
		return nil, "", err
	}
	trust, err := a.pki.TrustStoreP12(cfg.CertPassword)
	if err != nil {
		return nil, "", err
	}
	zip, err := datapkg.Connection(datapkg.Params{
		Platform: platform, Config: cfg, Username: u.Username, Callsign: u.Callsign, Team: u.Team, Role: u.Role,
		ClientP12: clientP12, TrustP12: trust,
	})
	if err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("%s-%s-%s.zip", sanitizeFile(cfg.DisplayName), sanitizeFile(u.Username), platform)
	return zip, name, nil
}

func sanitizeFile(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "tak"
	}
	return b.String()
}

func sendZip(w http.ResponseWriter, name string, data []byte) {
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (a *App) hDevicePackage(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	d, err := a.store.GetDevice(id)
	if err != nil {
		fail(w, err)
		return
	}
	u, err := a.store.GetUser(d.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	data, name, err := a.buildPackage(u, d, d.Platform)
	if err != nil {
		fail(w, err)
		return
	}
	sendZip(w, name, data)
}

// ---------- join page (public) ----------

func (a *App) lookupToken(tok string) (*store.Token, *store.Device, *store.User, string) {
	t, err := a.store.GetToken(strings.ToUpper(strings.TrimSpace(tok)))
	if err != nil {
		return nil, nil, nil, "unknown"
	}
	if time.Now().After(t.ExpiresAt) {
		return nil, nil, nil, "expired"
	}
	if !tokenUsable(t) {
		return nil, nil, nil, "used"
	}
	d, err := a.store.GetDevice(t.DeviceID)
	if err != nil {
		return nil, nil, nil, "unknown"
	}
	u, err := a.store.GetUser(d.UserID)
	if err != nil || u.Disabled {
		return nil, nil, nil, "unknown"
	}
	return t, d, u, ""
}

func (a *App) hJoin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.limiter.blocked(ip) {
		httpError(w, http.StatusTooManyRequests, "Zbyt wiele prób. Spróbuj za kilka minut.")
		return
	}
	t, d, u, reason := a.lookupToken(r.PathValue("token"))
	if reason != "" {
		a.limiter.fail(ip)
		writeJSON(w, 200, map[string]any{"valid": false, "reason": reason})
		return
	}
	cfg := a.cfg.Get()
	// The device's own platform first, then the other enabled platforms as alternatives.
	platforms := []string{d.Platform}
	for _, p := range cfg.Platforms {
		if p != d.Platform {
			platforms = append(platforms, p)
		}
	}
	var options []map[string]any
	for _, p := range platforms {
		options = append(options, map[string]any{
			"platform":   p,
			"label":      config.PlatformLabel(p),
			"packageUrl": "/api/join/" + t.Token + "/package/" + p,
			"nativeQr":   datapkg.NativeQR(p, cfg, u.Username, t.Token),
			"manual": map[string]any{
				"host": cfg.ServerName, "port": cfg.Ports.CoTTLS, "enrollPort": cfg.Ports.Enroll,
				"username": u.Username, "password": t.Token,
			},
		})
	}
	writeJSON(w, 200, map[string]any{
		"valid": true, "expiresAt": t.ExpiresAt, "displayName": cfg.DisplayName, "serverName": cfg.ServerName,
		"user":    map[string]string{"username": u.Username, "callsign": u.Callsign},
		"device":  map[string]any{"id": d.ID, "name": d.Name, "platform": d.Platform},
		"options": options,
	})
}

func (a *App) hJoinPackage(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if a.limiter.blocked(ip) {
		httpError(w, http.StatusTooManyRequests, "Zbyt wiele prób. Spróbuj za kilka minut.")
		return
	}
	t, d, u, reason := a.lookupToken(r.PathValue("token"))
	if reason != "" {
		a.limiter.fail(ip)
		httpError(w, http.StatusGone, "Link jest nieważny lub został już wykorzystany. Poproś administratora o nowy.")
		return
	}
	platform := r.PathValue("platform")
	if !config.ValidPlatform(platform) {
		httpError(w, 400, "Nieznana platforma.")
		return
	}
	data, name, err := a.buildPackage(u, d, platform)
	if err != nil {
		fail(w, err)
		return
	}
	_ = a.store.MarkTokenUsed(t.Token)
	sendZip(w, name, data)
}
