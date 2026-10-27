// Package stream implements the CoT streaming servers and the message router.
package stream

import (
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"chameleontak/internal/cot"
)

const AnonGroup = "__ANON__"

// Identity is who a connection belongs to, resolved from its client certificate.
type Identity struct {
	UserID     int64
	Username   string
	DeviceID   int64
	CertSerial string
	Groups     []string
}

type Client struct {
	ID          string
	Transport   string // tls | tcp
	IP          string
	ConnectedAt time.Time
	Identity    Identity

	conn      net.Conn
	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once
	drops     atomic.Int64
	received  atomic.Int64 // events read from the client, for diagnostics
	announced atomic.Bool

	mu       sync.RWMutex
	uid      string
	callsign string
	saType   string
	platform string
	version  string
	groups   []string
}

// Info is a snapshot for the panel.
type Info struct {
	ID          string    `json:"id"`
	UID         string    `json:"uid"`
	Callsign    string    `json:"callsign"`
	Username    string    `json:"username"`
	UserID      int64     `json:"userId"`
	DeviceID    int64     `json:"deviceId"`
	Platform    string    `json:"platform"`
	Version     string    `json:"version"`
	IP          string    `json:"ip"`
	Transport   string    `json:"transport"`
	ConnectedAt time.Time `json:"connectedAt"`
	Groups      []string  `json:"groups"`
}

func (c *Client) Info() Info {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Info{
		ID: c.ID, UID: c.uid, Callsign: c.callsign, Username: c.Identity.Username, UserID: c.Identity.UserID,
		DeviceID: c.Identity.DeviceID, Platform: c.platform, Version: c.version, IP: c.IP, Transport: c.Transport,
		ConnectedAt: c.ConnectedAt, Groups: append([]string(nil), c.groups...),
	}
}

func (c *Client) UID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uid
}

func (c *Client) Callsign() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.callsign
}

func (c *Client) Groups() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.groups
}

// Announce reports true once per connection, when its first own SA has been recorded (the panel
// then learns the callsign and the app).
func (c *Client) Announce() bool { return !c.announced.Swap(true) }

// Send queues raw bytes for the client without blocking the router.
func (c *Client) Send(raw []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.out <- raw:
	default:
		if c.drops.Add(1) > 2000 {
			c.Close()
		}
	}
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *Client) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case raw := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if _, err := c.conn.Write(raw); err != nil {
				c.Close()
				return
			}
			// A drained queue means the client keeps up; forget earlier bursts.
			if len(c.out) == 0 {
				c.drops.Store(0)
			}
		}
	}
}

// Hooks lets the application observe traffic (map, chat log, missions, device tracking).
type Hooks interface {
	OnConnect(c *Client)
	OnDisconnect(c *Client)
	// OnEvent is called for every relayed event after routing.
	OnEvent(c *Client, ev *cot.Event)
	// MissionSubscribers returns client UIDs subscribed to a mission; the event is also stored by the hook.
	MissionEvent(c *Client, ev *cot.Event, mission string) []string
	// Disconnected builds the event announcing that a client went offline (nil = nothing).
	Disconnected(c *Client) []byte
}

type saEntry struct {
	raw    []byte
	stale  time.Time
	client *Client
}

type Router struct {
	log   *slog.Logger
	hooks Hooks

	mu      sync.RWMutex
	clients map[string]*Client
	sa      map[string]*saEntry // by client UID
	seq     atomic.Int64
}

func NewRouter(log *slog.Logger, hooks Hooks) *Router {
	return &Router{log: log, hooks: hooks, clients: map[string]*Client{}, sa: map[string]*saEntry{}}
}

func (r *Router) newClient(conn net.Conn, transport string, id Identity) *Client {
	groups := id.Groups
	if len(groups) == 0 {
		groups = []string{AnonGroup}
	}
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	return &Client{
		ID:          transport + "-" + itoa(r.seq.Add(1)),
		Transport:   transport,
		IP:          ip,
		ConnectedAt: time.Now().UTC(),
		Identity:    id,
		conn:        conn,
		out:         make(chan []byte, 1024),
		done:        make(chan struct{}),
		groups:      groups,
	}
}

func (r *Router) register(c *Client) {
	r.mu.Lock()
	r.clients[c.ID] = c
	r.mu.Unlock()

	// Bring the newcomer up to date with everyone currently visible to it.
	r.ReplaySA(c)
	r.hooks.OnConnect(c)
}

// SAType is the CoT type of the client's own position reports (e.g. a-f-G-U-C).
func (c *Client) SAType() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saType
}

// ReplaySA sends the client the latest SA of every visible online client (TAK "latestSA").
func (r *Router) ReplaySA(c *Client) {
	now := time.Now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.sa {
		if e.client != c && e.stale.After(now) && shareGroup(e.client.Groups(), c.Groups()) {
			c.Send(e.raw)
		}
	}
}

func (r *Router) unregister(c *Client) {
	r.mu.Lock()
	delete(r.clients, c.ID)
	uid := c.UID()
	if e, ok := r.sa[uid]; ok && e.client == c {
		delete(r.sa, uid)
	}
	replaced := false
	for _, other := range r.clients {
		if uid != "" && other.UID() == uid {
			replaced = true
		}
	}
	r.mu.Unlock()
	if uid != "" && !replaced {
		if msg := r.hooks.Disconnected(c); msg != nil {
			r.broadcast(msg, c.Groups(), c)
		}
	}
	r.hooks.OnDisconnect(c)
}

// Handle routes one event received from a client.
func (r *Router) Handle(c *Client, ev *cot.Event) {
	if ev.IsPing() {
		c.Send(cot.Pong())
		return
	}
	if ev.IsControl() {
		return
	}

	// A UID belongs to whoever reports it as their own position. Another user's connection may
	// neither take it over nor publish events under it (spoofing, kicking the victim offline).
	r.mu.RLock()
	holder, held := r.sa[ev.UID]
	r.mu.RUnlock()
	if held && holder.client != c && !sameOwner(holder.client, c) {
		r.log.Warn("odrzucono zdarzenie z cudzym UID", "uid", ev.UID, "user", c.Identity.Username, "owner", holder.client.Identity.Username)
		return
	}

	if ev.IsSA() {
		c.mu.Lock()
		first := c.uid == ""
		if c.uid == "" || c.uid == ev.UID {
			c.uid = ev.UID
			c.callsign = ev.Callsign
			c.saType = ev.Type
			if ev.TakvPlatform != "" {
				c.platform = strings.TrimSpace(ev.TakvPlatform)
				c.version = strings.TrimSpace(ev.TakvVersion)
			}
		}
		owner := c.uid == ev.UID
		c.mu.Unlock()
		if owner {
			stale := ev.Stale
			if stale.IsZero() {
				stale = time.Now().Add(2 * time.Minute)
			}
			r.mu.Lock()
			if first {
				// A reconnecting device takes over its UID from a stale session.
				if old, ok := r.sa[ev.UID]; ok && old.client != c {
					go old.client.Close()
				}
			}
			r.sa[ev.UID] = &saEntry{raw: ev.Raw, stale: stale, client: c}
			r.mu.Unlock()
		}
	}

	allStreaming := false
	for _, d := range ev.Dest {
		if d.Callsign == "All Streaming" {
			allStreaming = true
		}
	}
	if len(ev.Dest) > 0 && !allStreaming {
		r.deliverDirect(c, ev)
	} else {
		r.broadcast(ev.Raw, c.Groups(), c)
	}
	r.hooks.OnEvent(c, ev)
}

func (r *Router) deliverDirect(from *Client, ev *cot.Event) {
	targets := map[*Client]bool{}
	callsigns, uids, missionUIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, d := range ev.Dest {
		switch {
		case d.Mission != "":
			for _, uid := range r.hooks.MissionEvent(from, ev, d.Mission) {
				missionUIDs[uid] = true
			}
		case d.UID != "":
			uids[d.UID] = true
		case d.Callsign != "":
			callsigns[d.Callsign] = true
		}
	}
	fromGroups := from.Groups()
	r.mu.RLock()
	for _, c := range r.clients {
		if c == from {
			continue
		}
		uid := c.UID()
		switch {
		case missionUIDs[uid]:
			// Mission subscribers were authorized when they subscribed.
			targets[c] = true
		case uids[uid] || callsigns[c.Callsign()]:
			// Direct messages still respect group isolation.
			if shareGroup(fromGroups, c.Groups()) {
				targets[c] = true
			}
		}
	}
	r.mu.RUnlock()
	for c := range targets {
		c.Send(ev.Raw)
	}
}

// broadcast sends to every client sharing a group with groups (nil groups = everyone).
func (r *Router) broadcast(raw []byte, groups []string, except *Client) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if c == except {
			continue
		}
		if groups == nil || shareGroup(groups, c.Groups()) {
			c.Send(raw)
		}
	}
}

// Broadcast sends a server-originated event to all clients (groups == nil) or to members of groups.
func (r *Router) Broadcast(raw []byte, groups []string) { r.broadcast(raw, groups, nil) }

// SendToUIDs delivers raw bytes to the clients with the given UIDs.
func (r *Router) SendToUIDs(raw []byte, uids []string) {
	want := map[string]bool{}
	for _, u := range uids {
		want[u] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if want[c.UID()] {
			c.Send(raw)
		}
	}
}

func (r *Router) SendToCallsign(raw []byte, callsign string) bool {
	sent := false
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if c.Callsign() == callsign {
			c.Send(raw)
			sent = true
		}
	}
	return sent
}

func (r *Router) Clients() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.clients))
	for _, c := range r.clients {
		out = append(out, c.Info())
	}
	return out
}

func (r *Router) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

func (r *Router) Kick(id string) bool {
	r.mu.RLock()
	c, ok := r.clients[id]
	r.mu.RUnlock()
	if ok {
		c.Close()
	}
	return ok
}

// ClientByUID returns the connected client with the given CoT UID.
func (r *Router) ClientByUID(uid string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if c.UID() == uid {
			return c
		}
	}
	return nil
}

// ClientByCallsign returns a connected client with the given callsign.
func (r *Router) ClientByCallsign(callsign string) *Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if strings.EqualFold(c.Callsign(), callsign) {
			return c
		}
	}
	return nil
}

// UpdateUser refreshes group membership of a user's live connections, or disconnects them.
func (r *Router) UpdateUser(userID int64, groups []string, disconnect bool) {
	if len(groups) == 0 {
		groups = []string{AnonGroup}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if c.Identity.UserID != userID || userID == 0 {
			continue
		}
		if disconnect {
			go c.Close()
			continue
		}
		c.mu.Lock()
		c.groups = groups
		c.mu.Unlock()
	}
}

// DisconnectWhere closes every connection matching fn (e.g. a revoked certificate).
func (r *Router) DisconnectWhere(fn func(c *Client) bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.clients {
		if fn(c) {
			go c.Close()
		}
	}
}

func (r *Router) CloseAll() {
	r.DisconnectWhere(func(*Client) bool { return true })
}

func shareGroup(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}

// sameOwner reports whether two connections belong to the same account (a device reconnecting).
// Anonymous TCP clients only match each other.
func sameOwner(a, b *Client) bool {
	return a.Identity.UserID == b.Identity.UserID
}
