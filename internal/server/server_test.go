package server

import (
	"bufio"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"chameleontak/internal/config"
	"chameleontak/internal/cot"
)


// newTestServer runs the setup and starts all services on free local ports.
func newTestServer(t *testing.T) *App {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := New(Options{DataDir: t.TempDir(), HTTPAddr: "127.0.0.1:0", Version: "test"}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.stopServices()
		app.router.CloseAll()
		app.Close()
	})
	var req SetupRequest
	req.ServerName = "127.0.0.1"
	req.Ports = freePorts(t)
	req.Admin.Username, req.Admin.Password = "admin", "supertajne"
	req.Platforms = []string{"atak", "itak"}
	req.Network.Mode = "lan"
	if err := app.validateSetup(&req); err != nil {
		t.Fatal(err)
	}
	app.cliMode = true
	if err := app.RunSetup(req, nil); err != nil {
		t.Fatal(err)
	}
	app.cliMode = false
	// A port probed as free can be handed out as an ephemeral source port before we bind it
	// (common on Linux CI), so on a bind failure pick fresh ports and try again.
	var failed string
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			ports := freePorts(t)
			if _, err := app.cfg.Update(func(c *config.Config) error { c.Ports = ports; return nil }); err != nil {
				t.Fatal(err)
			}
		}
		app.startServices()
		failed = ""
		for _, s := range app.listenerStates() {
			if s.State == "error" {
				failed = s.Name + ": " + s.Error
			}
		}
		if failed == "" {
			return app
		}
		app.stopServices()
	}
	t.Fatalf("listener %s", failed)
	return nil
}

// freePorts reserves six distinct ports at once (so none is returned twice) and releases them.
func freePorts(t *testing.T) config.Ports {
	t.Helper()
	var ps []int
	var lns []net.Listener
	for len(ps) < 6 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		lns = append(lns, ln)
		ps = append(ps, ln.Addr().(*net.TCPAddr).Port)
	}
	for _, ln := range lns {
		ln.Close()
	}
	return config.Ports{CoTTLS: ps[0], CoTTCP: ps[1], Marti: ps[2], Enroll: ps[3], HTTP: ps[4], HTTPS: ps[5]}
}

// clientTLS issues a certificate for a new user/device and returns a TLS config using it.
func clientTLS(t *testing.T, app *App, username string, groups []string) *tls.Config {
	t.Helper()
	u, err := app.createUser(userInput{Username: username, Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	d, err := app.addDevice(u, "atak", "test")
	if err != nil {
		t.Fatal(err)
	}
	cert, key, err := app.pki.IssueClient(u.Username, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.recordClientCert(cert, u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{cert.Raw}, PrivateKey: key}},
		RootCAs:      app.pki.CAPool(),
		ServerName:   "127.0.0.1",
	}
}

type testClient struct {
	conn *tls.Conn
	fr   *cot.Framer
}

func dialCoT(t *testing.T, app *App, cfg *tls.Config) *testClient {
	t.Helper()
	addr := net.JoinHostPort("127.0.0.1", itoa(app.cfg.Get().Ports.CoTTLS))
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{conn: conn, fr: cot.NewFramer(bufio.NewReader(conn))}
}

func (c *testClient) send(t *testing.T, s string) {
	t.Helper()
	if _, err := c.conn.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
}

// expect reads events until one contains want (or times out).
func (c *testClient) expect(t *testing.T, want string) {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		raw, err := c.fr.Next()
		if err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if strings.Contains(string(raw), want) {
			return
		}
	}
}

func (c *testClient) expectNothing(t *testing.T, notWant string) {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	for {
		raw, err := c.fr.Next()
		if err != nil {
			return
		}
		if strings.Contains(string(raw), notWant) {
			t.Fatalf("unexpected event containing %q", notWant)
		}
	}
}

func saXML(uid, callsign string) string {
	now := time.Now().UTC()
	return `<event version="2.0" uid="` + uid + `" type="a-f-G-U-C" how="m-g" time="` + cot.FormatTime(now) + `" start="` + cot.FormatTime(now) +
		`" stale="` + cot.FormatTime(now.Add(5*time.Minute)) + `"><point lat="52.2" lon="21.0" hae="100" ce="10" le="10"/><detail>` +
		`<takv os="34" version="5.4" device="TEST" platform="ATAK-CIV"/><contact endpoint="*:-1:stcp" callsign="` + callsign + `"/>` +
		`<__group role="Team Member" name="Cyan"/><status battery="77"/></detail></event>`
}

func TestStreamRoutingAndPing(t *testing.T) {
	app := newTestServer(t)
	alfa := dialCoT(t, app, clientTLS(t, app, "alfa", nil))
	alfa.send(t, saXML("UID-ALFA", "ALFA"))
	waitUIDs(t, app, "UID-ALFA")

	// A newcomer immediately receives the cached SA of everyone online.
	bravo := dialCoT(t, app, clientTLS(t, app, "bravo", nil))
	bravo.expect(t, `callsign="ALFA"`)

	bravo.send(t, saXML("UID-BRAVO", "BRAVO"))
	alfa.expect(t, `callsign="BRAVO"`)

	// Ping is answered with a pong and not relayed.
	alfa.send(t, `<event version="2.0" uid="UID-ALFA-ping" type="t-x-c-t" how="m-g" time="2026-10-25T10:00:00Z" start="2026-10-25T10:00:00Z" stale="2026-10-25T10:00:10Z"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail/></event>`)
	alfa.expect(t, `t-x-c-t-r`)
	bravo.expectNothing(t, `t-x-c-t`)

	if n := app.router.Count(); n != 2 {
		t.Fatalf("expected 2 clients, got %d", n)
	}
	// The map knows both contacts.
	waitFor(t, "two contacts on the map", func() bool { return len(app.pos.List()) >= 2 })
	found := 0
	for _, p := range app.pos.List() {
		if p.Kind == "contact" && p.Online {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("expected 2 online contacts on the map, got %d", found)
	}
}

func TestGroupsIsolateTraffic(t *testing.T) {
	app := newTestServer(t)
	red1 := dialCoT(t, app, clientTLS(t, app, "red1", []string{"Czerwoni"}))
	red2 := dialCoT(t, app, clientTLS(t, app, "red2", []string{"Czerwoni"}))
	blue := dialCoT(t, app, clientTLS(t, app, "blue", []string{"Niebiescy"}))
	waitClients(t, app, 3)

	red1.send(t, saXML("UID-RED1", "RED1"))
	red2.expect(t, `callsign="RED1"`)
	blue.expectNothing(t, `callsign="RED1"`)
}

func TestDirectMessageByCallsign(t *testing.T) {
	app := newTestServer(t)
	a := dialCoT(t, app, clientTLS(t, app, "a1", nil))
	b := dialCoT(t, app, clientTLS(t, app, "b1", nil))
	c := dialCoT(t, app, clientTLS(t, app, "c1", nil))
	a.send(t, saXML("UID-A", "A1"))
	b.send(t, saXML("UID-B", "B1"))
	c.send(t, saXML("UID-C", "C1"))
	waitUIDs(t, app, "UID-A", "UID-B", "UID-C")

	msg := `<event version="2.0" uid="GeoChat.UID-A.UID-B.x1" type="b-t-f" how="h-g-i-g-o" time="2026-10-25T10:00:00Z" start="2026-10-25T10:00:00Z" stale="2030-10-26T10:00:00Z"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail><__chat parent="RootContactGroup" groupOwner="false" messageId="x1" chatroom="B1" id="UID-B" senderCallsign="A1"><chatgrp uid0="UID-A" uid1="UID-B" id="UID-B"/></__chat><remarks source="BAO.F.ATAK.UID-A" to="UID-B" time="2026-10-25T10:00:00Z">tajne</remarks><marti><dest callsign="B1"/></marti></detail></event>`
	a.send(t, msg)
	b.expect(t, ">tajne<")
	c.expectNothing(t, ">tajne<")

	chat, _ := app.store.ListChat(10)
	if len(chat) != 1 || chat[0].Text != "tajne" || chat[0].From != "A1" {
		t.Fatalf("chat log: %+v", chat)
	}
}

func TestRevokedCertificateRejected(t *testing.T) {
	app := newTestServer(t)
	cfg := clientTLS(t, app, "zly", nil)
	certs, _ := app.store.ListCerts()
	for _, c := range certs {
		if c.CN == "zly" {
			_ = app.store.RevokeCert(c.Serial)
		}
	}
	addr := net.JoinHostPort("127.0.0.1", itoa(app.cfg.Get().Ports.CoTTLS))
	conn, err := tls.Dial("tcp", addr, cfg)
	if err != nil {
		return // rejected during handshake: fine
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("revoked certificate was able to stay connected")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// waitFor polls cond instead of sleeping a fixed time, so tests stay reliable on slow CI runners.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitUIDs waits until the router knows a connection for every UID (its SA was processed).
func waitUIDs(t *testing.T, app *App, uids ...string) {
	t.Helper()
	waitFor(t, "UIDs "+strings.Join(uids, ","), func() bool {
		for _, u := range uids {
			if app.router.ClientByUID(u) == nil {
				return false
			}
		}
		return true
	})
}

func waitClients(t *testing.T, app *App, n int) {
	t.Helper()
	waitFor(t, "connected clients", func() bool { return app.router.Count() >= n })
}
