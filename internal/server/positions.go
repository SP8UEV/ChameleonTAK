package server

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"chameleontak/internal/cot"
	"chameleontak/internal/store"
)

// Position is one item on the live map: a connected contact or a shared marker.
type Position struct {
	UID      string    `json:"uid"`
	Callsign string    `json:"callsign"`
	Username string    `json:"username"`
	Kind     string    `json:"kind"` // contact | marker
	Type     string    `json:"type"`
	Lat      float64   `json:"lat"`
	Lon      float64   `json:"lon"`
	Hae      float64   `json:"hae"`
	Course   float64   `json:"course"`
	Speed    float64   `json:"speed"`
	Battery  *int      `json:"battery"`
	Team     string    `json:"team"`
	Role     string    `json:"role"`
	Online   bool      `json:"online"`
	LastSeen time.Time `json:"lastSeen"`
	Stale    time.Time `json:"stale"`
	Remarks  string    `json:"remarks"`

	groups []string // groups of the client that sent it (not exported)
}

type Positions struct {
	st  *store.Store
	hub *Hub

	mu    sync.Mutex
	items map[string]*Position
	dirty map[string]bool
	raw   map[string][]byte // latest CoT per UID (memory only), used to add items to missions
}

func (p *Positions) Get(uid string) *Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	if pos, ok := p.items[uid]; ok {
		cp := *pos
		return &cp
	}
	return nil
}

func (p *Positions) Raw(uid string) []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.raw[uid]
}

func NewPositions(st *store.Store, hub *Hub) *Positions {
	p := &Positions{st: st, hub: hub, items: map[string]*Position{}, dirty: map[string]bool{}, raw: map[string][]byte{}}
	if saved, err := st.LoadPositions(time.Now().Add(-7 * 24 * time.Hour)); err == nil {
		for uid, raw := range saved {
			var pos Position
			if json.Unmarshal(raw, &pos) == nil {
				pos.Online = false
				p.items[uid] = &pos
			}
		}
	}
	return p
}

func (p *Positions) Run(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.expireMarkers()
			p.Flush()
		}
	}
}

func (p *Positions) Flush() {
	p.mu.Lock()
	items := map[string][]byte{}
	seen := map[string]time.Time{}
	for uid := range p.dirty {
		if pos, ok := p.items[uid]; ok {
			raw, _ := json.Marshal(pos)
			items[uid], seen[uid] = raw, pos.LastSeen
		}
	}
	p.dirty = map[string]bool{}
	p.mu.Unlock()
	if len(items) > 0 {
		_ = p.st.SavePositions(items, seen)
	}
}

func (p *Positions) expireMarkers() {
	now := time.Now()
	var removed []string
	p.mu.Lock()
	for uid, pos := range p.items {
		if pos.Kind == "marker" && !pos.Stale.IsZero() && pos.Stale.Before(now) {
			delete(p.items, uid)
			delete(p.raw, uid)
			removed = append(removed, uid)
		}
	}
	p.mu.Unlock()
	for _, uid := range removed {
		_ = p.st.DeletePosition(uid)
		p.hub.Publish("remove", map[string]string{"uid": uid})
	}
}

// isMapItem selects events worth showing on the panel map.
func isMapItem(ev *cot.Event) bool {
	if ev.Lat == 0 && ev.Lon == 0 {
		return false
	}
	return strings.HasPrefix(ev.Type, "a-") || strings.HasPrefix(ev.Type, "b-m-p") || strings.HasPrefix(ev.Type, "u-d-")
}

// Update records an event from a connected client.
func (p *Positions) Update(ev *cot.Event, username string, isOwnSA bool, groups []string) {
	if ev.IsDelete() {
		for _, l := range ev.Links {
			p.Remove(l.UID)
		}
		return
	}
	if !isMapItem(ev) {
		return
	}
	pos := &Position{
		UID: ev.UID, Callsign: ev.Callsign, Type: ev.Type, Lat: ev.Lat, Lon: ev.Lon, Hae: ev.Hae,
		Course: ev.Course, Speed: ev.Speed, Team: ev.GroupName, Role: ev.GroupRole,
		LastSeen: time.Now().UTC(), Stale: ev.Stale, Remarks: ev.Remarks, Kind: "marker", groups: groups,
	}
	if isOwnSA {
		pos.Kind, pos.Online, pos.Username = "contact", true, username
	}
	if b, err := strconv.Atoi(ev.Battery); err == nil {
		pos.Battery = &b
	}
	p.mu.Lock()
	if old, ok := p.items[ev.UID]; ok && old.Kind == "contact" && !isOwnSA {
		// Another client relaying someone's SA must not turn a contact into a marker.
		p.mu.Unlock()
		return
	}
	if _, known := p.items[ev.UID]; !known && len(p.items) >= maxMapItems {
		p.evictOldestMarker()
	}
	p.items[ev.UID] = pos
	if len(ev.Raw) <= maxRawCoT {
		p.raw[ev.UID] = ev.Raw
	} else {
		delete(p.raw, ev.UID)
	}
	p.dirty[ev.UID] = true
	p.mu.Unlock()
	p.hub.Publish("position", pos)
}

func (p *Positions) SetOffline(uid string) {
	p.mu.Lock()
	pos, ok := p.items[uid]
	if ok {
		pos.Online = false
		pos.LastSeen = time.Now().UTC()
		p.dirty[uid] = true
	}
	var cp Position
	if ok {
		cp = *pos
	}
	p.mu.Unlock()
	if ok {
		p.hub.Publish("position", cp)
	}
}

func (p *Positions) Remove(uid string) {
	p.mu.Lock()
	_, ok := p.items[uid]
	delete(p.items, uid)
	delete(p.raw, uid)
	delete(p.dirty, uid)
	p.mu.Unlock()
	if ok {
		_ = p.st.DeletePosition(uid)
		p.hub.Publish("remove", map[string]string{"uid": uid})
	}
}

func (p *Positions) List() []Position {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Position, 0, len(p.items))
	for _, pos := range p.items {
		out = append(out, *pos)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Callsign < out[j].Callsign })
	return out
}

// Bounds that keep a misbehaving or malicious client from exhausting memory on a small Pi.
const (
	maxMapItems = 5000
	maxRawCoT   = 64 << 10
)

// evictOldestMarker drops the least recently seen marker (contacts are kept). Caller holds p.mu.
func (p *Positions) evictOldestMarker() {
	oldest := ""
	var at time.Time
	for uid, pos := range p.items {
		if pos.Kind == "marker" && (oldest == "" || pos.LastSeen.Before(at)) {
			oldest, at = uid, pos.LastSeen
		}
	}
	if oldest != "" {
		delete(p.items, oldest)
		delete(p.raw, oldest)
		delete(p.dirty, oldest)
	}
}
