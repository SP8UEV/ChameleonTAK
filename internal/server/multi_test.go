package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"chameleontak/internal/config"
)

// panelCall sends an API request through the main server's panel as the logged-in admin.
func panelCall(t *testing.T, app *App, cookie *http.Cookie, method, path, server, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if server != "" {
		req.Header.Set(serverHeader, server)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	app.panelHandler(false).ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func adminCookie(t *testing.T, app *App) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/login", strings.NewReader(`{"username":"admin","password":"supertajne"}`))
	req.Header.Set("Content-Type", "application/json")
	app.panelHandler(false).ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	return nil
}

func TestAdditionalServer(t *testing.T) {
	app := newTestServer(t)
	app.others = &serverSet{ctx: t.Context()}
	cookie := adminCookie(t, app)
	mainPorts := app.cfg.Get().Ports

	// Ports of the main server are refused.
	clash := `{"id":"druzyna-b","displayName":"Drużyna B","ports":{"cotTls":` + itoa(mainPorts.CoTTLS) + `,"marti":18443,"enroll":18446}}`
	clash = strings.Replace(clash, "}}", `},"platforms":["atak"]}`, 1)
	if code, out := panelCall(t, app, cookie, "POST", "/api/servers", "", clash); code != 400 || !strings.Contains(out["error"].(string), "zajęty") {
		t.Fatalf("port clash: %d %v", code, out)
	}

	p := freePorts(t)
	body := `{"id":"druzyna-b","displayName":"Drużyna B","platforms":["atak"],"ports":{"cotTls":` + itoa(p.CoTTLS) + `,"marti":` + itoa(p.Marti) + `,"enroll":` + itoa(p.Enroll) + `}}`
	code, out := panelCall(t, app, cookie, "POST", "/api/servers", "", body)
	if code != 201 {
		t.Fatalf("create: %d %v", code, out)
	}
	b := app.serverByID("druzyna-b")
	if b == nil {
		t.Fatal("server not registered")
	}
	t.Cleanup(func() {
		app.others.mu.RLock()
		list := app.others.list
		app.others.mu.RUnlock()
		for _, rs := range list {
			rs.cancel()
			<-rs.done
		}
	})
	waitFor(t, "listeners of the new server", func() bool {
		for _, s := range b.listenerStates() {
			if s.State != "running" {
				return false
			}
		}
		return len(b.listenerStates()) == 3 // CoT TLS, Marti, enrollment — no panel of its own
	})
	if b.cfg.Get().Ports.HTTP != mainPorts.HTTP || b.pki.CACert().Equal(app.pki.CACert()) {
		t.Fatal("the new server needs the panel ports and a CA of its own")
	}

	// The header picks the server; without a session nothing is handed over.
	if code, out := panelCall(t, app, cookie, "GET", "/api/settings", "druzyna-b", ""); code != 200 || out["displayName"] != "Drużyna B" || out["main"] != false {
		t.Fatalf("settings of druzyna-b: %d %v", code, out)
	}
	if code, _ := panelCall(t, app, nil, "GET", "/api/settings", "druzyna-b", ""); code != 401 {
		t.Fatalf("without a session: %d", code)
	}
	if code, out := panelCall(t, app, cookie, "POST", "/api/users", "druzyna-b", `{"username":"pilot","callsign":"PILOT"}`); code != 201 && code != 200 {
		t.Fatalf("create user on druzyna-b: %d %v", code, out)
	}
	if _, err := b.store.GetUserByName("pilot"); err != nil {
		t.Fatal("user must land in druzyna-b's database")
	}
	if _, err := app.store.GetUserByName("pilot"); err == nil {
		t.Fatal("user leaked into the main server")
	}

	// A client of the new server connects to its own CoT port.
	c := dialCoT(t, b, clientTLS(t, b, "pilot2", nil))
	c.send(t, saXML("UID-B", "BRAVO"))
	waitUIDs(t, b, "UID-B")
	if app.router.Count() != 0 {
		t.Fatal("client counted on the main server")
	}

	// Aircraft can go to the additional server.
	if _, err := app.cfg.Update(func(c *config.Config) error { c.ADSB.Servers = []string{"druzyna-b"}; return nil }); err != nil {
		t.Fatal(err)
	}

	if code, out := panelCall(t, app, cookie, "DELETE", "/api/servers/druzyna-b", "", ""); code != 200 {
		t.Fatalf("delete: %d %v", code, out)
	}
	if app.serverByID("druzyna-b") != nil {
		t.Fatal("still registered")
	}
	if _, err := os.Stat(app.DataPath(serversDir, "druzyna-b")); err == nil {
		t.Fatal("data directory still in servers/")
	}
}
