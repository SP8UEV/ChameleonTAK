package server

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	pkcs12 "software.sslmate.com/src/go-pkcs12"
)

// ATAK negotiates TLS 1.2 without ECDHE ("DEFAULT:!ECDH"); the handshake must still succeed.
func TestTLS12WithoutECDHE(t *testing.T) {
	app := newTestServer(t)
	cfg := clientTLS(t, app, "starytel", nil)
	cfg.MaxVersion = tls.VersionTLS12
	cfg.CipherSuites = []uint16{tls.TLS_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_RSA_WITH_AES_256_CBC_SHA}
	conn, err := tls.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(app.cfg.Get().Ports.CoTTLS)), cfg)
	if err != nil {
		t.Fatalf("TLS 1.2 RSA handshake failed: %v", err)
	}
	conn.Close()
}

func TestEnrollmentV1ReturnsP12(t *testing.T) {
	app := newTestServer(t)
	u, _ := app.createUser(userInput{Username: "stary", Password: "haslo1234"})
	_ = u
	csr := newCSR(t)
	req, _ := http.NewRequest("POST", martiURL(app, app.cfg.Get().Ports.Enroll, "/Marti/api/tls/signClient?clientUid=A"), strings.NewReader(csr))
	req.SetBasicAuth("stary", "haslo1234")
	resp, err := httpsClient(app).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("v1: %d %s", resp.StatusCode, body)
	}
	certs, err := pkcs12.DecodeTrustStore(body, "atakatak")
	if err != nil || len(certs) != 2 || certs[0].Subject.CommonName != "stary" {
		t.Fatalf("v1 p12: %v %d", err, len(certs))
	}
}

func TestDataPackagesPerPlatform(t *testing.T) {
	app := newTestServer(t)
	u, _ := app.createUser(userInput{Username: "ola", Callsign: "OLA"})
	d, _ := app.addDevice(u, "atak", "")
	for platform, want := range map[string][]string{
		"atak":   {"MANIFEST/manifest.xml", "certs/config.pref"},
		"itak":   {"config.pref"},
		"wintak": {"MANIFEST/manifest.xml"},
	} {
		data, _, err := app.buildPackage(u, d, platform)
		if err != nil {
			t.Fatalf("%s: %v", platform, err)
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		p12s := 0
		for _, f := range zr.File {
			names[f.Name] = true
			if strings.HasSuffix(f.Name, ".p12") {
				p12s++
				rc, _ := f.Open()
				raw, _ := io.ReadAll(rc)
				rc.Close()
				if _, _, _, err := pkcs12.DecodeChain(raw, "atakatak"); err != nil {
					if _, err2 := pkcs12.DecodeTrustStore(raw, "atakatak"); err2 != nil {
						t.Fatalf("%s: %s is not a readable p12: %v / %v", platform, f.Name, err, err2)
					}
				}
			}
		}
		for _, n := range want {
			if !names[n] {
				t.Fatalf("%s package lacks %s: %v", platform, n, names)
			}
		}
		if platform != "wintak" && p12s != 2 {
			t.Fatalf("%s: expected 2 p12 files, got %d", platform, p12s)
		}
	}
}

// newCSR returns a CSR the way ATAK 4.6 sends it: PEM with only the armour lines removed.
func newCSR(t *testing.T) string {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "x", Organization: []string{"TAK"}}}, key)
	if err != nil {
		t.Fatal(err)
	}
	p := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	p = strings.Replace(p, "-----BEGIN CERTIFICATE REQUEST-----", "", 1)
	return strings.Replace(p, "-----END CERTIFICATE REQUEST-----", "", 1)
}

func TestProfilePushedAfterPanelChange(t *testing.T) {
	app := newTestServer(t)
	cfg := clientTLS(t, app, "kasia", nil)
	cl := httpsClient(app, cfg.Certificates...)
	url := martiURL(app, app.cfg.Get().Ports.Marti, "/Marti/api/device/profile/connection?syncSecago=60&clientUid=X")
	get := func() (int, []byte) {
		resp, err := cl.Get(url)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, b
	}
	if code, _ := get(); code != 204 {
		t.Fatalf("unchanged profile: expected 204, got %d", code)
	}
	u, _ := app.store.GetUserByName("kasia")
	u.Team = "Red"
	if err := app.store.UpdateUser(u); err != nil {
		t.Fatal(err)
	}
	code, body := get()
	if code != 200 || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("changed profile: expected zip, got %d", code)
	}
	zr, _ := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	found := false
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".pref") {
			rc, _ := f.Open()
			raw, _ := io.ReadAll(rc)
			rc.Close()
			found = strings.Contains(string(raw), ">Red<")
		}
	}
	if !found {
		t.Fatal("profile does not carry the new team")
	}
}
