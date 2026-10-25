package system

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrUnsupported = errors.New("operacja systemowa niedostępna na tym hoście")

const (
	FirewallFile    = "/etc/chameleontak/firewall.nft"
	CaptiveConf     = "/etc/NetworkManager/dnsmasq-shared.d/chameleontak-captive.conf"
	nmDir           = "/etc/NetworkManager/system-connections"
	WifiConn        = "chameleontak-wifi"
	HotspotConn     = "chameleontak-hotspot"
	SetupHotspot    = "chameleontak-setup"
	HotspotGateway  = "10.42.0.1"
	systemLoginUser = "tak"
)

var (
	reHostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)
	reIface    = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,15}$`)
)

// RunSys executes a privileged action. It is the entry point of "chameleontak sys" and must
// validate every argument because it runs as root through sudo.
func RunSys(args []string, stdin io.Reader) error {
	if runtime.GOOS != "linux" {
		return ErrUnsupported
	}
	if len(args) == 0 {
		return errors.New("brak akcji")
	}
	action, rest := args[0], args[1:]
	need := func(n int) error {
		if len(rest) != n {
			return fmt.Errorf("%s: oczekiwano %d argumentów", action, n)
		}
		return nil
	}
	secret := func() (string, error) {
		b, err := io.ReadAll(io.LimitReader(stdin, 256))
		return strings.TrimRight(string(b), "\r\n"), err
	}

	switch action {
	case "set-time":
		if err := need(1); err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, rest[0])
		if err != nil {
			return err
		}
		if err := run("date", "-u", "-s", "@"+strconv.FormatInt(t.Unix(), 10)); err != nil {
			return err
		}
		_ = run("fake-hwclock", "save")
		return nil

	case "set-hostname":
		if err := need(1); err != nil {
			return err
		}
		if !reHostname.MatchString(rest[0]) {
			return errors.New("niepoprawna nazwa hosta")
		}
		if err := run("hostnamectl", "set-hostname", rest[0]); err != nil {
			return err
		}
		return setHostsEntry(rest[0])

	case "firewall":
		if err := need(1); err != nil {
			return err
		}
		ports, err := parsePorts(rest[0])
		if err != nil {
			return err
		}
		// install.sh --no-firewall (or an active ufw/firewalld) hands the firewall to the admin.
		if _, err := os.Stat("/etc/chameleontak/firewall.disabled"); err == nil {
			return nil
		}
		if err := os.MkdirAll("/etc/chameleontak", 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(FirewallFile, []byte(FirewallRules(ports)), 0o644); err != nil {
			return err
		}
		return run("nft", "-f", FirewallFile)

	case "wifi-client":
		if err := need(1); err != nil {
			return err
		}
		pw, err := secret()
		if err != nil {
			return err
		}
		if err := validSSID(rest[0]); err != nil {
			return err
		}
		if pw != "" {
			if err := validPSK(pw); err != nil {
				return err
			}
		}
		if err := writeConn(WifiConn, wifiKeyfile(rest[0], pw)); err != nil {
			return err
		}
		_ = run("nmcli", "radio", "wifi", "on")
		_ = run("nmcli", "connection", "reload")
		return run("nmcli", "connection", "up", WifiConn)

	case "hotspot":
		if err := need(1); err != nil {
			return err
		}
		pw, err := secret()
		if err != nil {
			return err
		}
		if err := validSSID(rest[0]); err != nil {
			return err
		}
		if err := validPSK(pw); err != nil {
			return err
		}
		_ = os.Remove(CaptiveConf)
		_ = run("nmcli", "connection", "delete", SetupHotspot)
		if err := writeConn(HotspotConn, hotspotKeyfile(rest[0], pw)); err != nil {
			return err
		}
		_ = run("rfkill", "unblock", "wifi")
		_ = run("nmcli", "radio", "wifi", "on")
		_ = run("nmcli", "connection", "reload")
		return run("nmcli", "connection", "up", HotspotConn)

	case "hotspot-off":
		_ = os.Remove(CaptiveConf)
		_ = run("nmcli", "connection", "delete", SetupHotspot)
		_ = run("nmcli", "connection", "delete", HotspotConn)
		_ = os.Remove(nmDir + "/" + HotspotConn + ".nmconnection")
		return nil

	case "setup-hotspot-off":
		_ = os.Remove(CaptiveConf)
		_ = run("nmcli", "connection", "delete", SetupHotspot)
		return nil

	case "static-ip":
		if err := need(4); err != nil {
			return err
		}
		iface, cidr, gw, dns := rest[0], rest[1], rest[2], rest[3]
		if !reIface.MatchString(iface) {
			return errors.New("niepoprawny interfejs")
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return err
		}
		for _, ip := range []string{gw, dns} {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("niepoprawny adres %q", ip)
			}
		}
		out, err := exec.Command("nmcli", "-g", "GENERAL.CONNECTION", "device", "show", iface).Output()
		if err != nil {
			return err
		}
		conn := strings.TrimSpace(string(out))
		if conn == "" {
			return errors.New("interfejs nie ma aktywnego połączenia")
		}
		if err := run("nmcli", "connection", "modify", conn, "ipv4.method", "manual", "ipv4.addresses", cidr, "ipv4.gateway", gw, "ipv4.dns", dns); err != nil {
			return err
		}
		return run("nmcli", "connection", "up", conn)

	case "ssh":
		if err := need(1); err != nil {
			return err
		}
		switch rest[0] {
		case "enable":
			return run("systemctl", "enable", "--now", "ssh")
		case "disable":
			return run("systemctl", "disable", "--now", "ssh")
		}
		return errors.New("ssh: enable|disable")

	case "set-password":
		if err := need(1); err != nil {
			return err
		}
		if rest[0] != systemLoginUser {
			return errors.New("można zmienić hasło tylko konta tak")
		}
		pw, err := secret()
		if err != nil {
			return err
		}
		if len(pw) < 8 || strings.ContainsAny(pw, ":\n") {
			return errors.New("niepoprawne hasło")
		}
		cmd := exec.Command("chpasswd")
		cmd.Stdin = strings.NewReader(rest[0] + ":" + pw + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("chpasswd: %v: %s", err, out)
		}
		return nil

	case "reboot":
		return run("systemctl", "reboot")
	}
	return fmt.Errorf("nieznana akcja %q", action)
}

// FirewallRules renders the nftables table that replaces itself atomically.
func FirewallRules(tcpPorts []int) string {
	sort.Ints(tcpPorts)
	ps := make([]string, 0, len(tcpPorts))
	for _, p := range tcpPorts {
		ps = append(ps, strconv.Itoa(p))
	}
	return `#!/usr/sbin/nft -f
# Wygenerowane przez ChameleonTAK — nie edytuj ręcznie
table inet chameleontak
delete table inet chameleontak
table inet chameleontak {
  chain input {
    type filter hook input priority filter; policy drop;
    ct state established,related accept
    ct state invalid drop
    iif "lo" accept
    meta l4proto { icmp, ipv6-icmp } accept
    udp dport { 53, 67, 68, 5353 } accept
    tcp dport { 53 } accept
    tcp dport { ` + strings.Join(ps, ", ") + ` } accept
  }
}
`
}

func parsePorts(csv string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, s := range strings.Split(csv, ",") {
		p, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("niepoprawny port %q", s)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("brak portów")
	}
	return out, nil
}

func validSSID(s string) error {
	if len(s) < 1 || len(s) > 32 {
		return errors.New("SSID musi mieć 1–32 znaki")
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == ';' || r == '\\' || r == '[' || r == ']' || r == '=' {
			return errors.New("SSID zawiera niedozwolone znaki")
		}
	}
	return nil
}

func validPSK(pw string) error {
	if len(pw) < 8 || len(pw) > 63 {
		return errors.New("hasło Wi-Fi musi mieć 8–63 znaki")
	}
	for _, r := range pw {
		if r < 0x20 || r == 0x7f {
			return errors.New("hasło Wi-Fi zawiera niedozwolone znaki")
		}
	}
	return nil
}

func wifiKeyfile(ssid, pw string) string {
	sec := ""
	if pw != "" {
		sec = "\n[wifi-security]\nkey-mgmt=wpa-psk\npsk=" + pw + "\n"
	}
	return "[connection]\nid=" + WifiConn + "\ntype=wifi\nautoconnect=true\nautoconnect-priority=10\n\n[wifi]\nmode=infrastructure\nssid=" + ssid + "\n" +
		sec + "\n[ipv4]\nmethod=auto\n\n[ipv6]\nmethod=auto\n"
}

func hotspotKeyfile(ssid, pw string) string {
	return "[connection]\nid=" + HotspotConn + "\ntype=wifi\ninterface-name=wlan0\nautoconnect=true\nautoconnect-priority=100\n\n" +
		"[wifi]\nmode=ap\nssid=" + ssid + "\nband=bg\n\n" +
		"[wifi-security]\nkey-mgmt=wpa-psk\npsk=" + pw + "\nproto=rsn\npairwise=ccmp\ngroup=ccmp\n\n" +
		"[ipv4]\nmethod=shared\naddress1=" + HotspotGateway + "/24\n\n[ipv6]\nmethod=ignore\n"
}

func writeConn(name, content string) error {
	if err := os.MkdirAll(nmDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(nmDir+"/"+name+".nmconnection", []byte(content), 0o600)
}

// setHostsEntry keeps the 127.0.1.1 line in /etc/hosts in sync with the hostname (sudo warns otherwise).
func setHostsEntry(host string) error {
	raw, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "127.0.1.1") {
			lines[i] = "127.0.1.1\t" + host
			found = true
		}
	}
	if !found {
		lines = append(lines, "127.0.1.1\t"+host)
	}
	return os.WriteFile("/etc/hosts", []byte(strings.Join(lines, "\n")), 0o644)
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
