package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"chameleontak/internal/config"
)

func TestAccessEntries(t *testing.T) {
	for in, want := range map[string]string{
		" 192.168.1.20 ":    "192.168.1.20",
		"192.168.1.7/24":    "192.168.1.0/24",
		"AA-BB-CC-DD-EE-FF": "aa:bb:cc:dd:ee:ff",
		"2001:db8::1":       "2001:db8::1",
	} {
		got, err := normalizeAccessEntry(in)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"tak.example.com", "192.168.1.300", "aa:bb"} {
		if _, err := normalizeAccessEntry(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAccessAllowed(t *testing.T) {
	pa := config.PanelAccess{Restrict: true, Allow: []string{"192.168.1.20", "10.0.0.0/8", "aa:bb:cc:dd:ee:ff"}}
	macs := map[string]string{"192.168.1.55": "aa:bb:cc:dd:ee:ff", "192.168.1.56": "11:22:33:44:55:66"}
	macOf := func(ip string) string { return macs[ip] }
	for ip, want := range map[string]bool{
		"192.168.1.20":    true,
		"10.42.0.9":       true,
		"192.168.1.55":    true, // by MAC
		"192.168.1.56":    false,
		"203.0.113.9":     false,
		"127.0.0.1":       true, // the Pi itself
		"::ffff:10.1.1.1": true,
	} {
		if got := accessAllowed(pa, ip, macOf); got != want {
			t.Errorf("%s: %v, want %v", ip, got, want)
		}
	}
	if !accessAllowed(config.PanelAccess{}, "203.0.113.9", macOf) {
		t.Error("an unrestricted panel must be open")
	}
}

// With the access list on, the panel refuses other addresses but phones still open join pages.
func TestPanelAccessList(t *testing.T) {
	app := newTestServer(t)
	if _, err := app.cfg.Update(func(c *config.Config) error {
		c.PanelAccess = config.PanelAccess{Restrict: true, Allow: []string{"192.168.1.20"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h := app.panelHandler(false)
	get := func(path, remote string) int {
		req := httptest.NewRequest(http.MethodGet, "http://192.168.1.102"+path, nil)
		req.RemoteAddr = remote + ":5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := get("/api/dashboard", "192.168.1.30"); code != http.StatusForbidden {
		t.Fatalf("API from another address: %d", code)
	}
	if code := get("/", "192.168.1.30"); code != http.StatusForbidden {
		t.Fatalf("panel page from another address: %d", code)
	}
	if code := get("/api/dashboard", "192.168.1.20"); code != http.StatusUnauthorized {
		t.Fatalf("API from the allowed address: %d, want the login check", code)
	}
	if code := get("/api/join/nope", "192.168.1.30"); code == http.StatusForbidden {
		t.Fatal("join API blocked by the panel access list")
	}
}
