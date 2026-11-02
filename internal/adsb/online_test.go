package adsb

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The aggregators answer in readsb's format, with "now" in milliseconds.
func TestOnlineAggregator(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		if !strings.HasPrefix(r.UserAgent(), "ChameleonTAK") {
			t.Errorf("user agent %q", r.UserAgent())
		}
		_, _ = w.Write([]byte(`{"ac":[{"hex":"48b603","flight":"SPNRD   ","r":"SP-NRD","t":"DA42","alt_baro":9000,"gs":154.5,"track":85.5,"lat":52.37,"lon":19.74,"seen_pos":0.3}],"now":1793618516002}`))
	}))
	defer srv.Close()
	o := &Online{Endpoints: map[string]string{"adsblol": srv.URL, "adsbfi": srv.URL}}
	area := Area{Lat: 52.23, Lon: 21.01, RadiusKm: 150}
	for provider, path := range map[string]string{"adsblol": "/v2/point/52.2300/21.0100/81", "adsbfi": "/api/v3/lat/52.2300/lon/21.0100/dist/81"} {
		snap, err := o.Fetch(t.Context(), provider, area, Credentials{})
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if got != path {
			t.Errorf("%s asked %s, want %s", provider, got, path)
		}
		if snap.Now != 1793618516.002 || len(snap.Aircraft) != 1 {
			t.Fatalf("%s: %+v", provider, snap)
		}
		p := snap.Planes(Filter{})[0]
		if p.Callsign != "SPNRD" || p.Reg != "SP-NRD" || int(p.AltM) != 2743 {
			t.Fatalf("%s plane: %+v", provider, p)
		}
	}
	// No range limit asks for the largest area the aggregators serve.
	if _, err := o.Fetch(t.Context(), "adsblol", Area{Lat: 52.23, Lon: 21.01}, Credentials{}); err != nil || !strings.HasSuffix(got, "/250") {
		t.Fatalf("unlimited range: %s %v", got, err)
	}
}

func TestOnlineRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	o := &Online{Endpoints: map[string]string{"adsbfi": srv.URL}}
	_, err := o.Fetch(t.Context(), "adsbfi", Area{Lat: 52, Lon: 21, RadiusKm: 100}, Credentials{})
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 2*time.Minute {
		t.Fatalf("got %v", err)
	}
}

// OpenSky's state vectors use metres and m/s; with an API client the token is fetched once.
func TestOnlineOpenSky(t *testing.T) {
	tokens := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "me-api-client" || r.Form.Get("client_secret") != "s3cret" {
			http.Error(w, "bad client", http.StatusUnauthorized)
			return
		}
		tokens++
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":1800}`))
	})
	mux.HandleFunc("GET /api/states/all", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("extended") != "1" || q.Get("lamin") == "" || q.Get("lomax") == "" {
			t.Errorf("query %s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"time":1000,"states":[
			["471f41","WZZ9327 ","Hungary",990,1000,20.7373,52.3749,3048.0,false,100.0,121.9,-2.2,null,3100.0,"0506",false,0,4],
			["47875c","HELI1   ","Norway",1000,1000,20.88,52.20,300.0,false,50.0,10.0,0,null,null,null,false,0,8],
			["48ae99",null,"Poland",1000,1000,20.96,52.16,null,true,5.0,90.0,null,null,null,null,false,0,17],
			["48ae00","NOPOS   ","Poland",null,1000,null,null,null,false,null,null,null,null,null,null,false,0,0]]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	o := &Online{Endpoints: map[string]string{"opensky": srv.URL, "opensky-auth": srv.URL + "/token"}}
	area := Area{Lat: 52.23, Lon: 21.01, RadiusKm: 100}
	for _, cred := range []Credentials{{}, {ID: "me-api-client", Secret: "s3cret"}, {ID: "me-api-client", Secret: "s3cret"}} {
		snap, err := o.Fetch(t.Context(), "opensky", area, cred)
		if err != nil {
			t.Fatal(err)
		}
		planes := snap.Planes(Filter{})
		if len(planes) != 3 {
			t.Fatalf("planes: %+v", planes)
		}
		wizz, heli, car := planes[0], planes[1], planes[2]
		if wizz.Callsign != "WZZ9327" || int(wizz.AltM+0.5) != 3048 || int(wizz.SpeedKmh+0.5) != 360 || wizz.Category != "A3" || wizz.Seen.Unix() != 990 {
			t.Errorf("wizz: %+v", wizz)
		}
		if heli.CoTType() != "a-n-A-C-H" {
			t.Errorf("rotorcraft: %s (%s)", heli.CoTType(), heli.Category)
		}
		if !car.Ground || car.Category != "C2" {
			t.Errorf("surface vehicle: %+v", car)
		}
	}
	if tokens != 1 {
		t.Errorf("token fetched %d times, want once", tokens)
	}
	if _, err := o.Fetch(t.Context(), "opensky", area, Credentials{ID: "me-api-client", Secret: "wrong"}); err == nil || !strings.Contains(err.Error(), "client_id") {
		t.Errorf("wrong secret: %v", err)
	}
}

func TestOnlineInterval(t *testing.T) {
	small := Area{Lat: 52, Lon: 21, RadiusKm: 100}
	if got := Interval("adsblol", small, Credentials{}); got != 10*time.Second {
		t.Errorf("aggregator: %s", got)
	}
	// 400 credits a day without an account, 4000 with one; a small box costs one credit.
	if got := Interval("opensky", small, Credentials{}); got < 200*time.Second || got > 300*time.Second {
		t.Errorf("anonymous: %s", got)
	}
	if got := Interval("opensky", small, Credentials{ID: "x"}); got < 20*time.Second || got > 30*time.Second {
		t.Errorf("account: %s", got)
	}
	if Interval("opensky", Area{Lat: 52, Lon: 21}, Credentials{ID: "x"}) <= Interval("opensky", small, Credentials{ID: "x"}) {
		t.Error("a larger area costs more credits")
	}
}
