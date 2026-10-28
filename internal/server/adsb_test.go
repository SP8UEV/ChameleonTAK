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
