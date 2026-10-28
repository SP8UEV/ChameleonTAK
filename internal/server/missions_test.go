package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMissionLifecycle(t *testing.T) {
	app := newTestServer(t)
	aCfg := clientTLS(t, app, "dowodca", nil)
	bCfg := clientTLS(t, app, "zolnierz", nil)
	a := dialCoT(t, app, aCfg)
	b := dialCoT(t, app, bCfg)
	a.send(t, saXML("UID-DOW", "DOW"))
	b.send(t, saXML("UID-ZOL", "ZOL"))
	waitUIDs(t, app, "UID-DOW", "UID-ZOL")

	port := app.cfg.Get().Ports.Marti
	ca := httpsClient(app, aCfg.Certificates...)
	cb := httpsClient(app, bCfg.Certificates...)
	do := func(cl *http.Client, method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, martiURL(app, port, path), strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := cl.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	code, body := do(ca, "PUT", "/Marti/api/missions/Operacja1?creatorUid=UID-DOW&description=test&tool=public", "")
	if code != 201 || !strings.Contains(body, `"token"`) {
		t.Fatalf("create: %d %s", code, body)
	}
	code, body = do(cb, "GET", "/Marti/api/missions", "")
	var list struct {
		Data []struct{ Name string } `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &list)
	if code != 200 || len(list.Data) != 1 || list.Data[0].Name != "Operacja1" {
		t.Fatalf("list: %d %s", code, body)
	}
	code, body = do(cb, "PUT", "/Marti/api/missions/Operacja1/subscription?uid=UID-ZOL", "")
	if code != 201 || !strings.Contains(body, "MISSION_SUBSCRIBER") {
		t.Fatalf("subscribe: %d %s", code, body)
	}

	// A marker sent to the mission reaches the subscriber, is stored, and triggers a change notification.
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	marker := `<event version="2.0" uid="MARKER-1" type="a-h-G" how="h-g-i-g-o" time="` + now + `" start="` + now + `" stale="2030-01-01T00:00:00.000Z"><point lat="52.3" lon="21.1" hae="0" ce="9999999" le="9999999"/><detail><contact callsign="Wrog-1"/><marti><dest mission="Operacja1"/></marti></detail></event>`
	a.send(t, marker)
	b.expect(t, `uid="MARKER-1"`)
	b.expect(t, `t-x-m-c`)

	code, body = do(cb, "GET", "/Marti/api/missions/Operacja1/cot", "")
	if code != 200 || !strings.Contains(body, "MARKER-1") {
		t.Fatalf("mission cot: %d %s", code, body)
	}
	code, body = do(cb, "GET", "/Marti/api/missions/Operacja1/changes", "")
	if code != 200 || !strings.Contains(body, "ADD_CONTENT") || !strings.Contains(body, "CREATE_MISSION") {
		t.Fatalf("changes: %d %s", code, body)
	}

	// Data Sync extras: the owner sets a role (the client hears t-x-m-r), the GUID twin
	// answers like the name, a password can be set, and deleting tells the groups (t-x-m-d).
	if code, body := do(cb, "PUT", "/Marti/api/missions/Operacja1/role?clientUid=UID-ZOL&role=MISSION_READONLY_SUBSCRIBER", ""); code != 403 {
		t.Fatalf("subscriber changing roles: %d %s", code, body)
	}
	if code, body := do(ca, "PUT", "/Marti/api/missions/Operacja1/role?clientUid=UID-ZOL&role=MISSION_READONLY_SUBSCRIBER", ""); code != 200 {
		t.Fatalf("set role: %d %s", code, body)
	}
	b.expect(t, `t-x-m-r`)
	mi, err := app.store.GetMission("Operacja1")
	if err != nil {
		t.Fatal(err)
	}
	if code, body := do(cb, "GET", "/Marti/api/missions/guid/"+mi.GUID+"/subscription?uid=UID-ZOL", ""); code != 200 || !strings.Contains(body, "MISSION_READONLY_SUBSCRIBER") {
		t.Fatalf("guid twin: %d %s", code, body)
	}
	if code, body := do(ca, "PUT", "/Marti/api/missions/Operacja1/password?password=tajne123", ""); code != 200 || !strings.Contains(body, `"passwordProtected":true`) {
		t.Fatalf("set password: %d %s", code, body)
	}
	if code, body := do(cb, "GET", "/Marti/api/missions/Operacja1/contacts", ""); code != 200 || !strings.Contains(body, "UID-ZOL") {
		t.Fatalf("contacts: %d %s", code, body)
	}
	if code, body := do(ca, "DELETE", "/Marti/api/missions/Operacja1", ""); code != 200 {
		t.Fatalf("delete: %d %s", code, body)
	}
	b.expect(t, `t-x-m-d`)
}
