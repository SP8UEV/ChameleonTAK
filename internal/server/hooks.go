package server

import (
	"time"

	"chameleontak/internal/cot"
	"chameleontak/internal/store"
	"chameleontak/internal/stream"
)

// The App implements stream.Hooks.

func (a *App) OnConnect(c *stream.Client) {
	if c.Identity.DeviceID > 0 {
		_ = a.store.TouchDevice(c.Identity.DeviceID, "")
	}
	a.hub.Publish("client", map[string]any{"event": "connect", "client": c.Info()})
}

func (a *App) OnDisconnect(c *stream.Client) {
	if uid := c.UID(); uid != "" && a.router.ClientByUID(uid) == nil {
		a.pos.SetOffline(uid)
	}
	if c.Identity.DeviceID > 0 {
		_ = a.store.TouchDevice(c.Identity.DeviceID, "")
	}
	a.hub.Publish("client", map[string]any{"event": "disconnect", "client": c.Info()})
}

func (a *App) OnEvent(c *stream.Client, ev *cot.Event) {
	ownSA := ev.IsSA() && ev.UID == c.UID()
	if ownSA && c.Identity.DeviceID > 0 {
		_ = a.store.TouchDevice(c.Identity.DeviceID, ev.UID)
	}
	a.pos.Update(ev, c.Identity.Username, ownSA)
	for _, d := range ev.Dest {
		if d.Mission != "" {
			a.storeMissionCoT(c, ev, d.Mission)
		}
	}

	if ev.IsChat() {
		msg := &store.ChatMessage{
			Time:    time.Now().UTC(),
			From:    ev.Chat.SenderCallsign,
			FromUID: c.UID(),
			To:      ev.Chat.Chatroom,
			Text:    ev.ChatText(),
		}
		if msg.From == "" {
			msg.From = c.Callsign()
		}
		if err := a.store.AddChat(msg); err == nil {
			a.hub.Publish("chat", msg)
		}
	}
}

func (a *App) MissionEvent(c *stream.Client, ev *cot.Event, mission string) []string {
	return a.missionCoT(c, ev, mission)
}

func (a *App) Disconnected(c *stream.Client) []byte {
	return cot.Disconnect(c.UID(), c.SAType())
}
