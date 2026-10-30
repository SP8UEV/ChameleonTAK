package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/adsb"
	"chameleontak/internal/config"
	"chameleontak/internal/system"
)

// ADS-B bridge: every few seconds it reads aircraft.json from the decoder on the Pi and sends
// the aircraft to TAK clients as CoT events that go stale on their own when a plane is gone.

const (
	adsbInterval = 5 * time.Second
	adsbStale    = 45 * time.Second
)

type adsbBridge struct {
	app *App

	mu       sync.Mutex
	source   string // source in use (configured or detected)
	detected time.Time
	planes   []adsb.Plane
	messages int64
	lastRead time.Time
	readErr  string
	sentAt   time.Time

	job adsbJob
}

type adsbJob struct {
	Component string     `json:"component"`
	Running   bool       `json:"running"`
	Error     string     `json:"error"`
	Started   *time.Time `json:"started"`
	Finished  *time.Time `json:"finished"`
}

func newADSB(a *App) *adsbBridge { return &adsbBridge{app: a} }

func (b *adsbBridge) run(ctx context.Context) {
	t := time.NewTicker(adsbInterval)
	defer t.Stop()
	for {
		b.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (b *adsbBridge) step(ctx context.Context) {
	cfg := b.app.cfg.Get()
	if !cfg.SetupDone {
		return
	}
	src := strings.TrimSpace(cfg.ADSB.Source)
	b.mu.Lock()
	if src == "" {
		// Detect again now and then, e.g. after the decoder was installed.
		if b.source == "" && time.Since(b.detected) > 30*time.Second || b.source != "" && b.readErr != "" && time.Since(b.detected) > time.Minute {
			b.detected = time.Now()
			b.mu.Unlock()
			found, _ := adsb.Detect(ctx)
			b.mu.Lock()
			b.source = found
		}
		src = b.source
	} else {
		b.source = src
	}
	b.mu.Unlock()
	if src == "" {
		b.setRead(nil, "Nie znaleziono dekodera ADS-B.")
		return
	}
	snap, err := adsb.Read(ctx, src)
	if err != nil {
		b.setRead(nil, err.Error())
		return
	}
	if snap.Now > 0 && time.Since(time.Unix(int64(snap.Now), 0)) > time.Minute {
		b.setRead(nil, "Dekoder nie odświeża danych (aircraft.json ma ponad minutę).")
		return
	}
	planes := snap.Planes(adsb.Filter{
		Lat: cfg.ADSB.Lat, Lon: cfg.ADSB.Lon,
		MaxRangeKm: float64(cfg.ADSB.MaxRangeKm), MaxAltM: float64(cfg.ADSB.MaxAltM),
	})
	b.setRead(snap, "")
	b.mu.Lock()
	b.planes = planes
	b.mu.Unlock()
	if !cfg.ADSB.Bridge || len(planes) == 0 {
		return
	}
	var groups []string // nil = every client
	if len(cfg.ADSB.Groups) > 0 {
		groups = cfg.ADSB.Groups
	}
	targets := []*App{b.app}
	if len(cfg.ADSB.Servers) > 0 {
		targets = targets[:0]
		for _, id := range cfg.ADSB.Servers {
			if s := b.app.serverByID(id); s != nil {
				targets = append(targets, s)
			}
		}
	}
	for _, p := range planes {
		raw := p.CoT(adsbStale)
		for _, s := range targets {
			s.router.Broadcast(raw, groups)
		}
	}
	b.mu.Lock()
	b.sentAt = time.Now()
	b.mu.Unlock()
}

func (b *adsbBridge) setRead(snap *adsb.Snapshot, errMsg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.readErr = errMsg
	if snap != nil {
		b.lastRead, b.messages = time.Now(), snap.Messages
	} else {
		b.planes = nil
	}
}

// ---------- status ----------

type adsbComponent struct {
	Installed bool   `json:"installed"`
	Active    bool   `json:"active"`
	Detail    string `json:"detail"`
}

type adsbStatus struct {
	Supported bool          `json:"supported"` // privileged helper available (Linux)
	SDRs      []system.SDR  `json:"sdrs"`
	Decoder   adsbComponent `json:"decoder"`
	FR24      adsbComponent `json:"fr24"`
	ADSBX     adsbComponent `json:"adsbx"`
	Tar1090   string        `json:"tar1090"` // URL of the readsb map, "" when not installed
	Source    string        `json:"source"`
	ReadError string        `json:"readError"`
	LastRead  *time.Time    `json:"lastRead"`
	Messages  int64         `json:"messages"`
	Planes    []adsb.Plane  `json:"planes"`
	Sending   bool          `json:"sending"`
	SentAt    *time.Time    `json:"sentAt"`
	Job       adsbJob       `json:"job"`
}

func (a *App) adsbStatus(r *http.Request) adsbStatus {
	cfg := a.cfg.Get()
	b := a.adsb
	b.mu.Lock()
	st := adsbStatus{
		Source: b.source, ReadError: b.readErr, Messages: b.messages, Planes: slices.Clone(b.planes), Job: b.job,
		Sending: cfg.ADSB.Bridge && len(b.planes) > 0 && time.Since(b.sentAt) < 3*adsbInterval,
	}
	if !b.lastRead.IsZero() {
		t := b.lastRead
		st.LastRead = &t
	}
	if !b.sentAt.IsZero() {
		t := b.sentAt
		st.SentAt = &t
	}
	b.mu.Unlock()
	if st.Planes == nil {
		st.Planes = []adsb.Plane{}
	}
	st.Supported = a.helper.Available()
	st.SDRs = system.FindSDRs()
	st.Decoder = adsbComponent{Installed: system.ReadsbBinary() != "" || system.FileExists("/usr/bin/dump1090-fa")}
	st.Decoder.Active = system.ServiceActive("readsb") || system.ServiceActive("dump1090-fa")
	st.FR24 = adsbComponent{Installed: system.FileExists("/usr/bin/fr24feed"), Active: system.ServiceActive("fr24feed")}
	if st.FR24.Active {
		st.FR24.Detail = fr24Monitor(r.Context())
	}
	st.ADSBX = adsbComponent{Installed: system.ReadsbBinary() != "", Active: system.ServiceActive("chameleontak-adsbx")}
	if system.FileExists("/usr/local/share/tar1090/html") {
		st.Tar1090 = fmt.Sprintf("http://%s:%d/tar1090/", requestHostOr(r, cfg.ServerName), system.Tar1090Port())
	}
	return st
}

func requestHostOr(r *http.Request, def string) string {
	if h := requestHost(r); h != "" {
		return hostPort(h, 0, 0)
	}
	return def
}

// fr24Monitor summarises fr24feed's local status page (feed state and radar code).
func fr24Monitor(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:8754/monitor.json", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var m map[string]any
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m) != nil {
		return ""
	}
	parts := []string{}
	if v, _ := m["feed_status"].(string); v != "" {
		parts = append(parts, "stan: "+v)
	}
	if v, _ := m["feed_alias"].(string); v != "" {
		parts = append(parts, "radar "+v)
	}
	if v, ok := m["feed_num_ac_tracked"]; ok {
		parts = append(parts, fmt.Sprintf("samoloty: %v", v))
	}
	return strings.Join(parts, " · ")
}

// ---------- API ----------

func (a *App) hADSB(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"config": adsbView(a.cfg.Get().ADSB), "status": a.adsbStatus(r)})
}

// adsbView hides the ADS-B Exchange UUID's details the panel does not need.
func adsbView(c config.ADSB) config.ADSB {
	if c.Groups == nil {
		c.Groups = []string{}
	}
	if c.Servers == nil {
		c.Servers = []string{}
	}
	return c
}

type adsbStep struct {
	Step  string `json:"step"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (a *App) hPutADSB(w http.ResponseWriter, r *http.Request) {
	old := a.cfg.Get().ADSB
	in := old
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	in.Source, in.FR24Key = strings.TrimSpace(in.Source), strings.TrimSpace(in.FR24Key)
	in.ADSBXUUID = old.ADSBXUUID // never set by the panel
	switch {
	case in.Lat < -90 || in.Lat > 90 || in.Lon < -180 || in.Lon > 180 || math.IsNaN(in.Lat) || math.IsNaN(in.Lon):
		fail(w, invalid("Niepoprawne współrzędne anteny."))
		return
	case in.MaxRangeKm < 0 || in.MaxRangeKm > 1000 || in.MaxAltM < 0 || in.MaxAltM > 30000:
		fail(w, invalid("Zasięg: 0–1000 km, wysokość: 0–30 000 m."))
		return
	case in.FR24 && in.FR24Key == "":
		fail(w, invalid("Podaj klucz Flightradar24 (sharing key)."))
		return
	case in.FR24 && !reFR24KeyPanel.MatchString(in.FR24Key):
		fail(w, invalid("Klucz Flightradar24 składa się z liter i cyfr (zwykle 16 znaków)."))
		return
	case (in.ADSBX || in.FR24) && in.Lat == 0 && in.Lon == 0:
		fail(w, invalid("Ustaw położenie anteny — serwisy ADS-B go wymagają."))
		return
	}
	if in.Source != "" && !filepath.IsAbs(in.Source) && !strings.HasPrefix(in.Source, "http://") && !strings.HasPrefix(in.Source, "https://") {
		fail(w, invalid("Źródło danych to ścieżka do pliku aircraft.json albo adres http://…"))
		return
	}
	groups := []string{}
	for _, g := range in.Groups {
		if g = strings.TrimSpace(g); g != "" && !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	in.Groups = groups
	servers := []string{}
	for _, id := range in.Servers {
		if a.serverByID(id) == nil {
			fail(w, invalid("Nie ma serwera "+id+"."))
			return
		}
		if !slices.Contains(servers, id) {
			servers = append(servers, id)
		}
	}
	in.Servers = servers
	if in.ADSBX && in.ADSBXUUID == "" {
		in.ADSBXUUID = newUUID()
	}
	cfg, err := a.cfg.Update(func(c *config.Config) error { c.ADSB = in; return nil })
	if err != nil {
		fail(w, err)
		return
	}
	steps := a.applyADSB(old, cfg.ADSB)
	a.adsb.mu.Lock()
	a.adsb.detected = time.Time{} // look for the source again
	if cfg.ADSB.Source == "" {
		a.adsb.source = ""
	}
	a.adsb.mu.Unlock()
	writeJSON(w, 200, map[string]any{"config": adsbView(cfg.ADSB), "steps": steps})
}

var reFR24KeyPanel = regexp.MustCompile(`^[A-Za-z0-9]{8,40}$`)

// applyADSB pushes the settings to the decoder and the feeders on the Pi.
func (a *App) applyADSB(old, cur config.ADSB) []adsbStep {
	steps := []adsbStep{}
	if !a.helper.Available() {
		return steps
	}
	add := func(name string, err error) {
		s := adsbStep{Step: name, OK: err == nil}
		if err != nil {
			s.Error = err.Error()
			a.log.Warn("ADS-B", "krok", name, "err", err)
		}
		steps = append(steps, s)
	}
	moved := old.Lat != cur.Lat || old.Lon != cur.Lon
	if moved && system.ReadsbBinary() != "" && (cur.Lat != 0 || cur.Lon != 0) {
		add("Położenie anteny w dekoderze", a.helper.Run("", "adsb-location", fmtFloat(cur.Lat), fmtFloat(cur.Lon)))
	}
	if system.FileExists("/usr/bin/fr24feed") && (cur.FR24 != old.FR24 || cur.FR24Key != old.FR24Key) {
		if cur.FR24 {
			add("Flightradar24", a.helper.Run(cur.FR24Key, "adsb-fr24", "on"))
		} else {
			add("Flightradar24 (wyłączenie)", a.helper.Run("", "adsb-fr24", "off"))
		}
	}
	if cur.ADSBX != old.ADSBX || cur.ADSBX && moved {
		if cur.ADSBX {
			add("ADS-B Exchange", a.helper.Run("", "adsb-adsbx", "on", cur.ADSBXUUID, fmtFloat(cur.Lat), fmtFloat(cur.Lon)))
		} else {
			add("ADS-B Exchange (wyłączenie)", a.helper.Run("", "adsb-adsbx", "off"))
		}
	}
	return steps
}

func fmtFloat(v float64) string { return fmt.Sprintf("%.5f", v) }

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// hADSBInstall installs the decoder or fr24feed in the background; the panel follows the job
// through GET /api/adsb.
func (a *App) hADSBInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Component string `json:"component"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	action := map[string]string{"readsb": "adsb-install-readsb", "fr24": "adsb-install-fr24"}[req.Component]
	if action == "" {
		fail(w, invalid("Nieznany składnik."))
		return
	}
	if !a.helper.Available() {
		fail(w, invalid("Instalacja jest możliwa tylko na Raspberry Pi z ChameleonTAK."))
		return
	}
	b := a.adsb
	b.mu.Lock()
	if b.job.Running {
		b.mu.Unlock()
		fail(w, invalid("Trwa już inna instalacja."))
		return
	}
	now := time.Now()
	b.job = adsbJob{Component: req.Component, Running: true, Started: &now}
	b.mu.Unlock()
	a.log.Info("ADS-B: instalacja", "składnik", req.Component)
	go func() {
		err := a.helper.Run("", action)
		end := time.Now()
		b.mu.Lock()
		b.job.Running, b.job.Finished = false, &end
		if err != nil {
			b.job.Error = err.Error()
		}
		b.detected = time.Time{}
		b.mu.Unlock()
		if err != nil {
			a.log.Error("ADS-B: instalacja nie powiodła się", "składnik", req.Component, "err", err)
			return
		}
		a.log.Info("ADS-B: zainstalowano", "składnik", req.Component)
		if err := a.applyFirewall(); err != nil && err != system.ErrUnsupported {
			a.log.Error("firewall", "err", err)
		}
		// Apply the saved settings to what was just installed.
		cur := a.cfg.Get().ADSB
		a.applyADSB(config.ADSB{}, cur)
	}()
	writeJSON(w, 202, map[string]bool{"started": true})
}
