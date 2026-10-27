package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chameleontak/internal/config"
)

func TestAccessMode(t *testing.T) {
	for name, want := range map[string]string{
		"tak.example.com":    "domain",
		"203.0.113.5":        "ip",
		"2a01:4f8::1":        "ip",
		"192.168.1.102":      "local",
		"10.42.0.1":          "local",
		"100.72.1.1":         "local", // carrier-grade NAT
		"127.0.0.1":          "local",
		"chameleontak.local": "local",
	} {
		if got := accessMode(config.Config{ServerName: name}); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

func TestLetsEncryptNeedsPublicName(t *testing.T) {
	le := config.LetsEncrypt{Enabled: true}
	if validateLetsEncrypt("192.168.1.102", le) == nil {
		t.Fatal("Let's Encrypt accepted for a LAN address")
	}
	for _, name := range []string{"203.0.113.5", "tak.example.com"} {
		if err := validateLetsEncrypt(name, le); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

// Let's Encrypt validates a public IP address over plain HTTP; the token must be answered even
// with "force HTTPS" on.
func TestHTTP01ChallengeAnswered(t *testing.T) {
	app := newTestServer(t)
	if _, err := app.cfg.Update(func(c *config.Config) error { c.ForceHTTPS = true; return nil }); err != nil {
		t.Fatal(err)
	}
	app.public.setToken("tok123", "tok123.thumb")
	rec := httptest.NewRecorder()
	app.httpHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://203.0.113.5/.well-known/acme-challenge/tok123", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "tok123.thumb" {
		t.Fatalf("challenge response %d %q", rec.Code, rec.Body.String())
	}
}

// Without a certificate yet, the public address still gets plain HTTP join links; a phone that
// opened the page by the LAN address keeps that address for the download.
func TestJoinBaseURLPublicAddress(t *testing.T) {
	app := newTestServer(t)
	if _, err := app.cfg.Update(func(c *config.Config) error {
		c.ServerName, c.LetsEncrypt.Enabled = "203.0.113.5", true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := app.joinBaseURL(""); !strings.HasPrefix(got, "http://203.0.113.5") {
		t.Fatalf("join base %q, want plain HTTP until the certificate exists", got)
	}
	if got := app.joinBaseURL("192.168.1.102"); !strings.HasPrefix(got, "http://192.168.1.102") {
		t.Fatalf("LAN join base %q", got)
	}
}
