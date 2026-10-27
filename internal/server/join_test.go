package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"chameleontak/internal/config"
)

// Phones do not trust the server's own CA, so the join page, its API and the package stay on
// plain HTTP even with "force HTTPS", and ATAK's import link points at the address the phone used.
func TestJoinStaysOnHTTPForPhones(t *testing.T) {
	app := newTestServer(t)
	if _, err := app.cfg.Update(func(c *config.Config) error { c.ForceHTTPS = true; return nil }); err != nil {
		t.Fatal(err)
	}
	u, err := app.createUser(userInput{Username: "jan"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := app.addDevice(u, "atak", "")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := app.store.LatestToken(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := app.panelHandler(false)
	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "http://10.42.0.1"+path, nil))
		return rec
	}

	if rec := do(http.MethodGet, "/"); rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("panel over HTTP: status %d, want a redirect to HTTPS", rec.Code)
	}
	for _, p := range []string{"/join/" + tok.Token, "/api/status", "/api/join/" + tok.Token} {
		if rec := do(http.MethodGet, p); rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", p, rec.Code)
		}
	}

	if dv := app.deviceView(u, d, nil); !strings.HasPrefix(dv.Enrollment.JoinURL, "http://127.0.0.1") {
		t.Fatalf("join URL %q must use plain HTTP", dv.Enrollment.JoinURL)
	}

	var info struct {
		Options []struct {
			Platform string `json:"platform"`
			NativeQR string `json:"nativeQr"`
		} `json:"options"`
	}
	if err := json.Unmarshal(do(http.MethodGet, "/api/join/"+tok.Token).Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	q := info.Options[0].NativeQR
	if info.Options[0].Platform != "atak" || !strings.Contains(q, url.QueryEscape("http://10.42.0.1")) || !strings.HasSuffix(q, "atak.zip") {
		t.Fatalf("ATAK import link %q must download from the address the phone used", q)
	}

	if rec := do(http.MethodHead, "/api/join/"+tok.Token+"/atak.zip"); rec.Code != http.StatusOK {
		t.Fatalf("HEAD package: status %d", rec.Code)
	}
	if t2, err := app.store.GetToken(tok.Token); err != nil || !t2.UsedAt.IsZero() {
		t.Fatalf("HEAD must not use up the token (err %v)", err)
	}
	rec := do(http.MethodGet, "/api/join/"+tok.Token+"/atak.zip")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("package: status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
