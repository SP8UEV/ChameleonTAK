package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ADS-B on the Raspberry Pi: readsb (wiedehopf) decodes 1090 MHz from the RTL-SDR and offers
// Beast data on 127.0.0.1:30005, fr24feed and the ADS-B Exchange connection read from it.

const (
	readsbInstaller = "https://github.com/wiedehopf/adsb-scripts/raw/master/readsb-install.sh"
	fr24Version     = "1.0.53-0"
	fr24DebURL      = "https://repo-feed.flightradar24.com/rpi_binaries/fr24feed_%s_%s.deb"
	fr24Ini         = "/etc/fr24feed.ini"
	adsbxUnit       = "chameleontak-adsbx"
	adsbxUUIDFile   = "/etc/chameleontak/adsbx-uuid"
	// tar1090 (installed with readsb) runs in lighttpd, which must not take the panel's port 80.
	tar1090Port = 8504
)

var (
	reFR24Key = regexp.MustCompile(`^[A-Za-z0-9]{8,40}$`)
	reUUID    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ReadsbBinary is the installed readsb, or "".
func ReadsbBinary() string {
	for _, p := range []string{"/usr/bin/readsb", "/usr/local/bin/readsb"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func runADSB(action string, rest []string, secret func() (string, error)) error {
	switch action {
	case "adsb-install-readsb":
		return installReadsb()
	case "adsb-install-fr24":
		return installFR24()
	case "adsb-location":
		if len(rest) != 2 {
			return errors.New("adsb-location: oczekiwano szerokości i długości geograficznej")
		}
		lat, lon, err := parseLatLon(rest[0], rest[1])
		if err != nil {
			return err
		}
		set := "/usr/local/bin/readsb-set-location"
		if _, err := os.Stat(set); err != nil {
			return errors.New("brak readsb-set-location — położenie anteny ustaw w konfiguracji dekodera")
		}
		return run(set, fmtCoord(lat), fmtCoord(lon))
	case "adsb-fr24":
		if len(rest) != 1 {
			return errors.New("adsb-fr24: oczekiwano on albo off")
		}
		if rest[0] == "off" {
			_ = run("systemctl", "disable", "--now", "fr24feed")
			return nil
		}
		key, err := secret()
		if err != nil {
			return err
		}
		if !reFR24Key.MatchString(key) {
			return errors.New("niepoprawny klucz Flightradar24 (sharing key)")
		}
		if err := os.WriteFile(fr24Ini, []byte(FR24Ini(key)), 0o644); err != nil {
			return err
		}
		if err := run("systemctl", "enable", "fr24feed"); err != nil {
			return err
		}
		return run("systemctl", "restart", "fr24feed")
	case "adsb-adsbx":
		if len(rest) == 1 && rest[0] == "off" {
			_ = run("systemctl", "disable", "--now", adsbxUnit)
			return nil
		}
		if len(rest) != 4 || rest[0] != "on" {
			return errors.New("adsb-adsbx: oczekiwano on UUID LAT LON albo off")
		}
		if !reUUID.MatchString(rest[1]) {
			return errors.New("niepoprawny identyfikator UUID")
		}
		lat, lon, err := parseLatLon(rest[2], rest[3])
		if err != nil {
			return err
		}
		bin := ReadsbBinary()
		if bin == "" {
			return errors.New("najpierw zainstaluj dekoder readsb")
		}
		if err := os.MkdirAll(filepath.Dir(adsbxUUIDFile), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(adsbxUUIDFile, []byte(rest[1]+"\n"), 0o644); err != nil {
			return err
		}
		unit := ADSBXUnit(bin, lat, lon)
		if err := os.WriteFile("/etc/systemd/system/"+adsbxUnit+".service", []byte(unit), 0o644); err != nil {
			return err
		}
		if err := run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := run("systemctl", "enable", adsbxUnit); err != nil {
			return err
		}
		return run("systemctl", "restart", adsbxUnit)
	}
	return fmt.Errorf("nieznana akcja %q", action)
}

func parseLatLon(a, b string) (float64, float64, error) {
	lat, err1 := strconv.ParseFloat(a, 64)
	lon, err2 := strconv.ParseFloat(b, 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, errors.New("niepoprawne współrzędne anteny")
	}
	return lat, lon, nil
}

func fmtCoord(v float64) string { return strconv.FormatFloat(v, 'f', 5, 64) }

// FR24Ini makes fr24feed read Beast data from readsb instead of opening the SDR itself.
func FR24Ini(key string) string {
	return `receiver="beast-tcp"
fr24key="` + key + `"
host="127.0.0.1:30005"
bs="no"
raw="no"
logmode="1"
logpath="/var/log/fr24feed"
mlat="yes"
mlat-without-gps="yes"
`
}

// ADSBXUnit runs a second, network-only readsb that forwards the local Beast data to ADS-B
// Exchange (what their own feed client does), with every listening port off.
func ADSBXUnit(readsb string, lat, lon float64) string {
	return `# Wygenerowane przez ChameleonTAK — nie edytuj ręcznie
[Unit]
Description=ChameleonTAK - dane ADS-B dla ADS-B Exchange
Wants=network-online.target
After=network-online.target readsb.service

[Service]
ExecStart=` + readsb + ` --net-only --quiet --lat ` + fmtCoord(lat) + ` --lon ` + fmtCoord(lon) + ` \
  --net-ri-port 0 --net-ro-port 0 --net-sbs-port 0 --net-bi-port 0 --net-bo-port 0 \
  --net-connector 127.0.0.1,30005,beast_in \
  --net-connector feed1.adsbexchange.com,30004,beast_reduce_plus_out \
  --net-beast-reduce-interval 0.5 --uuid-file ` + adsbxUUIDFile + `
DynamicUser=yes
Restart=always
RestartSec=30

[Install]
WantedBy=multi-user.target
`
}

// download saves url to a new temporary file and returns its path.
func download(url, pattern string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("pobieranie %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("pobieranie %s: odpowiedź %d", url, resp.StatusCode)
	}
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20)); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// runLong runs a command with a time limit and returns the end of its output on failure.
func runLong(limit time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := strings.TrimSpace(string(out))
		if len(tail) > 1500 {
			tail = "…" + tail[len(tail)-1500:]
		}
		return fmt.Errorf("%s: %v: %s", filepath.Base(name), err, tail)
	}
	return nil
}

func installReadsb() error {
	script, err := download(readsbInstaller, "readsb-install-*.sh")
	if err != nil {
		return err
	}
	defer os.Remove(script)
	// The installer builds readsb from source (several minutes on a Pi) and adds tar1090.
	if err := runLong(30*time.Minute, "bash", script); err != nil {
		return err
	}
	moveLighttpd()
	return nil
}

// moveLighttpd moves tar1090's web server off port 80, which belongs to the panel.
func moveLighttpd() {
	const conf = "/etc/lighttpd/lighttpd.conf"
	b, err := os.ReadFile(conf)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "server.port") {
			lines[i] = fmt.Sprintf("server.port                 = %d", tar1090Port)
			found = true
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("server.port = %d", tar1090Port))
	}
	if os.WriteFile(conf, []byte(strings.Join(lines, "\n")), 0o644) == nil {
		_ = run("systemctl", "restart", "lighttpd")
	}
}

func installFR24() error {
	arch := map[string]string{"arm64": "arm64", "arm": "armhf"}[runtime.GOARCH]
	if arch == "" {
		return errors.New("Flightradar24 udostępnia fr24feed tylko dla Raspberry Pi (ARM)")
	}
	deb, err := download(fmt.Sprintf(fr24DebURL, fr24Version, arch), "fr24feed-*.deb")
	if err != nil {
		return err
	}
	defer os.Remove(deb)
	named := deb + ".deb"
	if err := os.Rename(deb, named); err != nil {
		return err
	}
	defer os.Remove(named)
	if err := runLong(15*time.Minute, "apt-get", "install", "-y", named); err != nil {
		return err
	}
	// Without a sharing key fr24feed would try to open the SDR itself; it starts once configured.
	if _, err := os.Stat(fr24Ini); err != nil || !strings.Contains(readFile(fr24Ini), `receiver="beast-tcp"`) {
		_ = run("systemctl", "disable", "--now", "fr24feed")
	}
	return nil
}

func readFile(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}

// ---------- status (no root needed) ----------

// SDR is a USB radio the decoder can use.
type SDR struct {
	Name string `json:"name"`
	USB  string `json:"usb"`
}

var sdrIDs = map[string]string{
	"0bda:2832": "RTL-SDR (RTL2832U)",
	"0bda:2838": "RTL-SDR (RTL2832U)",
	"1d50:60a1": "Airspy",
}

// FindSDRs lists the SDR receivers plugged into USB.
func FindSDRs() []SDR {
	var out []SDR
	dirs, _ := filepath.Glob("/sys/bus/usb/devices/*")
	for _, d := range dirs {
		v := strings.TrimSpace(readFile(filepath.Join(d, "idVendor")))
		p := strings.TrimSpace(readFile(filepath.Join(d, "idProduct")))
		name, ok := sdrIDs[v+":"+p]
		if !ok {
			continue
		}
		if prod := strings.TrimSpace(readFile(filepath.Join(d, "product"))); prod != "" {
			name = prod
		}
		out = append(out, SDR{Name: name, USB: v + ":" + p})
	}
	return out
}

// ServiceActive reports whether a systemd service runs.
func ServiceActive(name string) bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return exec.Command("systemctl", "is-active", "--quiet", name).Run() == nil
}

// FileExists reports whether path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Tar1090Port is where tar1090's map listens after moveLighttpd.
func Tar1090Port() int { return tar1090Port }
