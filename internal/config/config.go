// Package config holds the server configuration and the catalogue of supported client platforms.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type Ports struct {
	CoTTLS        int  `json:"cotTls"`
	CoTTCP        int  `json:"cotTcp"`
	CoTTCPEnabled bool `json:"cotTcpEnabled"`
	Marti         int  `json:"marti"`
	Enroll        int  `json:"enroll"`
	HTTP          int  `json:"http"`
	HTTPS         int  `json:"https"`
}

type LetsEncrypt struct {
	Enabled bool   `json:"enabled"`
	Email   string `json:"email"`
}

// Profile is applied to every device profile / data package the server generates.
type Profile struct {
	DefaultTeam  string            `json:"defaultTeam"`
	DefaultRole  string            `json:"defaultRole"`
	LockSettings bool              `json:"lockSettings"`
	ExtraPrefs   map[string]string `json:"extraPrefs"`
}

// DDNS keeps the domain's DNS record pointing at the router's public address.
type DDNS struct {
	Provider string `json:"provider"` // "" (off) | duckdns | cloudflare | url
	Token    string `json:"token"`    // DuckDNS token or Cloudflare API token
	URL      string `json:"url"`      // update URL of any other provider; {ip} and {domain} are substituted
}

// PanelAccess limits the admin panel to listed addresses; the join pages stay open to phones.
type PanelAccess struct {
	Restrict bool     `json:"restrict"`
	Allow    []string `json:"allow"` // IP addresses, networks (CIDR) and MAC addresses
}

// ADSB configures the ADS-B receiver on the Pi: aircraft go to TAK clients as CoT, and the
// decoder can feed Flightradar24 and ADS-B Exchange.
type ADSB struct {
	Bridge     bool     `json:"bridge"` // send aircraft to TAK clients
	Source     string   `json:"source"` // aircraft.json path or URL; "" = detect
	Lat        float64  `json:"lat"`    // antenna position
	Lon        float64  `json:"lon"`
	AltM       float64  `json:"altM"`       // antenna height above sea level
	MaxRangeKm int      `json:"maxRangeKm"` // 0 = no limit
	MaxAltM    int      `json:"maxAltM"`    // 0 = no limit
	Groups     []string `json:"groups"`     // empty = every client
	Servers    []string `json:"servers"`    // servers that get the aircraft; empty = the main one
	FR24       bool     `json:"fr24"`
	FR24Key    string   `json:"fr24Key"`
	ADSBX      bool     `json:"adsbx"`
	ADSBXUUID  string   `json:"adsbxUuid"`
}

// ZeroTier is the virtual network the device joined for access without a public address.
type ZeroTier struct {
	Network string `json:"network"` // 16-digit network ID; "" = not joined
}

type Network struct {
	Mode            string `json:"mode"` // lan | hotspot
	StaticIP        bool   `json:"staticIp"`
	WifiSSID        string `json:"wifiSsid"`
	HotspotSSID     string `json:"hotspotSsid"`
	HotspotPassword string `json:"hotspotPassword"`
}

type Config struct {
	SetupDone          bool        `json:"setupDone"`
	NodeID             string      `json:"nodeId"`
	ServerName         string      `json:"serverName"`
	DisplayName        string      `json:"displayName"`
	Organization       string      `json:"organization"`
	Ports              Ports       `json:"ports"`
	Platforms          []string    `json:"platforms"`
	CertValidityDays   int         `json:"certValidityDays"`
	TokenValidityHours int         `json:"tokenValidityHours"`
	CertPassword       string      `json:"certPassword"`
	ForceHTTPS         bool        `json:"forceHttps"`
	LetsEncrypt        LetsEncrypt `json:"letsEncrypt"`
	Profile            Profile     `json:"profile"`
	MapTileURL         string      `json:"mapTileUrl"`
	SSHEnabled         bool        `json:"sshEnabled"`
	ProfileUpdatedAt   time.Time   `json:"profileUpdatedAt"` // last change of the device profile settings
	Network            Network     `json:"network"`
	DDNS               DDNS        `json:"ddns"`
	PanelAccess        PanelAccess `json:"panelAccess"`
	ADSB               ADSB        `json:"adsb"`
	ZeroTier           ZeroTier    `json:"zerotier"`
}

func DefaultPorts() Ports {
	return Ports{CoTTLS: 8089, CoTTCP: 8087, Marti: 8443, Enroll: 8446, HTTP: 80, HTTPS: 443}
}

func Default() Config {
	return Config{
		NodeID:             randomHex(16),
		DisplayName:        "ChameleonTAK",
		Organization:       "ChameleonTAK",
		Ports:              DefaultPorts(),
		Platforms:          []string{"atak", "itak"},
		CertValidityDays:   365,
		TokenValidityHours: 168,
		CertPassword:       "atakatak",
		Profile: Profile{
			DefaultTeam: "Cyan",
			DefaultRole: "Team Member",
			ExtraPrefs:  map[string]string{},
		},
		MapTileURL: "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
		ADSB:       ADSB{MaxRangeKm: 150},
		Network:    Network{Mode: "lan", HotspotSSID: "ChameleonTAK"},
	}
}

// Platform describes a TAK client the operator can enable in the panel.
type Platform struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var Platforms = []Platform{
	{Key: "atak", Label: "ATAK (Android)"},
	{Key: "itak", Label: "iTAK (iOS)"},
	{Key: "wintak", Label: "WinTAK (Windows)"},
	{Key: "takaware", Label: "TAKAware (iOS)"},
}

func PlatformLabel(key string) string {
	for _, p := range Platforms {
		if p.Key == key {
			return p.Label
		}
	}
	return key
}

func ValidPlatform(key string) bool {
	for _, p := range Platforms {
		if p.Key == key {
			return true
		}
	}
	return false
}

// Store persists the configuration; implemented by store.Store.
type Store interface {
	GetSetting(key string, v any) (bool, error)
	SetSetting(key string, v any) error
}

// Manager guards the live configuration and writes every change through to the store.
type Manager struct {
	mu  sync.RWMutex
	cfg Config
	st  Store
}

const settingsKey = "config"

func Load(st Store) (*Manager, error) {
	cfg := Default()
	found, err := st.GetSetting(settingsKey, &cfg)
	if err != nil {
		return nil, err
	}
	m := &Manager{cfg: cfg, st: st}
	if !found {
		if err := st.SetSetting(settingsKey, cfg); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.cfg
	c.Platforms = append([]string(nil), m.cfg.Platforms...)
	c.PanelAccess.Allow = append([]string(nil), m.cfg.PanelAccess.Allow...)
	c.ADSB.Groups = append([]string(nil), m.cfg.ADSB.Groups...)
	c.ADSB.Servers = append([]string(nil), m.cfg.ADSB.Servers...)
	c.Profile.ExtraPrefs = make(map[string]string, len(m.cfg.Profile.ExtraPrefs))
	for k, v := range m.cfg.Profile.ExtraPrefs {
		c.Profile.ExtraPrefs[k] = v
	}
	return c
}

// Update applies fn to a copy of the config and persists it; the live config only changes if saving succeeds.
func (m *Manager) Update(fn func(c *Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.cfg
	c.Platforms = append([]string(nil), m.cfg.Platforms...)
	c.PanelAccess.Allow = append([]string(nil), m.cfg.PanelAccess.Allow...)
	c.ADSB.Groups = append([]string(nil), m.cfg.ADSB.Groups...)
	c.ADSB.Servers = append([]string(nil), m.cfg.ADSB.Servers...)
	extra := make(map[string]string, len(m.cfg.Profile.ExtraPrefs))
	for k, v := range m.cfg.Profile.ExtraPrefs {
		extra[k] = v
	}
	c.Profile.ExtraPrefs = extra
	if err := fn(&c); err != nil {
		return m.cfg, err
	}
	if err := m.st.SetSetting(settingsKey, c); err != nil {
		return m.cfg, err
	}
	m.cfg = c
	return c, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
