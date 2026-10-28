package adsb

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"chameleontak/internal/cot"
)

const sample = `{"now": %NOW%, "messages": 1234, "aircraft": [
 {"hex":"48ae21","flight":"LOT3NV  ","r":"SP-LWG","t":"E195","alt_baro":36000,"gs":451.2,"track":92.5,"lat":52.30,"lon":20.90,"category":"A3","squawk":"2311","seen_pos":1.2},
 {"hex":"~2a0b1c","alt_baro":"ground","gs":5,"track":10,"lat":52.165,"lon":20.967,"seen_pos":0.5},
 {"hex":"ae1234","flight":"RCH123","alt_baro":28000,"lat":53.0,"lon":21.0,"category":"A5","dbFlags":1,"seen_pos":3},
 {"hex":"3c6444","alt_baro":12000,"lat":52.2,"lon":20.95,"category":"A7","seen_pos":2},
 {"hex":"440000","flight":"NOPOS"},
 {"hex":"440001","alt_baro":5000,"lat":52.1,"lon":20.9,"seen_pos":300},
 {"hex":"440002","alt_baro":9000,"lat":54.5,"lon":18.5,"seen_pos":1}
]}`

func snapshot(t *testing.T) *Snapshot {
	t.Helper()
	raw := strings.Replace(sample, "%NOW%", "1793200000.5", 1)
	var s Snapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

func TestPlanesFilter(t *testing.T) {
	s := snapshot(t)
	all := s.Planes(Filter{})
	if len(all) != 5 { // no position and a 5-minute-old position are left out
		t.Fatalf("got %d planes", len(all))
	}
	near := s.Planes(Filter{Lat: 52.17, Lon: 20.97, MaxRangeKm: 120, MaxAltFt: 30000})
	got := map[string]bool{}
	for _, p := range near {
		got[p.Hex] = true
	}
	// LOT is too high, 440002 (Gdańsk) is too far.
	if len(near) != 3 || !got["2A0B1C"] || !got["AE1234"] || !got["3C6444"] {
		t.Fatalf("filtered: %v", got)
	}
}

func TestCoT(t *testing.T) {
	s := snapshot(t)
	types := map[string]string{}
	for _, p := range s.Planes(Filter{}) {
		raw := p.CoT(time.Minute)
		ev, err := cot.Parse(raw)
		if err != nil {
			t.Fatalf("%s: %v\n%s", p.Hex, err, raw)
		}
		if ev.UID != "ICAO-"+p.Hex || ev.Callsign != p.Label() {
			t.Fatalf("uid %q callsign %q", ev.UID, ev.Callsign)
		}
		types[p.Hex] = ev.Type
	}
	want := map[string]string{"48AE21": "a-n-A-C-F", "2A0B1C": "a-n-G-E-V", "AE1234": "a-n-A-M-F", "3C6444": "a-n-A-C-H"}
	for hex, typ := range want {
		if types[hex] != typ {
			t.Errorf("%s: %s, want %s", hex, types[hex], typ)
		}
	}
	lot := s.Planes(Filter{})[0]
	raw := string(lot.CoT(time.Minute))
	for _, part := range []string{`lat="52.300000"`, `hae="10972.8"`, `callsign="LOT3NV"`, "SP-LWG", "squawk 2311", `course="92.5"`} {
		if !strings.Contains(raw, part) {
			t.Errorf("missing %s in %s", part, raw)
		}
	}
}
