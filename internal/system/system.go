// Package system integrates with the host OS (Raspberry Pi OS / Debian): status information and
// the privileged "sys" helper that is run through sudo.
package system

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Addr struct {
	Iface string `json:"iface"`
	IP    string `json:"ip"`
}

type Info struct {
	Hostname  string    `json:"hostname"`
	IsPi      bool      `json:"isPi"`
	Model     string    `json:"model"`
	CPUTempC  float64   `json:"cpuTempC"`
	Load1     float64   `json:"load1"`
	MemTotal  uint64    `json:"memTotal"`
	MemUsed   uint64    `json:"memUsed"`
	DiskTotal uint64    `json:"diskTotal"`
	DiskUsed  uint64    `json:"diskUsed"`
	Addresses []Addr    `json:"addresses"`
	Time      time.Time `json:"time"`
	NTPSynced bool      `json:"ntpSynced"`
	OS        string    `json:"os"`
}

func Collect(dataDir string) Info {
	host, _ := os.Hostname()
	info := Info{
		Hostname:  host,
		Model:     Model(),
		Addresses: Addresses(),
		Time:      time.Now().UTC(),
		NTPSynced: NTPSynced(),
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
	}
	info.IsPi = strings.Contains(info.Model, "Raspberry Pi")
	if b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil {
			info.CPUTempC = v / 1000
		}
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			info.Load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	info.MemTotal, info.MemUsed = memInfo()
	info.DiskTotal, info.DiskUsed = diskUsage(dataDir)
	return info
}

func Model() string {
	if b, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		return strings.TrimRight(string(b), "\x00\n ")
	}
	return runtime.GOOS
}

func IsPi() bool { return strings.Contains(Model(), "Raspberry Pi") }

// WifiAvailable reports whether Wi-Fi can be managed: a wlan0 interface and NetworkManager.
func WifiAvailable() bool {
	if _, err := os.Stat("/sys/class/net/wlan0"); err != nil {
		return false
	}
	_, err := exec.LookPath("nmcli")
	return err == nil
}

// Addresses lists non-loopback IPv4 addresses, wired interfaces first.
func Addresses() []Addr {
	var out []Addr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.To4() == nil || ipn.IP.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, Addr{Iface: ifc.Name, IP: ipn.IP.String()})
		}
	}
	rank := func(a Addr) int {
		n := strings.ToLower(a.Iface)
		switch {
		case strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en") || strings.HasPrefix(n, "ethernet"):
			return 0
		case strings.HasPrefix(n, "wl") || strings.HasPrefix(n, "wi"):
			return 1
		}
		return 2
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// CurrentIPv4 finds the interface holding ip and returns it with its CIDR and default gateway.
func CurrentIPv4(ip string) (iface, cidr, gateway string, ok bool) {
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, isNet := a.(*net.IPNet)
			if !isNet || ipn.IP.String() != ip {
				continue
			}
			ones, _ := ipn.Mask.Size()
			gw := defaultGateway(ifc.Name)
			if gw == "" {
				return "", "", "", false
			}
			return ifc.Name, fmt.Sprintf("%s/%d", ip, ones), gw, true
		}
	}
	return "", "", "", false
}

// defaultGateway reads the IPv4 default route of an interface from /proc/net/route.
func defaultGateway(iface string) string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 || fields[0] != iface || fields[1] != "00000000" {
			continue
		}
		v, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
	}
	return ""
}

func NTPSynced() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	out, err := exec.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "yes"
}

func memInfo() (total, used uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 {
			v, _ := strconv.ParseUint(fields[1], 10, 64)
			vals[strings.TrimSuffix(fields[0], ":")] = v * 1024
		}
	}
	total = vals["MemTotal"]
	if avail, ok := vals["MemAvailable"]; ok && total >= avail {
		used = total - avail
	}
	return
}

// Helper invokes the privileged helper ("chameleontak sys …") through sudo.
type Helper struct {
	Bin string
}

const HelperPath = "/usr/local/bin/chameleontak"

func NewHelper() *Helper { return &Helper{Bin: HelperPath} }

// Available reports whether privileged operations can run on this host.
func (h *Helper) Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, err := os.Stat(h.Bin)
	return err == nil
}

// Run executes an action; secret values are passed on stdin.
func (h *Helper) Run(stdin string, action string, args ...string) error {
	if !h.Available() {
		return ErrUnsupported
	}
	sysArgs := append([]string{"sys", action}, args...)
	var cmd *exec.Cmd
	if os.Geteuid() == 0 {
		cmd = exec.Command(h.Bin, sysArgs...)
	} else {
		cmd = exec.Command("sudo", append([]string{"-n", h.Bin}, sysArgs...)...)
	}
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v: %s", action, err, strings.TrimSpace(out.String()))
	}
	return nil
}
