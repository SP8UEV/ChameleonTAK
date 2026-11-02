package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"chameleontak/internal/config"
)

// Aircraft from the decoder reach TAK clients in the chosen groups only.
func TestADSBBridge(t *testing.T) {
	app := newTestServer(t)
	src := filepath.Join(t.TempDir(), "aircraft.json")
	now := float64(time.Now().UnixNano()) / 1e9
	if err := os.WriteFile(src, []byte(fmt.Sprintf(`{"now":%f,"messages":10,"aircraft":[
		{"hex":"48ae21","flight":"LOT3NV","alt_baro":12000,"gs":300,"track":90,"lat":52.2,"lon":21.0,"seen_pos":1},
		{"hex":"48ae22","flight":"FAR","alt_baro":12000,"lat":55.0,"lon":21.0,"seen_pos":1}]}`, now)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.cfg.Update(func(c *config.Config) error {
		c.ADSB = config.ADSB{Bridge: true, Source: src, Lat: 52.17, Lon: 20.97, MaxRangeKm: 100, Groups: []string{"Lotnictwo"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	air := dialCoT(t, app, clientTLS(t, app, "pilot", []string{"Lotnictwo"}))
	ground := dialCoT(t, app, clientTLS(t, app, "piechur", []string{"Piechota"}))
	waitClients(t, app, 2)

	app.adsb.step(t.Context())
	air.expect(t, `uid="ICAO-48AE21"`)
	ground.expectNothing(t, `ICAO-`)
	st := app.adsbStatus(newTestRequest())
	if len(st.Planes) != 1 || st.Planes[0].Callsign != "LOT3NV" || st.ReadError != "" {
		t.Fatalf("status: %+v", st)
	}
}

func newTestRequest() *http.Request {
	return httptest.NewRequest(http.MethodGet, "http://192.168.1.102/api/adsb", nil)
}

// Without a receiver the internet provider's aircraft reach TAK on their own, marked with
// their source; with one, the receiver's own aircraft win.
func TestADSBOnline(t *testing.T) {
	app := newTestServer(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"now":%d,"ac":[
			{"hex":"48ae21","flight":"LOT3NV","alt_baro":12000,"gs":300,"track":90,"lat":52.2,"lon":21.0,"seen_pos":1},
			{"hex":"3c6444","flight":"DLH1","alt_baro":30000,"gs":450,"track":270,"lat":52.3,"lon":20.9,"seen_pos":2}]}`, time.Now().UnixMilli())
	}))
	defer api.Close()
	app.adsb.onlineAPI.Endpoints = map[string]string{"adsbfi": api.URL}
	src := filepath.Join(t.TempDir(), "missing.json")
	if _, err := app.cfg.Update(func(c *config.Config) error {
		c.ADSB = config.ADSB{Bridge: true, Source: src, Lat: 52.17, Lon: 20.97, MaxRangeKm: 100, Online: "adsbfi"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	c := dialCoT(t, app, clientTLS(t, app, "pilot", nil))
	waitClients(t, app, 1)

	deadline := time.Now().Add(5 * time.Second)
	for {
		app.adsb.step(t.Context()) // the first step asks the provider in the background
		if st := app.adsbStatus(newTestRequest()); st.Online.Planes == 2 {
			if st.Planes[0].Source != "adsb.fi" || st.Online.Error != "" || st.Online.LastFetch == nil {
				t.Fatalf("status: %+v", st)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no aircraft from the internet: %+v", app.adsbStatus(newTestRequest()).Online)
		}
		time.Sleep(50 * time.Millisecond)
	}
	c.expect(t, "źródło: adsb.fi")

	// A receiver's aircraft come first; the internet only adds the ones it does not see.
	if err := os.WriteFile(src, []byte(fmt.Sprintf(`{"now":%f,"aircraft":[{"hex":"48ae21","flight":"LOT3NV","alt_baro":12000,"lat":52.2,"lon":21.0,"seen_pos":1}]}`,
		float64(time.Now().UnixNano())/1e9)), 0o644); err != nil {
		t.Fatal(err)
	}
	app.adsb.step(t.Context())
	st := app.adsbStatus(newTestRequest())
	if len(st.Planes) != 2 || st.Planes[0].Hex != "48AE21" || st.Planes[0].Source != "" || st.Planes[1].Source != "adsb.fi" || st.Online.Planes != 1 {
		t.Fatalf("merged: %+v", st.Planes)
	}
}

// The panel never gets the OpenSky secret back and does not have to send it again.
func TestADSBOnlineSettings(t *testing.T) {
	app := newTestServer(t)
	cookie := adminCookie(t, app)
	if code, out := panelCall(t, app, cookie, "PUT", "/api/adsb", "", `{"online":"adsblol"}`); code != 400 {
		t.Fatalf("online without a position: %d %v", code, out)
	}
	if code, out := panelCall(t, app, cookie, "PUT", "/api/adsb", "", `{"online":"flightradar","lat":52,"lon":21}`); code != 400 {
		t.Fatalf("unknown provider: %d %v", code, out)
	}
	if code, out := panelCall(t, app, cookie, "PUT", "/api/adsb", "", `{"online":"opensky","lat":52,"lon":21,"openSkyId":"me-api-client"}`); code != 400 {
		t.Fatalf("client without a secret: %d %v", code, out)
	}
	code, out := panelCall(t, app, cookie, "PUT", "/api/adsb", "", `{"online":"opensky","lat":52,"lon":21,"openSkyId":"me-api-client","openSkySecret":"s3cret"}`)
	if code != 200 || out["openSkySecretSet"] != true || out["config"].(map[string]any)["openSkySecret"] != "" {
		t.Fatalf("save: %d %v", code, out)
	}
	if code, out = panelCall(t, app, cookie, "PUT", "/api/adsb", "", `{"online":"opensky","lat":52.1,"lon":21,"openSkyId":"me-api-client","openSkySecret":""}`); code != 200 {
		t.Fatalf("save again: %d %v", code, out)
	}
	if got := app.cfg.Get().ADSB.OpenSkySecret; got != "s3cret" {
		t.Fatalf("secret after saving without it: %q", got)
	}
	code, out = panelCall(t, app, cookie, "GET", "/api/adsb", "", "")
	if code != 200 || out["openSkySecretSet"] != true || len(out["providers"].([]any)) != 3 {
		t.Fatalf("get: %d %v", code, out)
	}
}
