package server

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"chameleontak/internal/cot"
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
)

const (
	roleOwner      = "MISSION_OWNER"
	roleSubscriber = "MISSION_SUBSCRIBER"
	roleReadOnly   = "MISSION_READONLY_SUBSCRIBER"
	timeFmt        = "2006-01-02T15:04:05.000Z"
)

var reMissionName = regexp.MustCompile(`^[^/\\<>"]{1,128}$`)

func martiTime(t time.Time) string { return t.UTC().Format(timeFmt) }

func rolePerms(role string) []string {
	switch role {
	case roleOwner:
		return []string{"MISSION_MANAGE_FEEDS", "MISSION_SET_PASSWORD", "MISSION_WRITE", "MISSION_MANAGE_LAYERS", "MISSION_UPDATE_GROUPS", "MISSION_READ", "MISSION_DELETE", "MISSION_SET_ROLE"}
	case roleReadOnly:
		return []string{"MISSION_READ"}
	}
	return []string{"MISSION_WRITE", "MISSION_READ"}
}

func roleJSON(role string) map[string]any {
	return map[string]any{"type": role, "permissions": rolePerms(role)}
}

// missionView renders a mission in the Marti v3 format.
func (a *App) missionView(m *store.Mission, withContents bool) map[string]any {
	keywords := m.Keywords
	if keywords == nil {
		keywords = []string{}
	}
	groups := m.Groups
	if len(groups) == 0 {
		groups = []string{stream.AnonGroup}
	}
	v := map[string]any{
		"name": m.Name, "description": m.Description, "chatRoom": m.ChatRoom, "baseLayer": m.BaseLayer, "bbox": m.BBox,
		"path": m.Path, "classification": m.Classification, "tool": m.Tool, "keywords": keywords, "creatorUid": m.CreatorUID,
		"createTime": martiTime(m.CreatedAt), "externalData": []any{}, "feeds": []any{}, "mapLayers": []any{},
		"defaultRole": roleJSON(m.DefaultRole), "inviteOnly": m.InviteOnly, "expiration": m.Expiration, "guid": m.GUID,
		"uids": []any{}, "contents": []any{}, "passwordProtected": m.PasswordHash != "", "groups": groups,
		"missionChanges": []any{}, "logs": []any{},
	}
	if !withContents {
		return v
	}
	files, _ := a.store.MissionFiles(m.Name)
	contents := []any{}
	for _, f := range files {
		contents = append(contents, map[string]any{
			"timestamp": martiTime(f.CreatedAt), "creatorUid": f.CreatorUID, "data": contentData(f.File),
		})
	}
	v["contents"] = contents
	items, _ := a.store.MissionItems(m.Name)
	uids := []any{}
	for _, it := range items {
		uids = append(uids, map[string]any{
			"data": it.UID, "timestamp": martiTime(it.CreatedAt), "creatorUid": it.CreatorUID,
			"details": map[string]any{"type": it.Type, "callsign": it.Callsign, "color": "", "location": map[string]float64{"lat": it.Lat, "lon": it.Lon}},
		})
	}
	v["uids"] = uids
	logs, _ := a.store.MissionLogs(m.Name)
	lv := []any{}
	for _, l := range logs {
		lv = append(lv, logJSON(m.Name, l))
	}
	v["logs"] = lv
	return v
}

func contentData(f *store.File) map[string]any {
	kw := f.Keywords
	if kw == nil {
		kw = []string{}
	}
	return map[string]any{
		"filename": f.Name, "keywords": kw, "mimeType": f.MIME, "name": f.Name, "submissionTime": martiTime(f.CreatedAt),
		"submitter": f.Submitter, "uid": f.UID, "creatorUid": f.CreatorUID, "hash": f.Hash, "size": f.Size, "expiration": -1,
	}
}

func logJSON(mission string, l store.MissionLog) map[string]any {
	kw := l.Keywords
	if kw == nil {
		kw = []string{}
	}
	return map[string]any{
		"id": l.ID, "content": l.Content, "creatorUid": l.CreatorUID, "entryUid": l.EntryUID, "missionNames": []string{mission},
		"servertime": martiTime(l.CreatedAt), "dtg": martiTime(l.DTG), "created": martiTime(l.CreatedAt), "keywords": kw,
		"contentHashes": []string{},
	}
}

// canSee reports whether the caller may access a mission (shared group, or admin).
func (a *App) canSee(c *martiCaller, m *store.Mission) bool {
	if c.User.IsAdmin || len(m.Groups) == 0 {
		return true
	}
	for _, g := range a.callerGroups(c) {
		for _, mg := range m.Groups {
			if strings.EqualFold(g, mg) {
				return true
			}
		}
	}
	return false
}

func (a *App) registerMissionRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /Marti/api/missions", a.mListMissions)
	m.HandleFunc("GET /Marti/api/missions/all/invitations", a.mInvitations)
	m.HandleFunc("GET /Marti/api/missions/invitations", a.mInvitations)
	m.HandleFunc("GET /Marti/api/missions/{name}", a.mGetMission)
	m.HandleFunc("PUT /Marti/api/missions/{name}", a.mCreateMission)
	m.HandleFunc("POST /Marti/api/missions/{name}", a.mCreateMission)
	m.HandleFunc("DELETE /Marti/api/missions/{name}", a.mDeleteMission)
	m.HandleFunc("GET /Marti/api/missions/{name}/cot", a.mMissionCoT)
	m.HandleFunc("GET /Marti/api/missions/{name}/changes", a.mMissionChanges)
	m.HandleFunc("GET /Marti/api/missions/{name}/log", a.mMissionLogs)
	m.HandleFunc("PUT /Marti/api/missions/{name}/subscription", a.mSubscribe)
	m.HandleFunc("POST /Marti/api/missions/{name}/subscription", a.mSubscribe)
	m.HandleFunc("DELETE /Marti/api/missions/{name}/subscription", a.mUnsubscribe)
	m.HandleFunc("GET /Marti/api/missions/{name}/subscription", a.mGetSubscription)
	m.HandleFunc("GET /Marti/api/missions/{name}/subscriptions", a.mSubscriptions)
	m.HandleFunc("GET /Marti/api/missions/{name}/subscriptions/roles", a.mSubscriptionRoles)
	m.HandleFunc("PUT /Marti/api/missions/{name}/contents", a.mAddContents)
	m.HandleFunc("DELETE /Marti/api/missions/{name}/contents", a.mRemoveContents)
	m.HandleFunc("PUT /Marti/api/missions/{name}/keywords", a.mSetKeywords)
	m.HandleFunc("PUT /Marti/api/missions/{name}/invite/{type}/{invitee}", a.mInvite)
	m.HandleFunc("DELETE /Marti/api/missions/{name}/invite/{type}/{invitee}", a.mUninvite)
	m.HandleFunc("POST /Marti/api/missions/logs/entries", a.mAddLog)
	m.HandleFunc("PUT /Marti/api/missions/logs/entries", a.mAddLog)
	m.HandleFunc("DELETE /Marti/api/missions/logs/entries/{id}", a.mDeleteLog)
	a.registerMissionExtraRoutes(m)
}

func (a *App) loadMission(w http.ResponseWriter, r *http.Request) (*store.Mission, bool) {
	m, err := a.store.GetMission(r.PathValue("name"))
	if err != nil {
		http.Error(w, "mission not found", http.StatusNotFound)
		return nil, false
	}
	if !a.canSee(callerOf(r), m) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return nil, false
	}
	return m, true
}

func (a *App) mListMissions(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	ms, err := a.store.ListMissions()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tool := r.URL.Query().Get("tool")
	out := []any{}
	for _, m := range ms {
		if !a.canSee(caller, m) || (tool != "" && m.Tool != tool) {
			continue
		}
		out = append(out, a.missionView(m, false))
	}
	a.martiJSON(w, 200, "Mission", out)
}

func (a *App) mGetMission(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, a.canReadContents(callerOf(r), m))})
}

// loadReadable is loadMission plus the password check for mission contents.
func (a *App) loadReadable(w http.ResponseWriter, r *http.Request) (*store.Mission, bool) {
	m, ok := a.loadMission(w, r)
	if ok && !a.canReadContents(callerOf(r), m) {
		http.Error(w, "subscription required", http.StatusForbidden)
		return nil, false
	}
	return m, ok
}

// loadWritable is loadMission plus the write permission check.
func (a *App) loadWritable(w http.ResponseWriter, r *http.Request) (*store.Mission, bool) {
	m, ok := a.loadMission(w, r)
	if ok && !a.canWrite(callerOf(r), m) {
		http.Error(w, "write permission required", http.StatusForbidden)
		return nil, false
	}
	return m, ok
}

// createMission is shared by the Marti API and the panel.
func (a *App) createMission(name, description, creatorUID, tool, password string, groups []string) (*store.Mission, error) {
	name = strings.TrimSpace(name)
	if !reMissionName.MatchString(name) {
		return nil, invalid("Niepoprawna nazwa misji.")
	}
	if _, err := a.store.GetMission(name); err == nil {
		return nil, invalid("Misja o tej nazwie już istnieje.")
	}
	if tool == "" {
		tool = "public"
	}
	m := &store.Mission{Name: name, GUID: randomUUID(), Description: description, CreatorUID: creatorUID, Tool: tool, Groups: cleanGroups(groups), Expiration: -1}
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		m.PasswordHash = string(h)
	}
	if err := a.store.CreateMission(m); err != nil {
		return nil, err
	}
	_ = a.store.AddMissionChange(m.Name, &store.MissionChange{Type: "CREATE_MISSION", CreatorUID: creatorUID})
	a.broadcastMissionCreated(m)
	return m, nil
}

func (a *App) mCreateMission(w http.ResponseWriter, r *http.Request) {
	caller := callerOf(r)
	q := r.URL.Query()
	name := r.PathValue("name")
	if existing, err := a.store.GetMission(name); err == nil {
		// Re-creating an existing mission just returns it (clients retry on reconnect).
		if !a.canSee(caller, existing) {
			http.Error(w, "forbidden", 403)
			return
		}
		a.martiJSON(w, 200, "Mission", []any{a.missionView(existing, a.canReadContents(caller, existing))})
		return
	}
	groups := a.allowedGroups(caller, q["group"])
	if len(groups) == 0 {
		groups = a.callerGroups(caller)
	}
	creator := q.Get("creatorUid")
	if creator != "" && !a.ownsUID(caller, creator) {
		http.Error(w, "creatorUid belongs to another user", 403)
		return
	}
	m, err := a.createMission(name, q.Get("description"), creator, q.Get("tool"), q.Get("password"), groups)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if kw := q["keywords"]; len(kw) > 0 {
		m.Keywords = kw
		_ = a.store.UpdateMission(m)
	}
	view := a.missionView(m, true)
	if creator != "" {
		sub := &store.MissionSub{ClientUID: creator, Username: caller.User.Username, Role: roleOwner, Token: randomToken()}
		_ = a.store.Subscribe(m.Name, sub)
		view["token"] = sub.Token
		view["ownerRole"] = roleJSON(roleOwner)
	}
	a.martiJSON(w, 201, "Mission", []any{view})
}

func (a *App) mDeleteMission(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	if a.missionRole(callerOf(r), m) != roleOwner {
		http.Error(w, "forbidden", 403)
		return
	}
	view := a.missionView(m, false)
	subs, _ := a.store.Subscriptions(m.Name)
	if err := a.store.DeleteMission(m.Name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.notifyMission(m, subs, "DELETE", nil)
	a.broadcastMissionDeleted(m)
	a.martiJSON(w, 200, "Mission", []any{view})
}

func (a *App) mMissionCoT(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadReadable(w, r)
	if !ok {
		return
	}
	items, _ := a.store.MissionItems(m.Name)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, `<?xml version='1.0' encoding='UTF-8' standalone='yes'?><events>`)
	for _, it := range items {
		_, _ = io.WriteString(w, it.CoT)
	}
	_, _ = io.WriteString(w, `</events>`)
}

func (a *App) changeJSON(m *store.Mission, c store.MissionChange) map[string]any {
	v := map[string]any{
		"type": c.Type, "missionName": m.Name, "timestamp": martiTime(c.CreatedAt), "creatorUid": c.CreatorUID, "serverTime": martiTime(c.CreatedAt),
	}
	if c.Hash != "" {
		if f, err := a.store.GetFile(c.Hash); err == nil {
			v["contentResource"] = contentData(f)
		} else {
			v["contentResource"] = map[string]any{"hash": c.Hash}
		}
	}
	if c.UID != "" {
		v["contentUid"] = c.UID
	}
	return v
}

func (a *App) mMissionChanges(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadReadable(w, r)
	if !ok {
		return
	}
	since := time.Time{}
	q := r.URL.Query()
	if s := q.Get("secago"); s != "" {
		var secs int64
		if _, err := fmt.Sscan(s, &secs); err == nil && secs > 0 {
			since = time.Now().Add(-time.Duration(secs) * time.Second)
		}
	}
	if s := q.Get("start"); s != "" {
		if t := cot.ParseTime(s); !t.IsZero() {
			since = t
		}
	}
	changes, _ := a.store.MissionChanges(m.Name, since)
	out := []any{}
	for _, c := range changes {
		out = append(out, a.changeJSON(m, c))
	}
	a.martiJSON(w, 200, "MissionChange", out)
}

func (a *App) mMissionLogs(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadReadable(w, r)
	if !ok {
		return
	}
	logs, _ := a.store.MissionLogs(m.Name)
	out := []any{}
	for _, l := range logs {
		out = append(out, logJSON(m.Name, l))
	}
	a.martiJSON(w, 200, "com.bbn.marti.sync.model.LogEntry", out)
}

func (a *App) subscriptionJSON(m *store.Mission, sub *store.MissionSub) map[string]any {
	return map[string]any{
		"token": sub.Token, "clientUid": sub.ClientUID, "username": sub.Username, "createTime": martiTime(sub.CreatedAt),
		"role": roleJSON(sub.Role), "mission": a.missionView(m, true),
	}
}

func (a *App) mSubscribe(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	caller := callerOf(r)
	q := r.URL.Query()
	uid := q.Get("uid")
	if uid == "" {
		uid = clientUIDParam(r)
	}
	if uid == "" {
		http.Error(w, "uid required", 400)
		return
	}
	if !a.ownsUID(caller, uid) {
		http.Error(w, "uid belongs to another user", 403)
		return
	}
	isCreator := m.CreatorUID != "" && a.ownsUID(caller, m.CreatorUID)
	existing := a.missionRole(caller, m)
	if m.PasswordHash != "" && existing == "" && !isCreator {
		if bcrypt.CompareHashAndPassword([]byte(m.PasswordHash), []byte(q.Get("password"))) != nil {
			http.Error(w, "invalid password", 403)
			return
		}
	}
	role := m.DefaultRole
	switch {
	case isCreator:
		role = roleOwner
	case existing != "" && existing != roleOwner:
		role = existing
	}
	if inv, ok := a.invitedRole(m.Name, uid); ok && roleRank[inv] > roleRank[role] {
		role = inv
	}
	sub := &store.MissionSub{ClientUID: uid, Username: caller.User.Username, Role: role, Token: randomToken()}
	if err := a.store.Subscribe(m.Name, sub); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if cur, err := a.store.GetSubscription(m.Name, uid); err == nil && roleRank[role] > roleRank[cur.Role] {
		_ = a.store.SetSubscriptionRole(m.Name, uid, role)
	}
	_ = a.store.Uninvite(m.Name, uid)
	saved, err := a.store.GetSubscription(m.Name, uid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.martiJSON(w, 201, "com.bbn.marti.sync.model.MissionSubscription", a.subscriptionJSON(m, saved))
}

func (a *App) mGetSubscription(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	uid := r.URL.Query().Get("uid")
	if !a.ownsUID(callerOf(r), uid) {
		http.Error(w, "forbidden", 403)
		return
	}
	sub, err := a.store.GetSubscription(m.Name, uid)
	if err != nil {
		http.Error(w, "not subscribed", 404)
		return
	}
	a.martiJSON(w, 200, "com.bbn.marti.sync.model.MissionSubscription", a.subscriptionJSON(m, sub))
}

func (a *App) mUnsubscribe(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	uid := firstNonEmpty(q.Get("uid"), clientUIDParam(r))
	if !a.ownsUID(callerOf(r), uid) {
		http.Error(w, "forbidden", 403)
		return
	}
	// TAK defaults to disconnectOnly=true: an owner keeps the role when the device just leaves.
	if sub, err := a.store.GetSubscription(m.Name, uid); err == nil && sub.Role == roleOwner && q.Get("disconnectOnly") != "false" {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = a.store.Unsubscribe(m.Name, uid)
	w.WriteHeader(http.StatusOK)
}

func (a *App) mSubscriptions(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	subs, _ := a.store.Subscriptions(m.Name)
	out := []string{}
	for _, s := range subs {
		out = append(out, s.ClientUID)
	}
	a.martiJSON(w, 200, "MissionSubscription", out)
}

func (a *App) mSubscriptionRoles(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadMission(w, r)
	if !ok {
		return
	}
	subs, _ := a.store.Subscriptions(m.Name)
	out := []any{}
	for _, s := range subs {
		out = append(out, map[string]any{"clientUid": s.ClientUID, "username": s.Username, "createTime": martiTime(s.CreatedAt), "role": roleJSON(s.Role)})
	}
	a.martiJSON(w, 200, "MissionSubscription", out)
}

func (a *App) mAddContents(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadWritable(w, r)
	if !ok {
		return
	}
	var in struct {
		Hashes []string `json:"hashes"`
		UIDs   []string `json:"uids"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := remarshalBytes(body, &in); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	creator := r.URL.Query().Get("creatorUid")
	for _, h := range in.Hashes {
		h = strings.ToLower(h)
		if _, err := a.store.GetFile(h); err != nil {
			continue
		}
		if added, _ := a.store.AddMissionFile(m.Name, h, creator); added {
			c := &store.MissionChange{Type: "ADD_CONTENT", Hash: h, CreatorUID: creator}
			_ = a.store.AddMissionChange(m.Name, c)
			a.notifyMissionChange(m, *c)
		}
	}
	callerGroups := a.callerGroups(callerOf(r))
	for _, uid := range in.UIDs {
		// UIDs reference CoT already seen on the network; the latest SA/marker from the map is used,
		// but only if its sender is visible to the caller (no copying across groups).
		if p := a.pos.Get(uid); p != nil && (callerOf(r).User.IsAdmin || sharesGroup(p.groups, callerGroups)) {
			if raw := a.pos.Raw(uid); raw != nil {
				a.addMissionItem(m, uid, p.Type, p.Callsign, p.Lat, p.Lon, string(raw), creator)
			}
		}
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

func (a *App) mRemoveContents(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadWritable(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	creator := q.Get("creatorUid")
	if h := strings.ToLower(q.Get("hash")); h != "" {
		if removed, _ := a.store.RemoveMissionFile(m.Name, h); removed {
			c := &store.MissionChange{Type: "REMOVE_CONTENT", Hash: h, CreatorUID: creator}
			_ = a.store.AddMissionChange(m.Name, c)
			a.notifyMissionChange(m, *c)
		}
	}
	if uid := q.Get("uid"); uid != "" {
		if removed, _ := a.store.RemoveMissionItem(m.Name, uid); removed {
			c := &store.MissionChange{Type: "REMOVE_CONTENT", UID: uid, CreatorUID: creator}
			_ = a.store.AddMissionChange(m.Name, c)
			a.notifyMissionChange(m, *c)
		}
	}
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

func (a *App) mSetKeywords(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadWritable(w, r)
	if !ok {
		return
	}
	var kw []string
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err := remarshalBytes(body, &kw); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	m.Keywords = kw
	_ = a.store.UpdateMission(m)
	a.martiJSON(w, 200, "Mission", []any{a.missionView(m, true)})
}

func (a *App) mInvitations(w http.ResponseWriter, r *http.Request) {
	uid := clientUIDParam(r)
	if uid == "" {
		uid = r.URL.Query().Get("uid")
	}
	names, _ := a.store.Invitations(uid)
	if names == nil {
		names = []string{}
	}
	a.martiJSON(w, 200, "MissionInvitation", names)
}

func (a *App) mInvite(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadWritable(w, r)
	if !ok {
		return
	}
	invitee := r.PathValue("invitee")
	if r.PathValue("type") == "callsign" {
		if c := a.router.ClientByCallsign(invitee); c != nil {
			invitee = c.UID()
		}
	}
	role := r.URL.Query().Get("role")
	if role == "" {
		role = m.DefaultRole
	}
	if roleRank[role] == 0 {
		http.Error(w, "unknown role", 400)
		return
	}
	if role == roleOwner && a.missionRole(callerOf(r), m) != roleOwner {
		http.Error(w, "only owners can invite owners", 403)
		return
	}
	creator := r.URL.Query().Get("creatorUid")
	if err := a.store.Invite(m.Name, invitee, role, creator); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.router.SendToUIDs(a.missionInviteCoT(m, creator, role), []string{invitee})
	w.WriteHeader(http.StatusOK)
}

func (a *App) mUninvite(w http.ResponseWriter, r *http.Request) {
	m, ok := a.loadWritable(w, r)
	if !ok {
		return
	}
	_ = a.store.Uninvite(m.Name, r.PathValue("invitee"))
	w.WriteHeader(http.StatusOK)
}

func (a *App) mAddLog(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID           string   `json:"id"`
		Content      string   `json:"content"`
		CreatorUID   string   `json:"creatorUid"`
		EntryUID     string   `json:"entryUid"`
		MissionNames []string `json:"missionNames"`
		DTG          string   `json:"dtg"`
		Keywords     []string `json:"keywords"`
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err := remarshalBytes(body, &in); err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	if in.ID == "" {
		in.ID = randomUUID()
	}
	l := store.MissionLog{ID: in.ID, Content: in.Content, CreatorUID: in.CreatorUID, EntryUID: in.EntryUID, DTG: cot.ParseTime(in.DTG), Keywords: in.Keywords}
	var last map[string]any
	for _, name := range in.MissionNames {
		m, err := a.store.GetMission(name)
		if err != nil || !a.canWrite(callerOf(r), m) {
			continue
		}
		if err := a.store.UpsertMissionLog(m.Name, &l); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		last = logJSON(m.Name, l)
	}
	if last == nil {
		http.Error(w, "mission not found", 404)
		return
	}
	a.martiJSON(w, 201, "com.bbn.marti.sync.model.LogEntry", last)
}

func (a *App) mDeleteLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	names, _ := a.store.MissionLogMissions(id)
	deleted := 0
	for _, name := range names {
		m, err := a.store.GetMission(name)
		if err != nil || !a.canWrite(callerOf(r), m) {
			continue
		}
		if err := a.store.DeleteMissionLog(id, m.Name); err == nil {
			deleted++
		}
	}
	if deleted == 0 && len(names) > 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---------- CoT side: <dest mission="…"/> and change notifications ----------

// missionCoT stores an event addressed to a mission and returns the subscribers to deliver it to.
func (a *App) missionCoT(c *stream.Client, ev *cot.Event, name string) []string {
	m, ok := a.streamWritable(c, name)
	if !ok {
		return nil
	}
	subs, _ := a.store.Subscriptions(m.Name)
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.ClientUID)
	}
	return out
}

// storeMissionCoT persists an event addressed to a mission; it runs after the event was relayed so
// subscribers see the item before the change notification.
func (a *App) storeMissionCoT(c *stream.Client, ev *cot.Event, name string) {
	m, ok := a.streamWritable(c, name)
	if !ok {
		return
	}
	if ev.IsDelete() {
		for _, l := range ev.Links {
			if removed, _ := a.store.RemoveMissionItem(m.Name, l.UID); removed {
				ch := &store.MissionChange{Type: "REMOVE_CONTENT", UID: l.UID, CreatorUID: c.UID()}
				_ = a.store.AddMissionChange(m.Name, ch)
				a.notifyMissionChange(m, *ch)
			}
		}
		return
	}
	if !ev.IsChat() {
		a.addMissionItemSilently(m, ev, c.UID())
	}
}

func (a *App) addMissionItemSilently(m *store.Mission, ev *cot.Event, creator string) {
	a.missionMu.Lock()
	defer a.missionMu.Unlock()
	isNew, err := a.store.UpsertMissionItem(m.Name, &store.MissionItem{
		UID: ev.UID, Type: ev.Type, Callsign: ev.Callsign, Lat: ev.Lat, Lon: ev.Lon, CoT: string(ev.Raw), CreatorUID: creator,
	})
	if err == nil && isNew {
		ch := &store.MissionChange{Type: "ADD_CONTENT", UID: ev.UID, CreatorUID: creator}
		_ = a.store.AddMissionChange(m.Name, ch)
		a.notifyMissionChange(m, *ch)
	}
}

func (a *App) addMissionItem(m *store.Mission, uid, typ, callsign string, lat, lon float64, raw, creator string) {
	a.missionMu.Lock()
	defer a.missionMu.Unlock()
	isNew, err := a.store.UpsertMissionItem(m.Name, &store.MissionItem{UID: uid, Type: typ, Callsign: callsign, Lat: lat, Lon: lon, CoT: raw, CreatorUID: creator})
	if err == nil && isNew {
		ch := &store.MissionChange{Type: "ADD_CONTENT", UID: uid, CreatorUID: creator}
		_ = a.store.AddMissionChange(m.Name, ch)
		a.notifyMissionChange(m, *ch)
		subs, _ := a.store.Subscriptions(m.Name)
		var uids []string
		for _, s := range subs {
			uids = append(uids, s.ClientUID)
		}
		a.router.SendToUIDs([]byte(raw), uids)
	}
}

func (a *App) notifyMissionChange(m *store.Mission, c store.MissionChange) {
	subs, _ := a.store.Subscriptions(m.Name)
	a.notifyMission(m, subs, "CHANGE", []store.MissionChange{c})
}

type xmlMissionChange struct {
	XMLName         xml.Name `xml:"MissionChange"`
	ContentResource *struct {
		Inner string `xml:",innerxml"`
	} `xml:"contentResource,omitempty"`
	ContentUID  string `xml:"contentUid,omitempty"`
	CreatorUID  string `xml:"creatorUid"`
	IsFederated bool   `xml:"isFederatedChange"`
	MissionName string `xml:"missionName"`
	Timestamp   string `xml:"timestamp"`
	Type        string `xml:"type"`
}

// notifyMission sends a t-x-m-c mission notification to every subscriber.
func (a *App) notifyMission(m *store.Mission, subs []store.MissionSub, kind string, changes []store.MissionChange) {
	if len(subs) == 0 {
		return
	}
	var inner strings.Builder
	if len(changes) > 0 {
		inner.WriteString("<MissionChanges>")
		for _, c := range changes {
			x := xmlMissionChange{ContentUID: c.UID, CreatorUID: c.CreatorUID, MissionName: m.Name, Timestamp: martiTime(c.CreatedAt), Type: c.Type}
			if c.Hash != "" {
				var cr strings.Builder
				if f, err := a.store.GetFile(c.Hash); err == nil {
					fmt.Fprintf(&cr, "<filename>%s</filename><hash>%s</hash><name>%s</name><size>%d</size><submitter>%s</submitter><uid>%s</uid><creatorUid>%s</creatorUid><mimeType>%s</mimeType><submissionTime>%s</submissionTime>",
						xmlEscape(f.Name), f.Hash, xmlEscape(f.Name), f.Size, xmlEscape(f.Submitter), xmlEscape(f.UID), xmlEscape(f.CreatorUID), xmlEscape(f.MIME), martiTime(f.CreatedAt))
				} else {
					fmt.Fprintf(&cr, "<hash>%s</hash>", xmlEscape(c.Hash))
				}
				x.ContentResource = &struct {
					Inner string `xml:",innerxml"`
				}{Inner: cr.String()}
			}
			b, _ := xml.Marshal(x)
			inner.Write(b)
		}
		inner.WriteString("</MissionChanges>")
	}
	now := time.Now()
	raw := fmt.Sprintf(`<event version="2.0" uid="%s" type="t-x-m-c" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><mission type="%s" tool="%s" name="%s" guid="%s" authorUid="%s">%s</mission></detail></event>`,
		randomUUID(), cot.FormatTime(now), cot.FormatTime(now), cot.FormatTime(now.Add(20*time.Second)),
		kind, xmlEscape(m.Tool), xmlEscape(m.Name), xmlEscape(m.GUID), xmlEscape(a.serverUID()), inner.String())
	uids := make([]string, 0, len(subs))
	for _, s := range subs {
		uids = append(uids, s.ClientUID)
	}
	a.router.SendToUIDs([]byte(raw), uids)
}

// broadcastMissionCreated tells everyone in the mission's groups that a new mission exists.
func (a *App) broadcastMissionCreated(m *store.Mission) {
	now := time.Now()
	raw := fmt.Sprintf(`<event version="2.0" uid="%s" type="t-x-m-n" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><mission type="CREATE" tool="%s" name="%s" guid="%s" authorUid="%s"/></detail></event>`,
		randomUUID(), cot.FormatTime(now), cot.FormatTime(now), cot.FormatTime(now.Add(20*time.Second)),
		xmlEscape(m.Tool), xmlEscape(m.Name), xmlEscape(m.GUID), xmlEscape(m.CreatorUID))
	var groups []string
	if len(m.Groups) > 0 {
		groups = m.Groups
	}
	a.router.Broadcast([]byte(raw), groups)
}

func (a *App) missionInviteCoT(m *store.Mission, creator, role string) []byte {
	now := time.Now()
	return []byte(fmt.Sprintf(`<event version="2.0" uid="%s" type="t-x-m-i" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><mission type="INVITE" tool="%s" name="%s" guid="%s" authorUid="%s"><role type="%s"/></mission></detail></event>`,
		randomUUID(), cot.FormatTime(now), cot.FormatTime(now), cot.FormatTime(now.Add(20*time.Second)),
		xmlEscape(m.Tool), xmlEscape(m.Name), xmlEscape(m.GUID), xmlEscape(creator), xmlEscape(role)))
}

// ---------- panel ----------

func (a *App) panelMission(m *store.Mission) map[string]any {
	files, _ := a.store.MissionFiles(m.Name)
	items, _ := a.store.MissionItems(m.Name)
	subs, _ := a.store.Subscriptions(m.Name)
	groups := m.Groups
	if groups == nil {
		groups = []string{}
	}
	return map[string]any{
		"name": m.Name, "description": m.Description, "creatorUid": m.CreatorUID, "createdAt": m.CreatedAt, "guid": m.GUID,
		"contents": len(files), "uids": len(items), "subscribers": len(subs), "passwordProtected": m.PasswordHash != "", "groups": groups,
	}
}

func (a *App) hListMissions(w http.ResponseWriter, r *http.Request) {
	ms, err := a.store.ListMissions()
	if err != nil {
		fail(w, err)
		return
	}
	out := []map[string]any{}
	for _, m := range ms {
		out = append(out, a.panelMission(m))
	}
	writeJSON(w, 200, out)
}

func (a *App) hCreateMission(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Groups      []string `json:"groups"`
		Password    string   `json:"password"`
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	m, err := a.createMission(in.Name, in.Description, a.serverUID(), "public", in.Password, in.Groups)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 201, a.panelMission(m))
}

func (a *App) hGetMission(w http.ResponseWriter, r *http.Request) {
	m, err := a.store.GetMission(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	v := a.panelMission(m)
	files, _ := a.store.MissionFiles(m.Name)
	fl := []map[string]any{}
	for _, f := range files {
		fl = append(fl, fileJSON(f.File))
	}
	items, _ := a.store.MissionItems(m.Name)
	il := []map[string]any{}
	for _, it := range items {
		il = append(il, map[string]any{"uid": it.UID, "callsign": it.Callsign, "type": it.Type, "lat": it.Lat, "lon": it.Lon, "createdAt": it.CreatedAt})
	}
	subs, _ := a.store.Subscriptions(m.Name)
	sl := []map[string]any{}
	for _, s := range subs {
		sl = append(sl, map[string]any{"clientUid": s.ClientUID, "username": s.Username, "role": s.Role, "createdAt": s.CreatedAt})
	}
	changes, _ := a.store.MissionChanges(m.Name, time.Time{})
	cl := []map[string]any{}
	for _, c := range changes {
		cl = append(cl, map[string]any{"type": c.Type, "time": c.CreatedAt, "creatorUid": c.CreatorUID, "hash": c.Hash, "uid": c.UID})
	}
	v["files"], v["items"], v["subscribers"], v["changes"] = fl, il, sl, cl
	v["subscriberCount"] = len(sl)
	writeJSON(w, 200, v)
}

func (a *App) hDeleteMission(w http.ResponseWriter, r *http.Request) {
	m, err := a.store.GetMission(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	subs, _ := a.store.Subscriptions(m.Name)
	if err := a.store.DeleteMission(m.Name); err != nil {
		fail(w, err)
		return
	}
	a.notifyMission(m, subs, "DELETE", nil)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) hMissionAddFile(w http.ResponseWriter, r *http.Request) {
	m, err := a.store.GetMission(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	var in struct{ Hash string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if _, err := a.store.GetFile(in.Hash); err != nil {
		fail(w, err)
		return
	}
	if added, err := a.store.AddMissionFile(m.Name, in.Hash, a.serverUID()); err != nil {
		fail(w, err)
		return
	} else if added {
		c := &store.MissionChange{Type: "ADD_CONTENT", Hash: in.Hash, CreatorUID: a.serverUID()}
		_ = a.store.AddMissionChange(m.Name, c)
		a.notifyMissionChange(m, *c)
	}
	writeJSON(w, 200, a.panelMission(m))
}

func (a *App) hMissionRemoveFile(w http.ResponseWriter, r *http.Request) {
	m, err := a.store.GetMission(r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	hash := r.PathValue("hash")
	if removed, err := a.store.RemoveMissionFile(m.Name, hash); err != nil {
		fail(w, err)
		return
	} else if removed {
		c := &store.MissionChange{Type: "REMOVE_CONTENT", Hash: hash, CreatorUID: a.serverUID()}
		_ = a.store.AddMissionChange(m.Name, c)
		a.notifyMissionChange(m, *c)
	}
	writeJSON(w, 200, a.panelMission(m))
}
