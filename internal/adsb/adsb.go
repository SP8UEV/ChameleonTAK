// Package adsb reads aircraft from an ADS-B decoder (readsb, dump1090-fa, dump1090-mutability)
// and turns them into CoT events for TAK clients.
package adsb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"chameleontak/internal/cot"
)

// KnownSources are the aircraft.json files the common decoders write, in order of preference.
var KnownSources = []string{
	"/run/readsb/aircraft.json",
	"/run/dump1090-fa/aircraft.json",
	"/run/dump1090-mutability/aircraft.json",
	"/run/dump1090/aircraft.json",
	"http://127.0.0.1:8080/data/aircraft.json",
}

// Aircraft is one entry of aircraft.json (the fields this package uses).
type Aircraft struct {
	Hex      string          `json:"hex"`
	Flight   string          `json:"flight"`
	Reg      string          `json:"r"`
	Type     string          `json:"t"`
	Lat      *float64        `json:"lat"`
	Lon      *float64        `json:"lon"`
	AltBaro  json.RawMessage `json:"alt_baro"` // feet, or "ground"
	AltGeom  *float64        `json:"alt_geom"` // feet
	GS       *float64        `json:"gs"`       // knots
	Track    *float64        `json:"track"`    // degrees
	Category string          `json:"category"`
	Squawk   string          `json:"squawk"`
	SeenPos  *float64        `json:"seen_pos"` // seconds since the last position
	DBFlags  int             `json:"dbFlags"`  // readsb: 1 = military
	Messages int64           `json:"messages"`
}

// Snapshot is a decoded aircraft.json.
type Snapshot struct {
	Now      float64    `json:"now"`
	Messages int64      `json:"messages"`
	Aircraft []Aircraft `json:"aircraft"`
}

// Read loads aircraft.json from a file path or an http(s) URL.
func Read(ctx context.Context, source string) (*Snapshot, error) {
	var b []byte
	var err error
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if rerr != nil {
			return nil, rerr
		}
		resp, rerr := http.DefaultClient.Do(req)
		if rerr != nil {
			return nil, rerr
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("odpowiedź %d", resp.StatusCode)
		}
		b, err = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	} else {
		b, err = os.ReadFile(source)
	}
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("nieczytelny plik aircraft.json: %w", err)
	}
	return &s, nil
}

// Detect returns the first known source that holds a fresh aircraft.json.
func Detect(ctx context.Context) (string, error) {
	for _, src := range KnownSources {
		s, err := Read(ctx, src)
		if err == nil && s.Now > 0 && time.Since(time.Unix(int64(s.Now), 0)) < time.Minute {
			return src, nil
		}
	}
	return "", errors.New("nie znaleziono działającego dekodera ADS-B (readsb / dump1090)")
}

// Filter limits which aircraft reach TAK clients.
type Filter struct {
	Lat, Lon   float64 // receiver position; 0,0 = unknown (no range limit)
	MaxRangeKm float64 // 0 = no limit
	MaxAltM    float64 // 0 = no limit
	MaxAge     time.Duration
}

// Plane is an aircraft with a usable position.
type Plane struct {
	Hex      string    `json:"hex"`
	Callsign string    `json:"callsign"`
	Reg      string    `json:"reg"`
	Type     string    `json:"type"`
	Lat      float64   `json:"lat"`
	Lon      float64   `json:"lon"`
	AltM     float64   `json:"altM"` // barometric altitude in metres
	Ground   bool      `json:"ground"`
	SpeedKmh float64   `json:"speedKmh"`
	Track    float64   `json:"track"`
	Category string    `json:"category"`
	Squawk   string    `json:"squawk"`
	Military bool      `json:"military"`
	DistKm   float64   `json:"distKm"`
	Seen     time.Time `json:"seen"`
}

// Planes picks the aircraft that pass the filter.
func (s *Snapshot) Planes(f Filter) []Plane {
	now := time.Now()
	if s.Now > 0 {
		now = time.Unix(0, int64(s.Now*1e9))
	}
	maxAge := f.MaxAge
	if maxAge == 0 {
		maxAge = 60 * time.Second
	}
	out := make([]Plane, 0, len(s.Aircraft))
	for _, a := range s.Aircraft {
		if a.Lat == nil || a.Lon == nil || a.Hex == "" {
			continue
		}
		seen := 0.0
		if a.SeenPos != nil {
			seen = *a.SeenPos
		}
		if time.Duration(seen*float64(time.Second)) > maxAge {
			continue
		}
		p := Plane{
			Hex: strings.ToUpper(strings.TrimPrefix(a.Hex, "~")), Callsign: strings.TrimSpace(a.Flight), Reg: a.Reg, Type: a.Type,
			Lat: *a.Lat, Lon: *a.Lon, Category: a.Category, Squawk: a.Squawk, Military: a.DBFlags&1 != 0,
			Seen: now.Add(-time.Duration(seen * float64(time.Second))),
		}
		// Decoders report feet and knots, as aviation does; the panel and TAK get metric units.
		var alt any
		if len(a.AltBaro) > 0 && json.Unmarshal(a.AltBaro, &alt) == nil {
			switch v := alt.(type) {
			case float64:
				p.AltM = v * ftToM
			case string:
				p.Ground = v == "ground"
			}
		}
		if p.AltM == 0 && !p.Ground && a.AltGeom != nil {
			p.AltM = *a.AltGeom * ftToM
		}
		if a.GS != nil {
			p.SpeedKmh = *a.GS * ktToKmh
		}
		if a.Track != nil {
			p.Track = *a.Track
		}
		if f.Lat != 0 || f.Lon != 0 {
			p.DistKm = distanceKm(f.Lat, f.Lon, p.Lat, p.Lon)
			if f.MaxRangeKm > 0 && p.DistKm > f.MaxRangeKm {
				continue
			}
		}
		if f.MaxAltM > 0 && p.AltM > f.MaxAltM {
			continue
		}
		out = append(out, p)
	}
	return out
}

const (
	ftToM   = 0.3048
	ktToKmh = 1.852
)

func distanceKm(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0
	rad := math.Pi / 180
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}

// CoTType maps the aircraft to a MIL-STD-2525 CoT type: neutral (n) air (A), civil (C) or
// military (M), fixed wing (F) or rotary wing (H).
func (p Plane) CoTType() string {
	aff := "C"
	if p.Military {
		aff = "M"
	}
	frame := "F"
	switch p.Category {
	case "A7": // rotorcraft
		frame = "H"
	case "B1", "B4", "B6": // glider, ultralight, UAV
		frame = "F"
	}
	if p.Ground {
		return "a-n-G-E-V" // surface vehicle on the airport
	}
	return "a-n-A-" + aff + "-" + frame
}

// Label is the name shown on the map: flight number, else registration, else ICAO address.
func (p Plane) Label() string {
	switch {
	case p.Callsign != "":
		return p.Callsign
	case p.Reg != "":
		return p.Reg
	}
	return p.Hex
}

// CoT renders the aircraft as a CoT event that goes stale after stale.
func (p Plane) CoT(stale time.Duration) []byte {
	now := time.Now()
	var remarks []string
	if p.Callsign != "" && p.Reg != "" {
		remarks = append(remarks, p.Reg)
	}
	if p.Type != "" {
		remarks = append(remarks, p.Type)
	}
	if p.Ground {
		remarks = append(remarks, "na ziemi")
	} else {
		remarks = append(remarks, fmt.Sprintf("%.0f m", math.Round(p.AltM/10)*10))
	}
	remarks = append(remarks, fmt.Sprintf("%.0f km/h", p.SpeedKmh))
	if p.Squawk != "" {
		remarks = append(remarks, "squawk "+p.Squawk)
	}
	remarks = append(remarks, "ICAO "+p.Hex)
	return []byte(fmt.Sprintf(`<event version="2.0" uid="ICAO-%[1]s" type="%[2]s" how="m-g" time="%[3]s" start="%[3]s" stale="%[4]s">`+
		`<point lat="%.6[5]f" lon="%.6[6]f" hae="%.1[7]f" ce="50.0" le="50.0"/>`+
		`<detail><contact callsign="%[8]s"/><track course="%.1[9]f" speed="%.2[10]f"/>`+
		`<remarks>%[11]s</remarks><_flow-tags_ chameleontak-adsb="%[3]s"/></detail></event>`,
		cot.Escape(p.Hex), p.CoTType(), cot.FormatTime(now), cot.FormatTime(now.Add(stale)),
		p.Lat, p.Lon, p.AltM, cot.Escape(p.Label()), p.Track, p.SpeedKmh/3.6,
		cot.Escape(strings.Join(remarks, " · "))))
}
