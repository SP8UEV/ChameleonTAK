package cot

import (
	"fmt"
	"time"
)

// Pong answers a client's t-x-c-t keep-alive.
func Pong() []byte {
	now := time.Now()
	return []byte(fmt.Sprintf(`<event version="2.0" uid="takPong" type="t-x-c-t-r" how="h-g-i-g-o" time="%s" start="%s" stale="%s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail/></event>`,
		FormatTime(now), FormatTime(now), FormatTime(now.Add(20*time.Second))))
}

// ServerChat builds a GeoChat message sent by the server itself (e.g. from the web panel).
// chatroom is "All Chat Rooms" for a broadcast or the recipient's callsign; recipientUID
// is the recipient's UID for direct messages ("" for broadcast).
func ServerChat(serverUID, serverCallsign, chatroom, recipientUID, msgID, text string) []byte {
	now := time.Now()
	room := chatroom
	id := chatroom
	if recipientUID != "" {
		id = recipientUID
	}
	return []byte(fmt.Sprintf(`<event version="2.0" uid="GeoChat.%[1]s.%[2]s.%[3]s" type="b-t-f" how="h-g-i-g-o" time="%[4]s" start="%[4]s" stale="%[5]s">`+
		`<point lat="0.0" lon="0.0" hae="9999999.0" ce="9999999.0" le="9999999.0"/>`+
		`<detail><__chat parent="RootContactGroup" groupOwner="false" messageId="%[3]s" chatroom="%[6]s" id="%[7]s" senderCallsign="%[8]s">`+
		`<chatgrp uid0="%[1]s" uid1="%[7]s" id="%[7]s"/></__chat>`+
		`<link uid="%[1]s" type="a-f-G-U-C" relation="p-p"/>`+
		`<remarks source="BAO.F.ATAK.%[1]s" to="%[7]s" time="%[4]s">%[9]s</remarks>`+
		`<__serverdestination destinations="0.0.0.0:4242:tcp:%[1]s"/>`+
		`</detail></event>`,
		Escape(serverUID), Escape(id), Escape(msgID), FormatTime(now), FormatTime(now.Add(24*time.Hour)),
		Escape(room), Escape(id), Escape(serverCallsign), Escape(text)))
}

// Disconnect tells peers that a client went offline (TAK Server behaviour): ATAK greys the
// contact out. The link must carry the client's UID and SA type, or ATAK ignores it.
func Disconnect(uid, saType string) []byte {
	if uid == "" || saType == "" {
		return nil
	}
	now := time.Now()
	return []byte(fmt.Sprintf(`<event version="2.0" uid="%[1]s" type="t-x-d-d" how="h-g-i-g-o" time="%[2]s" start="%[2]s" stale="%[3]s"><point lat="0.0" lon="0.0" hae="0.0" ce="9999999.0" le="9999999.0"/><detail><link uid="%[1]s" relation="p-p" type="%[4]s"/></detail></event>`,
		Escape(uid), FormatTime(now), FormatTime(now.Add(20*time.Second)), Escape(saType)))
}
