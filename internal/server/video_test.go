package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chameleontak/internal/config"
)

// videoAuth asks the MediaMTX hook from addr.
func videoAuth(app *App, addr, body string) int {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/video/auth", strings.NewReader(body))
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()
	app.panelHandler(false).ServeHTTP(rec, req)
	return rec.Code
}

func TestVideoStreamAccess(t *testing.T) {
	app := newTestServer(t)
	if _, err := app.cfg.Update(func(c *config.Config) error {
		c.Video = config.VideoService{Enabled: true, Secret: "s3cret"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, app)
	pilotCfg := clientTLS(t, app, "pilot", []string{"Lotnictwo"})
	_ = clientTLS(t, app, "piechur", []string{"Piechota"})

	code, out := panelCall(t, app, cookie, "POST", "/api/video/streams", "", `{"name":"Dron 1","audience":"groups","groups":["Lotnictwo"]}`)
	if code != 201 {
		t.Fatalf("create stream: %d %v", code, out)
	}
	raw, _ := json.Marshal(out)
	var st streamView
	_ = json.Unmarshal(raw, &st)
	if !strings.HasPrefix(st.PublishURL, "rtmp://") || !strings.Contains(st.LarixURL, "larix://set/v1") {
		t.Fatalf("publish links: %+v", st)
	}

	ask := func(action, user, pass string) int {
		b, _ := json.Marshal(map[string]string{"action": action, "user": user, "password": pass, "path": st.Path, "ip": "192.168.1.50"})
		return videoAuth(app, "127.0.0.1:40000", string(b))
	}
	if c := ask("publish", "pub", st.PublishKey); c != 200 {
		t.Fatalf("publish with the key: %d", c)
	}
	if c := ask("publish", "pub", "zly-klucz"); c != 401 {
		t.Fatalf("publish with a wrong key: %d", c)
	}
	if c := ask("read", "pilot", app.videoToken("pilot")); c != 200 {
		t.Fatalf("read by the audience: %d", c)
	}
	if c := ask("read", "piechur", app.videoToken("piechur")); c != 401 {
		t.Fatalf("read outside the audience: %d", c)
	}
	if c := ask("read", "pilot", app.videoToken("piechur")); c != 401 {
		t.Fatalf("read with someone else's password: %d", c)
	}
	if c := videoAuth(app, "192.168.1.60:5000", `{"action":"read"}`); c != 403 {
		t.Fatalf("hook from outside loopback: %d", c)
	}

	// The audience sees the stream in the video list and gets it as a feed.
	if code, body := marti(t, app, pilotCfg, "GET", "/Marti/vcm", ""); code != 200 || !strings.Contains(body, "<alias>Dron 1</alias>") || !strings.Contains(body, "pilot:") {
		t.Fatalf("vcm: %d %s", code, body)
	}
	c := dialCoT(t, app, pilotCfg)
	c.send(t, saXML("UID-PILOT", "PILOT"))
	waitUIDs(t, app, "UID-PILOT")
	app.pushVideoFeed(app.cfg.Get().Streams[0])
	c.expect(t, `type="b-i-v"`)
}
