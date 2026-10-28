package server

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The CA profile must be well-formed XML, carry the CA as a root payload and be reachable over
// plain HTTP (phones do not trust the panel's HTTPS yet).
func TestCAMobileconfig(t *testing.T) {
	app := newTestServer(t)
	rec := httptest.NewRecorder()
	app.httpHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://192.168.1.102/api/ca.mobileconfig", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-apple-aspen-config" {
		t.Fatalf("status %d, type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.Bytes()
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("profile is not valid XML: %v", err)
			}
			break
		}
	}
	if !bytes.Contains(body, []byte("<string>com.apple.security.root</string>")) {
		t.Fatal("no root certificate payload")
	}
	der := base64.StdEncoding.EncodeToString(app.pki.CACert().Raw)
	if !strings.Contains(string(body), "<data>"+der+"</data>") {
		t.Fatal("profile does not carry the CA certificate")
	}
	// Same CA, same identifiers: installing the profile again replaces it.
	if !bytes.Equal(caMobileconfig(app.pki.CACert(), "x"), caMobileconfig(app.pki.CACert(), "x")) {
		t.Fatal("profile identifiers are not stable")
	}
}
