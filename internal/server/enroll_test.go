package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func martiURL(app *App, port int, path string) string {
	return "https://127.0.0.1:" + itoa(port) + path
}

func httpsClient(app *App, certs ...tls.Certificate) *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			RootCAs: app.pki.CAPool(), ServerName: "127.0.0.1", Certificates: certs,
		}},
	}
}

func TestEnrollmentWithTokenThenStream(t *testing.T) {
	app := newTestServer(t)
	u, err := app.createUser(userInput{Username: "jan"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := app.addDevice(u, "atak", "Telefon")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := app.store.LatestToken(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	port := app.cfg.Get().Ports.Enroll
	cl := httpsClient(app)

	// 1. /tls/config with Basic auth (username + one-time token)
	req, _ := http.NewRequest("GET", martiURL(app, port, "/Marti/api/tls/config"), nil)
	req.SetBasicAuth("jan", tok.Token)
	resp, err := cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "nameEntry") {
		t.Fatalf("tls/config: %d %s", resp.StatusCode, body)
	}

	// Wrong password is rejected.
	req, _ = http.NewRequest("GET", martiURL(app, port, "/Marti/api/tls/config"), nil)
	req.SetBasicAuth("jan", "ZLEHASLO")
	resp, _ = cl.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 for bad token, got %d", resp.StatusCode)
	}

	// 2. signClient/v2 with a CSR sent as bare base64 (ATAK style) — XML answer.
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	csrDER, _ := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "ktos-inny"}}, key)
	csrB64 := base64.StdEncoding.EncodeToString(csrDER)
	req, _ = http.NewRequest("POST", martiURL(app, port, "/Marti/api/tls/signClient/v2?clientUid=ANDROID-123&version=5.4.0"), strings.NewReader(csrB64))
	req.SetBasicAuth("jan", tok.Token)
	req.Header.Set("Accept", "application/xml")
	resp, err = cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("signClient: %d %s", resp.StatusCode, body)
	}
	var enr struct {
		SignedCert string `xml:"signedCert"`
		CA0        string `xml:"ca"`
	}
	if err := xml.Unmarshal(body, &enr); err != nil {
		t.Fatalf("enrollment xml: %v\n%s", err, body)
	}
	der, _ := base64.StdEncoding.DecodeString(strings.ReplaceAll(enr.SignedCert, "\n", ""))
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "jan" {
		t.Fatalf("CN must be forced to the username, got %q", cert.Subject.CommonName)
	}

	// 3. JSON variant (iTAK style) with a PEM CSR, still inside the token grace period.
	pemCSR := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	req, _ = http.NewRequest("POST", martiURL(app, port, "/Marti/api/tls/signClient/v2?clientUid=X"), strings.NewReader(string(pemCSR)))
	req.SetBasicAuth("jan", tok.Token)
	req.Header.Set("Accept", "application/json")
	resp, err = cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var js map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&js)
	resp.Body.Close()
	if js["signedCert"] == "" || js["ca0"] == "" {
		t.Fatalf("json enrollment: %v", js)
	}

	// 4. The enrolled certificate works on the CoT port and is bound to the device.
	c := dialCoT(t, app, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key}},
		RootCAs:      app.pki.CAPool(), ServerName: "127.0.0.1",
	})
	c.send(t, saXML("ANDROID-123", "JAN"))
	time.Sleep(200 * time.Millisecond)
	clients := app.router.Clients()
	if len(clients) != 1 || clients[0].DeviceID != d.ID || clients[0].Username != "jan" {
		t.Fatalf("unexpected clients: %+v", clients)
	}

	// 5. The Marti API on 8443 accepts the client certificate.
	mc := httpsClient(app, tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: key})
	resp, err = mc.Get(martiURL(app, app.cfg.Get().Ports.Marti, "/Marti/api/version/config"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ServerConfig") {
		t.Fatalf("version/config: %d %s", resp.StatusCode, body)
	}
}

func TestDataPackageUploadAndSearch(t *testing.T) {
	app := newTestServer(t)
	cfg := clientTLS(t, app, "pliki", nil)
	cl := httpsClient(app, cfg.Certificates...)
	port := app.cfg.Get().Ports.Marti

	resp, err := cl.Post(martiURL(app, port, "/Marti/sync/missionupload?hash=x&filename=paczka.zip&creatorUid=U1"), "application/octet-stream", strings.NewReader("PK-zawartosc"))
	if err != nil {
		t.Fatal(err)
	}
	urlBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(urlBody), "/Marti/sync/content?hash=") {
		t.Fatalf("upload: %d %s", resp.StatusCode, urlBody)
	}
	resp, err = cl.Get(martiURL(app, port, "/Marti/sync/search?keywords=missionpackage"))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		ResultCount int `json:"resultCount"`
		Results     []struct{ Hash, Name string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	if res.ResultCount != 1 || res.Results[0].Name != "paczka.zip" {
		t.Fatalf("search: %+v", res)
	}
	resp, err = cl.Get(martiURL(app, port, "/Marti/sync/content?hash="+res.Results[0].Hash))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(data) != "PK-zawartosc" {
		t.Fatalf("download mismatch: %q", data)
	}
}
