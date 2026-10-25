package server

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// marti performs a request against the Marti port with the given client certificate.
func marti(t *testing.T, app *App, cfg *tls.Config, method, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, martiURL(app, app.cfg.Get().Ports.Marti, path), strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpsClient(app, cfg.Certificates...).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestMissionOwnerAndPasswordCannotBeSpoofed(t *testing.T) {
	app := newTestServer(t)
	ownerCfg := clientTLS(t, app, "wlasciciel", nil)
	evilCfg := clientTLS(t, app, "intruz", nil)
	owner := dialCoT(t, app, ownerCfg)
	owner.send(t, saXML("UID-OWNER", "OWNER"))
	time.Sleep(200 * time.Millisecond)

	if code, body := marti(t, app, ownerCfg, "PUT", "/Marti/api/missions/Tajna?creatorUid=UID-OWNER&password=sekret123", ""); code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	// Posing as the creator's UID must not grant ownership or skip the password.
	if code, _ := marti(t, app, evilCfg, "PUT", "/Marti/api/missions/Tajna/subscription?uid=UID-OWNER", ""); code != 403 {
		t.Fatalf("subscribe as creator UID: expected 403, got %d", code)
	}
	if code, _ := marti(t, app, evilCfg, "PUT", "/Marti/api/missions/Tajna/subscription?uid=UID-EVIL", ""); code != 403 {
		t.Fatalf("subscribe without password: expected 403, got %d", code)
	}
	if code, _ := marti(t, app, evilCfg, "DELETE", "/Marti/api/missions/Tajna?creatorUid=UID-OWNER", ""); code != 403 {
		t.Fatalf("delete as non-owner: expected 403, got %d", code)
	}
	if code, _ := marti(t, app, evilCfg, "GET", "/Marti/api/missions/Tajna/cot", ""); code != 403 {
		t.Fatalf("read protected contents: expected 403, got %d", code)
	}
	// With the password the subscription works, but only as a subscriber.
	code, body := marti(t, app, evilCfg, "PUT", "/Marti/api/missions/Tajna/subscription?uid=UID-EVIL&password=sekret123", "")
	if code != 201 || strings.Contains(body, roleOwner) {
		t.Fatalf("subscribe with password: %d %s", code, body)
	}
	if _, err := app.store.GetMission("Tajna"); err != nil {
		t.Fatal("mission must still exist")
	}
}

func TestCrossGroupIsolationOnMartiAndMissions(t *testing.T) {
	app := newTestServer(t)
	redCfg := clientTLS(t, app, "czerwony", []string{"Czerwoni"})
	blueCfg := clientTLS(t, app, "niebieski", []string{"Niebiescy"})
	red := dialCoT(t, app, redCfg)
	blue := dialCoT(t, app, blueCfg)
	red.send(t, saXML("UID-RED", "RED"))
	blue.send(t, saXML("UID-BLUE", "BLUE"))
	time.Sleep(200 * time.Millisecond)

	// clientEndPoints only lists visible contacts.
	if _, body := marti(t, app, blueCfg, "GET", "/Marti/api/clientEndPoints", ""); strings.Contains(body, "UID-RED") {
		t.Fatal("clientEndPoints leaked a contact from another group")
	}
	// Blue cannot copy Red's live position into its own mission.
	marti(t, app, blueCfg, "PUT", "/Marti/api/missions/Niebieska?creatorUid=UID-BLUE", "")
	marti(t, app, blueCfg, "PUT", "/Marti/api/missions/Niebieska/contents?creatorUid=UID-BLUE", `{"uids":["UID-RED"]}`)
	if _, body := marti(t, app, blueCfg, "GET", "/Marti/api/missions/Niebieska/cot", ""); strings.Contains(body, "UID-RED") {
		t.Fatal("another group's position was copied into a mission")
	}
	// Blue cannot inject markers into Red's mission via the CoT stream.
	marti(t, app, redCfg, "PUT", "/Marti/api/missions/Czerwona?creatorUid=UID-RED", "")
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	blue.send(t, `<event version="2.0" uid="FAKE-1" type="a-h-G" how="h-g-i-g-o" time="`+now+`" start="`+now+`" stale="2030-01-01T00:00:00.000Z"><point lat="1" lon="1" hae="0" ce="9" le="9"/><detail><marti><dest mission="Czerwona"/></marti></detail></event>`)
	red.expectNothing(t, "FAKE-1")
	if items, _ := app.store.MissionItems("Czerwona"); len(items) != 0 {
		t.Fatal("cross-group CoT was stored in a mission")
	}
	// Direct messages do not cross groups either.
	blue.send(t, `<event version="2.0" uid="DM-1" type="b-t-f" how="h-g-i-g-o" time="`+now+`" start="`+now+`" stale="2030-01-01T00:00:00.000Z"><point lat="0" lon="0" hae="0" ce="9" le="9"/><detail><__chat chatroom="RED" id="UID-RED" senderCallsign="BLUE"/><remarks>hej</remarks><marti><dest callsign="RED"/></marti></detail></event>`)
	red.expectNothing(t, "DM-1")
}

func TestUIDHijackRejected(t *testing.T) {
	app := newTestServer(t)
	victim := dialCoT(t, app, clientTLS(t, app, "ofiara", nil))
	victim.send(t, saXML("UID-VICTIM", "OFIARA"))
	time.Sleep(200 * time.Millisecond)
	attacker := dialCoT(t, app, clientTLS(t, app, "napastnik", nil))
	attacker.send(t, saXML("UID-VICTIM", "PODSZYW"))
	time.Sleep(300 * time.Millisecond)

	if c := app.router.ClientByUID("UID-VICTIM"); c == nil || c.Identity.Username != "ofiara" {
		t.Fatal("the victim lost its UID")
	}
	victim.expectNothing(t, "PODSZYW")
}

func TestFileOwnershipAndPrivacy(t *testing.T) {
	app := newTestServer(t)
	ownerCfg := clientTLS(t, app, "nadawca", nil)
	otherCfg := clientTLS(t, app, "ciekawski", nil)
	code, url := marti(t, app, ownerCfg, "POST", "/Marti/sync/missionupload?filename=tajne.zip&creatorUid=U1", "PK\x03\x04tajne")
	if code != 200 {
		t.Fatalf("upload: %d", code)
	}
	hash := url[strings.Index(url, "hash=")+5:]
	marti(t, app, ownerCfg, "PUT", "/Marti/api/sync/metadata/"+hash+"/tool", "private")

	if _, body := marti(t, app, otherCfg, "GET", "/Marti/sync/search?tool=private", ""); strings.Contains(body, hash) {
		t.Fatal("private file listed for another user")
	}
	if code, _ := marti(t, app, otherCfg, "PUT", "/Marti/api/sync/metadata/"+hash+"/tool", "public"); code != 403 {
		t.Fatalf("metadata change by non-owner: expected 403, got %d", code)
	}
	// Re-uploading identical content must not transfer ownership.
	marti(t, app, otherCfg, "POST", "/Marti/sync/missionupload?filename=moje.zip&creatorUid=U2", "PK\x03\x04tajne")
	if code, _ := marti(t, app, otherCfg, "DELETE", "/Marti/api/files/"+hash, ""); code != 403 {
		t.Fatalf("delete after re-upload: expected 403, got %d", code)
	}
}

func TestPanelRejectsCrossSiteWrites(t *testing.T) {
	app := newTestServer(t)
	h := app.panelHandler(false)
	post := func(origin, ctype string) int {
		req := httptest.NewRequest("POST", "http://192.168.1.5/api/login", strings.NewReader(`{"username":"admin","password":"supertajne"}`))
		req.Host = "192.168.1.5"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("http://evil.example", "application/json"); code != 403 {
		t.Fatalf("cross-origin write: expected 403, got %d", code)
	}
	if code := post("", "text/plain"); code != 400 {
		t.Fatalf("form-style body: expected 400, got %d", code)
	}
	if code := post("http://192.168.1.5", "application/json"); code != 200 {
		t.Fatalf("same-origin login: expected 200, got %d", code)
	}
}
