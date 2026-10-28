package system

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ZeroTier: a virtual network for servers without a public address or domain. The Pi and the
// phones join the same ZeroTier network and reach each other by its private addresses, through
// any NAT or mobile network, without port forwarding.

const zerotierInstaller = "https://install.zerotier.com"

var reNetworkID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidNetworkID reports whether s is a 16-digit hexadecimal ZeroTier network ID.
func ValidNetworkID(s string) bool { return reNetworkID.MatchString(s) }

// ZeroTierInstalled reports whether zerotier-one is installed.
func ZeroTierInstalled() bool {
	_, err := exec.LookPath("zerotier-cli")
	return err == nil || FileExists("/usr/sbin/zerotier-cli")
}

// ZeroTierIPs are the IPv4 addresses of the ZeroTier interfaces (zt*).
func ZeroTierIPs() []string {
	out := []string{}
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if !strings.HasPrefix(ifc.Name, "zt") || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

func runZeroTier(action string, rest []string) error {
	switch action {
	case "zerotier-install":
		script, err := download(zerotierInstaller, "zerotier-install-*.sh")
		if err != nil {
			return err
		}
		defer os.Remove(script)
		// The script adds ZeroTier's signed apt repository and installs zerotier-one.
		if err := runLong(15*time.Minute, "bash", script); err != nil {
			return err
		}
		return run("systemctl", "enable", "--now", "zerotier-one")
	case "zerotier-join", "zerotier-leave":
		if len(rest) != 1 || !ValidNetworkID(rest[0]) {
			return errors.New("identyfikator sieci ZeroTier to 16 znaków 0–9 i a–f")
		}
		return run("zerotier-cli", strings.TrimPrefix(action, "zerotier-"), rest[0])
	case "zerotier-status":
		// The CLI needs root (it reads authtoken.secret); the JSON goes to the service on stdout.
		info, err := exec.Command("zerotier-cli", "-j", "info").Output()
		if err != nil {
			return fmt.Errorf("zerotier-cli info: %w", err)
		}
		nets, err := exec.Command("zerotier-cli", "-j", "listnetworks").Output()
		if err != nil {
			return fmt.Errorf("zerotier-cli listnetworks: %w", err)
		}
		_, err = fmt.Fprintf(os.Stdout, `{"info":%s,"networks":%s}`, bytes.TrimSpace(info), bytes.TrimSpace(nets))
		return err
	}
	return fmt.Errorf("nieznana akcja %q", action)
}
