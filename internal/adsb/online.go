package adsb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Internet sources: public aggregators that share the data of volunteer receivers. Only services
// whose terms allow pulling their data into an application are listed; each wants its name shown
// next to the data, so every aircraft carries its source into TAK.

// Provider is an internet source of aircraft.
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Site    string `json:"site"`
	Terms   string `json:"terms"`   // what the data may be used for
	Account bool   `json:"account"` // an optional API account raises the limits
}

// Providers are the supported internet sources, the most permissive first.
var Providers = []Provider{
	{ID: "adsblol", Name: "adsb.lol", Site: "https://adsb.lol",
		Terms: "Otwarte dane na licencji ODbL. Wolno ich używać, także komercyjnie, z podaniem źródła."},
	{ID: "adsbfi", Name: "adsb.fi", Site: "https://adsb.fi",
		Terms: "Tylko do użytku osobistego i niekomercyjnego, z podaniem źródła."},
	{ID: "opensky", Name: "OpenSky Network", Site: "https://opensky-network.org", Account: true,
		Terms: "Do celów niekomercyjnych i badawczych. Bez konta API dane odświeżają się co kilka minut."},
}

// ProviderByID returns the provider with the given ID.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// Area is the circle the aircraft are fetched for.
type Area struct {
	Lat, Lon float64
	RadiusKm float64 // 0 = the largest area the providers serve
}

// maxRadiusKm is the largest radius the aggregators answer for (250 NM).
const maxRadiusKm = 250 * nmToKm

const nmToKm = 1.852

func (a Area) radiusKm() float64 {
	if a.RadiusKm <= 0 || a.RadiusKm > maxRadiusKm {
		return maxRadiusKm
	}
	return a.RadiusKm
}

// Credentials are an optional OpenSky API client (client credentials flow).
type Credentials struct {
	ID, Secret string
}

// RateLimitError means the provider asked to slow down.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("dostawca ogranicza liczbę zapytań, kolejna próba za %s", e.RetryAfter.Round(time.Second))
}

// Interval is how often the provider may be asked about the area. The aggregators allow one
// request a second; OpenSky gives a daily credit budget that depends on the area and the account.
func Interval(provider string, area Area, cred Credentials) time.Duration {
	if provider != "opensky" {
		return 10 * time.Second
	}
	credits := 400.0 // anonymous
	if cred.ID != "" {
		credits = 4000
	}
	_, _, cost := openSkyBox(area)
	sec := math.Ceil(86400 * cost / credits * 1.1)
	return max(time.Duration(sec)*time.Second, 10*time.Second)
}

// Online fetches aircraft from the internet providers. It keeps the OpenSky access token between
// calls.
type Online struct {
	Client    *http.Client      // nil = http.DefaultClient
	Endpoints map[string]string // overrides the providers' addresses (tests)

	mu       sync.Mutex
	tokenFor string // client the token belongs to (ID and secret)
	token    string
	tokenExp time.Time
}

var defaultEndpoints = map[string]string{
	"adsblol":      "https://api.adsb.lol",
	"adsbfi":       "https://opendata.adsb.fi",
	"opensky":      "https://opensky-network.org",
	"opensky-auth": "https://auth.opensky-network.org/auth/realms/opensky-network/protocol/openid-connect/token",
}

const userAgent = "ChameleonTAK (+https://github.com/SP8UEV/ChameleonTAK)"

func (o *Online) endpoint(key string) string {
	if u, ok := o.Endpoints[key]; ok {
		return u
	}
	return defaultEndpoints[key]
}

func (o *Online) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return http.DefaultClient
}

// Fetch returns the aircraft the provider sees in the area.
func (o *Online) Fetch(ctx context.Context, provider string, area Area, cred Credentials) (*Snapshot, error) {
	nm := int(math.Ceil(area.radiusKm() / nmToKm))
	lat, lon := strconv.FormatFloat(area.Lat, 'f', 4, 64), strconv.FormatFloat(area.Lon, 'f', 4, 64)
	switch provider {
	case "adsblol":
		return o.aggregator(ctx, fmt.Sprintf("%s/v2/point/%s/%s/%d", o.endpoint("adsblol"), lat, lon, nm))
	case "adsbfi":
		return o.aggregator(ctx, fmt.Sprintf("%s/api/v3/lat/%s/lon/%s/dist/%d", o.endpoint("adsbfi"), lat, lon, nm))
	case "opensky":
		return o.openSky(ctx, area, cred)
	}
	return nil, fmt.Errorf("nieznany dostawca %q", provider)
}

// aggregator reads a readsb-style answer ({"now": ms, "ac": [...]}) of adsb.lol or adsb.fi.
func (o *Online) aggregator(ctx context.Context, u string) (*Snapshot, error) {
	b, err := o.get(ctx, u, "")
	if err != nil {
		return nil, err
	}
	var r struct {
		Now float64    `json:"now"`
		AC  []Aircraft `json:"ac"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("nieczytelna odpowiedź dostawcy: %w", err)
	}
	if r.Now > 1e11 { // milliseconds, unlike aircraft.json
		r.Now /= 1000
	}
	return &Snapshot{Now: r.Now, Aircraft: r.AC}, nil
}

var errUnauthorized = errors.New("dostawca odrzucił dane logowania")

func (o *Online) get(ctx context.Context, u, bearer string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, &RateLimitError{RetryAfter: retryAfter(resp.Header)}
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, errUnauthorized
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s odpowiedział kodem %d", req.URL.Host, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// retryAfter reads how long a provider wants us to wait (Retry-After, or OpenSky's own header).
func retryAfter(h http.Header) time.Duration {
	for _, k := range []string{"X-Rate-Limit-Retry-After-Seconds", "Retry-After"} {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Get(k))); err == nil && n > 0 {
			return min(time.Duration(n)*time.Second, 24*time.Hour)
		}
	}
	return time.Minute
}

// ---------- OpenSky Network ----------

// openSkyBox turns the circle into the bounding box OpenSky asks for and its credit cost.
func openSkyBox(a Area) (dLat, dLon, cost float64) {
	r := a.radiusKm()
	dLat = r / 111.32
	dLon = r / (111.32 * math.Max(math.Cos(a.Lat*math.Pi/180), 0.01))
	switch sq := 4 * dLat * dLon; {
	case sq <= 25:
		cost = 1
	case sq <= 100:
		cost = 2
	case sq <= 400:
		cost = 3
	default:
		cost = 4
	}
	return dLat, dLon, cost
}

func (o *Online) openSky(ctx context.Context, area Area, cred Credentials) (*Snapshot, error) {
	dLat, dLon, _ := openSkyBox(area)
	q := url.Values{}
	q.Set("lamin", strconv.FormatFloat(math.Max(area.Lat-dLat, -90), 'f', 4, 64))
	q.Set("lamax", strconv.FormatFloat(math.Min(area.Lat+dLat, 90), 'f', 4, 64))
	q.Set("lomin", strconv.FormatFloat(math.Max(area.Lon-dLon, -180), 'f', 4, 64))
	q.Set("lomax", strconv.FormatFloat(math.Min(area.Lon+dLon, 180), 'f', 4, 64))
	q.Set("extended", "1") // aircraft category
	token := ""
	if cred.ID != "" {
		var err error
		if token, err = o.openSkyToken(ctx, cred); err != nil {
			return nil, err
		}
	}
	b, err := o.get(ctx, o.endpoint("opensky")+"/api/states/all?"+q.Encode(), token)
	if errors.Is(err, errUnauthorized) {
		o.mu.Lock()
		o.token = "" // fetch a new one next time
		o.mu.Unlock()
	}
	if err != nil {
		return nil, err
	}
	var r struct {
		Time   float64 `json:"time"`
		States [][]any `json:"states"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("nieczytelna odpowiedź OpenSky: %w", err)
	}
	snap := &Snapshot{Now: r.Time, Aircraft: make([]Aircraft, 0, len(r.States))}
	for _, s := range r.States {
		if a, ok := openSkyAircraft(s, r.Time); ok {
			snap.Aircraft = append(snap.Aircraft, a)
		}
	}
	return snap, nil
}

// openSkyAircraft converts a state vector (metres, m/s) into an aircraft.json entry (feet, knots),
// so both kinds of source go through the same filter.
func openSkyAircraft(s []any, now float64) (Aircraft, bool) {
	at := func(i int) any {
		if i < len(s) {
			return s[i]
		}
		return nil
	}
	str := func(i int) string { v, _ := at(i).(string); return strings.TrimSpace(v) }
	num := func(i int) *float64 {
		if v, ok := at(i).(float64); ok {
			return &v
		}
		return nil
	}
	hex := str(0)
	lat, lon := num(6), num(5)
	if hex == "" || lat == nil || lon == nil {
		return Aircraft{}, false
	}
	a := Aircraft{Hex: hex, Flight: str(1), Squawk: str(14), Lat: lat, Lon: lon}
	if ground, _ := at(8).(bool); ground {
		a.AltBaro = json.RawMessage(`"ground"`)
	} else if alt := num(7); alt != nil {
		a.AltBaro = json.RawMessage(strconv.FormatFloat(*alt/ftToM, 'f', 0, 64))
	}
	if geo := num(13); geo != nil {
		v := *geo / ftToM
		a.AltGeom = &v
	}
	if v := num(9); v != nil {
		gs := *v * 3.6 / ktToKmh
		a.GS = &gs
	}
	a.Track = num(10)
	if t := num(3); t != nil && now > 0 {
		seen := math.Max(now-*t, 0)
		a.SeenPos = &seen
	}
	if c := num(17); c != nil {
		a.Category = openSkyCategory(int(*c))
	}
	return a, true
}

// openSkyCategory maps OpenSky's category number to the ADS-B emitter category (A1…C5).
func openSkyCategory(c int) string {
	switch {
	case c >= 2 && c <= 8:
		return "A" + strconv.Itoa(c-1)
	case c >= 9 && c <= 15:
		return "B" + strconv.Itoa(c-8)
	case c >= 16 && c <= 20:
		return "C" + strconv.Itoa(c-15)
	}
	return ""
}

func (o *Online) openSkyToken(ctx context.Context, cred Credentials) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.token != "" && o.tokenFor == cred.ID+" "+cred.Secret && time.Now().Before(o.tokenExp) {
		return o.token, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {cred.ID}, "client_secret": {cred.Secret}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint("opensky-auth"), strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := o.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OpenSky nie przyjął klienta API (kod %d). Sprawdź client_id i client_secret", resp.StatusCode)
	}
	var t struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil || t.AccessToken == "" {
		return "", errors.New("OpenSky nie zwrócił tokenu dostępu")
	}
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 1800
	}
	o.token, o.tokenFor = t.AccessToken, cred.ID+" "+cred.Secret
	o.tokenExp = time.Now().Add(time.Duration(t.ExpiresIn)*time.Second - time.Minute)
	return o.token, nil
}
