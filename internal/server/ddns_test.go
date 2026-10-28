package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chameleontak/internal/config"
)

func TestDuckDNSName(t *testing.T) {
	for in, want := range map[string]string{"mojtak.duckdns.org": "mojtak", "a.mojtak.duckdns.org": "mojtak", "duckdns.org": "", "tak.example.com": ""} {
		if got := duckDNSName(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
	if validateDDNS("192.168.1.102", config.DDNS{Provider: "duckdns", Token: "x"}) == nil {
		t.Error("DDNS accepted without a domain")
	}
	if validateDDNS("tak.example.com", config.DDNS{Provider: "duckdns", Token: "x"}) == nil {
		t.Error("DuckDNS accepted for a domain outside duckdns.org")
	}
	if err := validateDDNS("tak.example.com", config.DDNS{Provider: "url", URL: "https://dyn.example/update?ip={ip}"}); err != nil {
		t.Error(err)
	}
}

func TestDDNSUpdateURL(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		if r.URL.Query().Get("pass") != "dobre" {
			_, _ = w.Write([]byte("badauth"))
			return
		}
		_, _ = w.Write([]byte("good 203.0.113.7"))
	}))
	defer srv.Close()
	ctx := t.Context()
	d := config.DDNS{Provider: "url", URL: srv.URL + "/nic/update?hostname={domain}&myip={ip}&pass=dobre"}
	if _, err := ddnsSend(ctx, d, "tak.example.com", "203.0.113.7"); err != nil {
		t.Fatal(err)
	}
	if got != "hostname=tak.example.com&myip=203.0.113.7&pass=dobre" {
		t.Fatalf("query %q", got)
	}
	d.URL = srv.URL + "/nic/update?myip={ip}&pass=zle"
	if _, err := ddnsSend(ctx, d, "tak.example.com", "203.0.113.7"); err == nil || strings.Contains(err.Error(), "pass=") {
		t.Fatalf("badauth must fail without leaking the URL: %v", err)
	}
}
