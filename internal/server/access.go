package server

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/config"
	"chameleontak/internal/system"
)

// Panel access list: with Restrict on, only listed IP addresses, networks and MAC addresses
// open the admin panel. Phones keep reaching the join pages (phonePath) from anywhere, and the
// Raspberry Pi itself (loopback) is always allowed. MAC addresses are known only for devices in
// the same local network; from the internet only IP entries can match.

// normalizeAccessEntry checks one list entry and returns it in canonical form.
func normalizeAccessEntry(s string) (string, error) {
	s = strings.TrimSpace(s)
	if mac := system.NormalizeMAC(s); mac != "" {
		return mac, nil
	}
	if ip := net.ParseIP(s); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String(), nil
		}
		return ip.String(), nil
	}
	if _, n, err := net.ParseCIDR(s); err == nil {
		return n.String(), nil
	}
	return "", invalid(fmt.Sprintf("„%s” nie jest adresem IP, siecią (np. 192.168.1.0/24) ani adresem MAC.", s))
}

func normalizeAccessList(list []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range list {
		if strings.TrimSpace(e) == "" {
			continue
		}
		n, err := normalizeAccessEntry(e)
		if err != nil {
			return nil, err
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// accessAllowed reports whether a client at ip may use the panel under pa.
func accessAllowed(pa config.PanelAccess, ip string, macOf func(string) string) bool {
	if !pa.Restrict {
		return true
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	if v4 := addr.To4(); v4 != nil {
		addr = v4
	}
	if addr.IsLoopback() {
		return true
	}
	mac := ""
	for _, e := range pa.Allow {
		if ip2 := net.ParseIP(e); ip2 != nil {
			if ip2.Equal(addr) {
				return true
			}
			continue
		}
		if _, n, err := net.ParseCIDR(e); err == nil {
			if n.Contains(addr) {
				return true
			}
			continue
		}
		if strings.Count(e, ":") == 5 {
			if mac == "" {
				mac = macOf(addr.String())
				if mac == "" {
					mac = "-"
				}
			}
			if mac == e {
				return true
			}
		}
	}
	return false
}

func (a *App) panelAllowed(r *http.Request) bool {
	return accessAllowed(a.cfg.Get().PanelAccess, clientIP(r), system.NeighborMAC)
}

// denyPanel answers a request from an address outside the access list.
func denyPanel(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		httpError(w, http.StatusForbidden, "Panel jest niedostępny z tego adresu.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>ChameleonTAK</title>` +
		`<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 16px;line-height:1.5">` +
		`<h1 style="font-size:1.3rem">Panel niedostępny z tego adresu</h1>` +
		`<p>Administrator ograniczył dostęp do panelu ChameleonTAK do wybranych urządzeń. Twój adres: <code>` + htmlEscape(clientIP(r)) + `</code>.</p>` +
		`<p style="color:#666">Jeśli to Twój serwer i straciłeś dostęp, wyłącz ograniczenie w konsoli Raspberry Pi: <code>sudo -u chameleontak chameleontak panel-access off</code>, a potem <code>sudo systemctl restart chameleontak</code>.</p>`))
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// DisablePanelAccess turns the access list off (console recovery when the admin locked
// themselves out).
func (a *App) DisablePanelAccess() error {
	_, err := a.cfg.Update(func(c *config.Config) error { c.PanelAccess.Restrict = false; return nil })
	return err
}

// ---------- API ----------

// hClientAddress tells the panel the address (and, in the local network, the MAC) it is used
// from, so the access list can start with it.
func (a *App) hClientAddress(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
		ip = parsed.To4().String()
	}
	writeJSON(w, 200, map[string]string{"ip": ip, "mac": system.NeighborMAC(ip)})
}

func (a *App) hChangePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ip := clientIP(r)
	if a.limiter.blocked(ip) {
		httpError(w, http.StatusTooManyRequests, "Zbyt wiele nieudanych prób. Spróbuj za kilka minut.")
		return
	}
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		a.limiter.fail(ip)
		fail(w, invalid("Obecne hasło jest nieprawidłowe."))
		return
	}
	if len(req.New) < 8 {
		fail(w, invalid("Nowe hasło musi mieć co najmniej 8 znaków."))
		return
	}
	if req.New == req.Current {
		fail(w, invalid("Nowe hasło musi różnić się od obecnego."))
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		fail(w, err)
		return
	}
	u.PasswordHash = string(h)
	if err := a.store.UpdateUser(u); err != nil {
		fail(w, err)
		return
	}
	// Log out every other browser that knew the old password; this one gets a fresh session.
	if err := a.store.DeleteUserSessions(u.ID); err != nil {
		fail(w, err)
		return
	}
	a.startSession(w, r, u)
	a.log.Info("zmieniono hasło administratora", "user", u.Username, "ip", ip)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
