package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"chameleontak/internal/config"
	"chameleontak/internal/system"
)

// ZeroTier access (system/zerotier.go): the Pi joins a ZeroTier network and the server uses its
// ZeroTier address, so phones in the same network reach it from anywhere without a public IP,
// a domain or port forwarding. The network belongs to the device, so the main server manages it.

type ztNetwork struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Status   string   `json:"status"` // OK | REQUESTING_CONFIGURATION | ACCESS_DENIED | NOT_FOUND | ...
	Addrs    []string `json:"addrs"`  // IPv4 addresses without the prefix length
	PortName string   `json:"device"`
}

type ztStatus struct {
	Supported bool        `json:"supported"`
	Installed bool        `json:"installed"`
	Running   bool        `json:"running"`
	Online    bool        `json:"online"`
	NodeID    string      `json:"nodeId"`
	Version   string      `json:"version"`
	Network   string      `json:"network"` // configured network ID
	Networks  []ztNetwork `json:"networks"`
	Error     string      `json:"error"`
}

func (a *App) zerotierStatus() ztStatus {
	h := a.host()
	st := ztStatus{Supported: h.helper.Available(), Installed: system.ZeroTierInstalled(), Network: h.cfg.Get().ZeroTier.Network, Networks: []ztNetwork{}}
	if !st.Supported || !st.Installed {
		return st
	}
	out, err := h.helper.Output("zerotier-status")
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Running = true
	var raw struct {
		Info struct {
			Address string `json:"address"`
			Online  bool   `json:"online"`
			Version string `json:"version"`
		} `json:"info"`
		Networks []struct {
			ID       string   `json:"id"`
			NWID     string   `json:"nwid"`
			Name     string   `json:"name"`
			Status   string   `json:"status"`
			Assigned []string `json:"assignedAddresses"`
			Port     string   `json:"portDeviceName"`
		} `json:"networks"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		st.Error = "nieczytelny stan ZeroTier"
		return st
	}
	st.NodeID, st.Online, st.Version = raw.Info.Address, raw.Info.Online, raw.Info.Version
	for _, n := range raw.Networks {
		id := n.ID
		if id == "" {
			id = n.NWID
		}
		net := ztNetwork{ID: id, Name: n.Name, Status: n.Status, PortName: n.Port, Addrs: []string{}}
		for _, cidr := range n.Assigned {
			ip, _, _ := strings.Cut(cidr, "/")
			if !strings.Contains(ip, ":") {
				net.Addrs = append(net.Addrs, ip)
			}
		}
		st.Networks = append(st.Networks, net)
	}
	return st
}

// zerotierNetworkFor is the ZeroTier network phones must join to reach the server at its
// configured address ("" when the address is not a ZeroTier one).
func (a *App) zerotierNetworkFor() string {
	if slices.Contains(system.ZeroTierIPs(), a.cfg.Get().ServerName) {
		return a.host().cfg.Get().ZeroTier.Network
	}
	return ""
}

// ---------- API ----------

func (a *App) hZeroTier(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.zerotierStatus())
}

func (a *App) hZeroTierInstall(w http.ResponseWriter, r *http.Request) {
	if !a.helper.Available() {
		fail(w, invalid("Instalacja ZeroTier jest możliwa tylko na Raspberry Pi z ChameleonTAK."))
		return
	}
	a.log.Info("ZeroTier: instalacja")
	if err := a.helper.Run("", "zerotier-install"); err != nil {
		a.log.Error("ZeroTier: instalacja nie powiodła się", "err", err)
		fail(w, err)
		return
	}
	a.log.Info("ZeroTier: zainstalowano")
	if err := a.applyFirewall(); err != nil && err != system.ErrUnsupported {
		a.log.Warn("zapora", "err", err)
	}
	writeJSON(w, 200, a.zerotierStatus())
}

func (a *App) hZeroTierJoin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Network string `json:"network"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	id := strings.ToLower(strings.TrimSpace(req.Network))
	if !system.ValidNetworkID(id) {
		fail(w, invalid("Identyfikator sieci ZeroTier ma 16 znaków (cyfry i litery a–f), np. 8056c2e21c000001."))
		return
	}
	old := a.cfg.Get().ZeroTier.Network
	if old != "" && old != id {
		_ = a.helper.Run("", "zerotier-leave", old)
	}
	if err := a.helper.Run("", "zerotier-join", id); err != nil {
		fail(w, err)
		return
	}
	if _, err := a.cfg.Update(func(c *config.Config) error { c.ZeroTier.Network = id; return nil }); err != nil {
		fail(w, err)
		return
	}
	a.log.Info("ZeroTier: dołączono do sieci", "sieć", id)
	writeJSON(w, 200, a.zerotierStatus())
}

func (a *App) hZeroTierLeave(w http.ResponseWriter, r *http.Request) {
	id := a.cfg.Get().ZeroTier.Network
	if id == "" {
		fail(w, invalid("Serwer nie jest w żadnej sieci ZeroTier."))
		return
	}
	for _, s := range a.servers() {
		if s.zerotierNetworkFor() != "" {
			fail(w, invalid("Serwer „"+s.cfg.Get().DisplayName+"” używa adresu ZeroTier. Najpierw zmień jego adres w ustawieniach."))
			return
		}
	}
	if err := a.helper.Run("", "zerotier-leave", id); err != nil {
		fail(w, err)
		return
	}
	if _, err := a.cfg.Update(func(c *config.Config) error { c.ZeroTier.Network = ""; return nil }); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, a.zerotierStatus())
}
