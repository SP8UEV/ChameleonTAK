// Package config holds the server configuration and the catalogue of supported client platforms.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
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
	Network            Network     `json:"network"`
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
