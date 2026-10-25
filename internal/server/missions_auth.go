package server

import (
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
)

// ownsUID reports whether the caller may act as the given CoT UID: a live connection of theirs
// uses it, or one of their devices reported it. A UID nobody has claimed yet is accepted (a device
// may call the API before its first position report), except the server's own UID.
func (a *App) ownsUID(c *martiCaller, uid string) bool {
	if uid == "" || uid == a.serverUID() {
		return false
	}
	if cl := a.router.ClientByUID(uid); cl != nil {
		return cl.Identity.UserID == c.User.ID
	}
	d, err := a.store.DeviceByClientUID(uid)
	if err != nil {
		return true
	}
	return d.UserID == c.User.ID
}

var roleRank = map[string]int{roleReadOnly: 1, roleSubscriber: 2, roleOwner: 3}

// missionRole is the caller's best role in a mission ("" when not subscribed). Admins own everything.
func (a *App) missionRole(c *martiCaller, m *store.Mission) string {
	if c.User.IsAdmin {
		return roleOwner
	}
	return a.userMissionRole(c.User.Username, m)
}

func (a *App) userMissionRole(username string, m *store.Mission) string {
	subs, _ := a.store.Subscriptions(m.Name)
	best := ""
	for _, s := range subs {
		if s.Username == username && roleRank[s.Role] > roleRank[best] {
			best = s.Role
		}
	}
	return best
}

// canReadContents: password-protected missions only reveal their contents to subscribers.
func (a *App) canReadContents(c *martiCaller, m *store.Mission) bool {
	return a.canSee(c, m) && (m.PasswordHash == "" || a.missionRole(c, m) != "")
}

// canWrite: changing a mission requires a subscription with write permission.
func (a *App) canWrite(c *martiCaller, m *store.Mission) bool {
	if !a.canSee(c, m) {
		return false
	}
	r := a.missionRole(c, m)
	return r == roleOwner || r == roleSubscriber
}

// streamCaller maps a CoT connection to the Marti caller model (nil for anonymous TCP clients).
func (a *App) streamCaller(c *stream.Client) *martiCaller {
	if c.Identity.UserID == 0 {
		return nil
	}
	u, err := a.store.GetUser(c.Identity.UserID)
	if err != nil || u.Disabled {
		return nil
	}
	return &martiCaller{User: u, DeviceID: c.Identity.DeviceID, ViaCert: true}
}

// allowedGroups keeps only the requested groups the caller belongs to (admins may use any).
func (a *App) allowedGroups(c *martiCaller, requested []string) []string {
	if c.User.IsAdmin {
		return requested
	}
	mine := a.callerGroups(c)
	var out []string
	for _, g := range requested {
		for _, m := range mine {
			if g == m {
				out = append(out, g)
			}
		}
	}
	return out
}

func (a *App) invitedRole(mission, uid string) (string, bool) {
	role, err := a.store.InviteRole(mission, uid)
	return role, err == nil && roleRank[role] > 0
}

// streamWritable resolves a mission addressed by <dest mission="…"/> and checks that the sending
// connection may write to it (anonymous clients never can).
func (a *App) streamWritable(c *stream.Client, name string) (*store.Mission, bool) {
	caller := a.streamCaller(c)
	if caller == nil {
		return nil, false
	}
	m, err := a.store.GetMission(name)
	if err != nil || !a.canWrite(caller, m) {
		return nil, false
	}
	return m, true
}

func sharesGroup(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}
