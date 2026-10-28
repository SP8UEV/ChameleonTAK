package system

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	macCacheMu sync.Mutex
	macCache   = map[string]macEntry{}
)

type macEntry struct {
	mac string
	at  time.Time
}

// NeighborMAC returns the MAC address of a device in the local network (from the kernel's
// neighbour table), or "" when the address is not on a directly attached network.
func NeighborMAC(ip string) string {
	addr := net.ParseIP(ip)
	if addr == nil {
		return ""
	}
	if v4 := addr.To4(); v4 != nil {
		ip = v4.String()
	}
	macCacheMu.Lock()
	if e, ok := macCache[ip]; ok && time.Since(e.at) < 30*time.Second {
		macCacheMu.Unlock()
		return e.mac
	}
	macCacheMu.Unlock()

	mac := ""
	if addr.To4() != nil {
		mac = arpLookup(ip)
	} else if out, err := exec.Command("ip", "-6", "neigh", "show", ip).Output(); err == nil {
		f := strings.Fields(string(out))
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "lladdr" {
				mac = NormalizeMAC(f[i+1])
			}
		}
	}
	macCacheMu.Lock()
	if len(macCache) > 1024 {
		macCache = map[string]macEntry{}
	}
	macCache[ip] = macEntry{mac, time.Now()}
	macCacheMu.Unlock()
	return mac
}

func arpLookup(ip string) string {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// IP address  HW type  Flags  HW address  Mask  Device
		cols := strings.Fields(sc.Text())
		if len(cols) >= 4 && cols[0] == ip && cols[2] != "0x0" {
			return NormalizeMAC(cols[3])
		}
	}
	return ""
}

// NormalizeMAC returns a MAC address as lower-case aa:bb:cc:dd:ee:ff, or "" if s is not one.
func NormalizeMAC(s string) string {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil || len(hw) != 6 || hw.String() == "00:00:00:00:00:00" {
		return ""
	}
	return hw.String()
}

// ListeningTCP lists the TCP ports something listens on for other machines (sockets bound to
// loopback are left out). It reads /proc and returns nil where that is not available.
func ListeningTCP() []int {
	seen := map[int]bool{}
	for _, file := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Scan() // header
		for sc.Scan() {
			// sl  local_address rem_address   st ...
			cols := strings.Fields(sc.Text())
			if len(cols) < 4 || cols[3] != "0A" { // 0A = LISTEN
				continue
			}
			host, port, ok := strings.Cut(cols[1], ":")
			if !ok || loopbackHex(host) {
				continue
			}
			if p, err := strconv.ParseUint(port, 16, 16); err == nil {
				seen[int(p)] = true
			}
		}
		f.Close()
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// loopbackHex reports whether a /proc/net address (little-endian 32-bit words) is a loopback one.
func loopbackHex(h string) bool {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return false
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	return net.IP(b).IsLoopback()
}

// FirewallState reads the ports the server's own firewall lets in: "managed" with the list,
// "disabled" when the administrator runs their own firewall, "unknown" when there is none.
func FirewallState() (string, []int) {
	if _, err := os.Stat("/etc/chameleontak/firewall.disabled"); err == nil {
		return "disabled", nil
	}
	b, err := os.ReadFile(FirewallFile)
	if err != nil {
		return "unknown", nil
	}
	var ports []int
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "tcp dport {") || !strings.HasSuffix(line, "} accept") {
			continue
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "tcp dport {"), "} accept")
		for _, f := range strings.Split(inner, ",") {
			if p, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
				ports = append(ports, p)
			}
		}
	}
	return "managed", ports
}
