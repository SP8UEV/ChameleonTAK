package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/cot"
	"chameleontak/internal/store"
)

// The rest of the Data Sync minimal set (docs/research/cot-marti-missions.md §4.11): mission
// password, subscriber roles, access token, expiration, contacts, the /missions/guid/{guid}
// twins newer clients use, and the role-change and deletion notifications.

func (a *App) registerMissionExtraRoutes(m *http.ServeMux) {
	m.HandleFunc("PUT /Marti/api/missions/{name}/password", a.mSetPassword)
	m.HandleFunc("DELETE /Marti/api/missions/{name}/password", a.mRemovePassword)
	m.HandleFunc("PUT /Marti/api/missions/{name}/role", a.mSetRole)
	m.HandleFunc("GET /Marti/api/missions/{name}/token", a.mMissionToken)
	m.HandleFunc("PUT /Marti/api/missions/{name}/expiration", a.mSetExpiration)
	m.HandleFunc("GET /Marti/api/missions/{name}/contacts", a.mMissionContacts)
}

// missionByGUID rewrites /Marti/api/missions/guid/{guid}/... to /Marti/api/missions/{name}/...
// (a mux pattern for it would overlap every /missions/{name}/... route). ok is false when the
// mission does not exist.
func (a *App) missionByGUID(r *http.Request) (*http.Request, bool) {
	rest, found := strings.CutPrefix(r.URL.Path, "/Marti/api/missions/guid/")
	if !found {
		return r, true
	}
	guid, tail, _ := strings.Cut(rest, "/")
	mi, err := a.store.GetMissionByGUID(guid)
	if err != nil {
		return r, false
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/Marti/api/missions/" + mi.Name
	if tail != "" {
		r2.URL.Path += "/" + tail
	}
	r2.URL.RawPath = "/Marti/api/missions/" + url.PathEscape(mi.Name)
	if tail != "" {
		r2.URL.RawPath += "/" + tail
	}
	return r2, true
}

// ownerOnly loads the mission and checks that the caller owns it.
func (a *App) ownerOnly(w http.ResponseWriter, r *http.Request) (*store.Mission, bool) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return nil, false
	}
	if a.missionRole(callerOf(r), m) != roleOwner {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return m, true
}

func (a *App) mSetPassword(w http.ResponseWriter, r *http.Request) {
	m, ok := a.ownerOnly(w, r)
	if !ok {
		return
	}
	pw := r.URL.Query().Get("password")
	if pw == "" {
		http.Error(w, "password required", http.StatusBadRequest)
		return
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	m.PasswordHash = string(h)
	if err := a.store.UpdateMission(m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

func (a *App) mRemovePassword(w http.ResponseWriter, r *http.Request) {
	m, ok := a.ownerOnly(w, r)
	if !ok {
		return
	}
	m.PasswordHash = ""
	if err := a.store.UpdateMission(m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

// mSetRole changes a subscriber's role (owner only) and tells that client (t-x-m-r).
func (a *App) mSetRole(w http.ResponseWriter, r *http.Request) {
	m, ok := a.ownerOnly(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	uid := clientUIDParam(r)
	role := firstNonEmpty(q.Get("role"), roleSubscriber)
	if _, known := roleRank[role]; !known {
		http.Error(w, "unknown role", http.StatusBadRequest)
		return
	}
	if uid == "" {
		http.Error(w, "clientUid required", http.StatusBadRequest)
		return
	}
	if _, err := a.store.GetSubscription(m.Name, uid); err != nil {
		http.Error(w, "not subscribed", http.StatusNotFound)
		return
	}
	if err := a.store.SetSubscriptionRole(m.Name, uid, role); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now()
	raw := fmt.Sprintf(`<event version="2.0" uid="%s" type="t-x-m-r" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><mission type="ROLE_CHANGE" tool="%s" name="%s" guid="%s" authorUid="%s"><role type="%s"/></mission></detail></event>`,
		randomUUID(), cot.FormatTime(now), cot.FormatTime(now), cot.FormatTime(now.Add(20*time.Second)),
		xmlEscape(m.Tool), xmlEscape(m.Name), xmlEscape(m.GUID), xmlEscape(a.serverUID()), xmlEscape(role))
	a.router.SendToUIDs([]byte(raw), []string{uid})
	w.WriteHeader(http.StatusOK)
}

// mMissionToken checks the mission password and hands out an access token. Clients here are
// authenticated by their certificate, so the token only confirms the password.
func (a *App) mMissionToken(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	caller := callerOf(r)
	if !a.canSee(caller, m) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if m.PasswordHash != "" && a.missionRole(caller, m) == "" &&
		bcrypt.CompareHashAndPassword([]byte(m.PasswordHash), []byte(r.URL.Query().Get("password"))) != nil {
		http.Error(w, "invalid password", http.StatusForbidden)
		return
	}
	writeJSON(w, 201, map[string]any{"version": "3", "type": "java.lang.String", "data": randomToken(), "nodeId": a.cfg.Get().NodeID})
}

func (a *App) mSetExpiration(w http.ResponseWriter, r *http.Request) {
	m, ok := a.ownerOnly(w, r)
	if !ok {
		return
	}
	exp, err := strconv.ParseInt(firstNonEmpty(r.URL.Query().Get("expiration"), "-1"), 10, 64)
	if err != nil {
		http.Error(w, "invalid expiration", http.StatusBadRequest)
		return
	}
	m.Expiration = exp
	if err := a.store.UpdateMission(m); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

// mMissionContacts lists the subscribers that are connected right now, like /api/contacts/all.
func (a *App) mMissionContacts(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	if a.missionRole(callerOf(r), m) == "" && !a.canSee(callerOf(r), m) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	subs, _ := a.store.Subscriptions(m.Name)
	out := []map[string]any{}
	for _, s := range subs {
		c := a.router.ClientByUID(s.ClientUID)
		if c == nil {
			continue
		}
		info := c.Info()
		out = append(out, map[string]any{
			"uid": s.ClientUID, "callsign": info.Callsign, "takv": info.Platform, "notes": "", "filterGroups": info.Groups,
		})
	}
	writeJSON(w, 200, out)
}

// broadcastMissionDeleted tells everyone in the mission's groups that it is gone (t-x-m-d).
func (a *App) broadcastMissionDeleted(m *store.Mission) {
	now := time.Now()
	raw := fmt.Sprintf(`<event version="2.0" uid="%s" type="t-x-m-d" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><mission type="DELETE" tool="%s" name="%s" guid="%s" authorUid="%s"/></detail></event>`,
		randomUUID(), cot.FormatTime(now), cot.FormatTime(now), cot.FormatTime(now.Add(20*time.Second)),
		xmlEscape(m.Tool), xmlEscape(m.Name), xmlEscape(m.GUID), xmlEscape(a.serverUID()))
	var groups []string
	if len(m.Groups) > 0 {
		groups = m.Groups
	}
	a.router.Broadcast([]byte(raw), groups)
}
