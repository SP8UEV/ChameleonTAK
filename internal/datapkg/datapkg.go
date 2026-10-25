// Package datapkg builds TAK data packages (zip) and QR payloads for client onboarding.
package datapkg

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"chameleontak/internal/config"
)

type Params struct {
	Platform string
	Config   config.Config
	Username string
	Callsign string
	Team     string
	Role     string

	ClientP12 []byte // client certificate + key (connection packages)
	TrustP12  []byte // CA trust store
}

// NativeQR is the payload for the QR scanner built into the TAK app itself, or "" when that path
// cannot work. ATAK and iTAK only enroll against a publicly trusted certificate on the enrollment
// port (no trust prompt), so with our private CA they need the data package instead; TAKAware
// accepts any certificate during enrollment.
func NativeQR(platform string, cfg config.Config, username, token string) string {
	if (platform == "atak" || platform == "itak") && !cfg.LetsEncrypt.Enabled {
		return ""
	}
	switch platform {
	case "atak":
		// The host may carry the streaming port; the enrollment port always comes from the
		// device setting (default 8446), so a custom enrollment port cannot be expressed here.
		host := cfg.ServerName
		if cfg.Ports.CoTTLS != 8089 {
			host = fmt.Sprintf("%s:%d", host, cfg.Ports.CoTTLS)
		}
		q := url.Values{}
		q.Set("host", host)
		q.Set("username", username)
		q.Set("token", token)
		return "tak://com.atakmap.app/enroll?" + q.Encode()
	case "itak", "takaware":
		// iTAK/TAKAware "Scan QR": description,host,port,protocol, then username/password are asked for.
		return fmt.Sprintf("%s,%s,%d,ssl", strings.ReplaceAll(cfg.DisplayName, ",", " "), cfg.ServerName, cfg.Ports.CoTTLS)
	}
	return ""
}

type entry struct {
	key, class, value string
}

func str(k, v string) entry { return entry{k, "class java.lang.String", v} }
func boolean(k string, v bool) entry {
	return entry{k, "class java.lang.Boolean", strconv.FormatBool(v)}
}
func integer(k string, v int) entry { return entry{k, "class java.lang.Integer", strconv.Itoa(v)} }

type section struct {
	name    string
	entries []entry
}

func renderPrefs(sections []section) []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version='1.0' encoding='ASCII' standalone='yes'?>\n<preferences>\n")
	for _, s := range sections {
		fmt.Fprintf(&b, "  <preference version=\"1\" name=\"%s\">\n", xmlAttr(s.name))
		for _, e := range s.entries {
			fmt.Fprintf(&b, "    <entry key=\"%s\" class=\"%s\">%s</entry>\n", xmlAttr(e.key), e.class, xmlText(e.value))
		}
		b.WriteString("  </preference>\n")
	}
	b.WriteString("</preferences>\n")
	return b.Bytes()
}

// identityPrefs sets callsign/team/role and operator-defined extras.
func identityPrefs(p Params) []entry {
	cfg := p.Config
	team, role := p.Team, p.Role
	if team == "" {
		team = cfg.Profile.DefaultTeam
	}
	if role == "" {
		role = cfg.Profile.DefaultRole
	}
	var out []entry
	if p.Callsign != "" {
		out = append(out, str("locationCallsign", p.Callsign))
	}
	if team != "" {
		out = append(out, str("locationTeam", team))
	}
	if role != "" {
		out = append(out, str("atakRoleType", role))
	}
	out = append(out, boolean("displayServerConnectionWidget", true))
	// ATAK asks the server for an updated profile on every connect (changes made in the panel).
	out = append(out, boolean("deviceProfileEnableOnConnect", true))
	keys := make([]string, 0, len(cfg.Profile.ExtraPrefs))
	for k := range cfg.Profile.ExtraPrefs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, typed(k, cfg.Profile.ExtraPrefs[k]))
	}
	return out
}

// typed guesses the Java type of an operator-provided preference value.
func typed(k, v string) entry {
	if v == "true" || v == "false" {
		return entry{k, "class java.lang.Boolean", v}
	}
	if _, err := strconv.Atoi(v); err == nil && len(v) < 10 {
		return entry{k, "class java.lang.Integer", v}
	}
	return str(k, v)
}

func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "tak"
	}
	return b.String()
}

// Connection builds the data package that configures the server connection with certificates.
func Connection(p Params) ([]byte, error) {
	cfg := p.Config
	if len(p.ClientP12) == 0 || len(p.TrustP12) == 0 {
		return nil, fmt.Errorf("brak certyfikatów do paczki")
	}
	trustName := "truststore-" + safeName(cfg.DisplayName) + ".p12"
	clientName := safeName(p.Username) + ".p12"
	connect := fmt.Sprintf("%s:%d:ssl", cfg.ServerName, cfg.Ports.CoTTLS)

	switch p.Platform {
	case "itak", "takaware":
		// iOS clients take a flat archive and read the certificates from the non-indexed keys.
		streams := section{name: "cot_streams", entries: []entry{
			integer("count", 1),
			str("description0", cfg.DisplayName),
			boolean("enabled0", true),
			str("connectString0", connect),
		}}
		app := section{name: "com.atakmap.app_preferences", entries: append([]entry{
			str("caLocation", "cert/"+trustName),
			str("caPassword", cfg.CertPassword),
			str("certificateLocation", "cert/"+clientName),
			str("clientPassword", cfg.CertPassword),
		}, identityPrefs(p)...)}
		return buildZip("", []file{
			{name: "config.pref", data: renderPrefs([]section{streams, app})},
			{name: trustName, data: p.TrustP12},
			{name: clientName, data: p.ClientP12},
		}, false)
	}

	// ATAK / WinTAK: indexed cot_streams keys; certificates land in atak/cert/<basename>.
	streams := section{name: "cot_streams", entries: []entry{
		integer("count", 1),
		str("description0", cfg.DisplayName),
		boolean("enabled0", true),
		str("connectString0", connect),
		str("caLocation0", "cert/"+trustName),
		str("caPassword0", cfg.CertPassword),
		str("certificateLocation0", "cert/"+clientName),
		str("clientPassword0", cfg.CertPassword),
	}}
	app := section{name: "com.atakmap.app_preferences", entries: identityPrefs(p)}
	inner, err := buildZip(cfg.DisplayName+" - "+p.Username, []file{
		{name: "certs/config.pref", data: renderPrefs([]section{streams, app})},
		{name: "certs/" + trustName, data: p.TrustP12},
		{name: "certs/" + clientName, data: p.ClientP12},
	}, true)
	if err != nil || p.Platform != "wintak" {
		return inner, err
	}
	// WinTAK: the package wrapped in an outer package, as OpenTAKServer/FreeTAKServer ship it.
	return buildZip(cfg.DisplayName+"_CONFIG", []file{
		{name: newUUID() + "/" + safeName(p.Username) + ".zip", data: inner},
	}, true)
}

// Profile builds the preferences-only package pushed after enrollment (callsign, team, role, extras).
func Profile(p Params) ([]byte, error) {
	prefs := renderPrefs([]section{{name: "com.atakmap.app_preferences", entries: identityPrefs(p)}})
	return buildZip("Profil "+p.Config.DisplayName, []file{{name: "prefs/profile.pref", data: prefs}}, true)
}

type file struct {
	name string
	data []byte
}

func buildZip(pkgName string, files []file, manifest bool) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mod := time.Now()
	write := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: mod})
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if manifest {
		if err := write("MANIFEST/manifest.xml", manifestXML(pkgName, files)); err != nil {
			return nil, err
		}
	}
	for _, f := range files {
		if err := write(f.name, f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func manifestXML(name string, files []file) []byte {
	var b bytes.Buffer
	b.WriteString("<MissionPackageManifest version=\"2\">\n  <Configuration>\n")
	fmt.Fprintf(&b, "    <Parameter name=\"uid\" value=\"%s\"/>\n", newUUID())
	fmt.Fprintf(&b, "    <Parameter name=\"name\" value=\"%s\"/>\n", xmlAttr(name))
	b.WriteString("    <Parameter name=\"onReceiveDelete\" value=\"true\"/>\n  </Configuration>\n  <Contents>\n")
	for _, f := range files {
		fmt.Fprintf(&b, "    <Content ignore=\"false\" zipEntry=\"%s\"/>\n", xmlAttr(f.name))
	}
	b.WriteString("  </Contents>\n</MissionPackageManifest>\n")
	return b.Bytes()
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func xmlAttr(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

func xmlText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}
