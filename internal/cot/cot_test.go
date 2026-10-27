package cot

import (
	"io"
	"strings"
	"testing"
)

const sa = `<event version="2.0" uid="ANDROID-abc" type="a-f-G-U-C" how="m-g" time="2026-10-25T10:00:00.000Z" start="2026-10-25T10:00:00.000Z" stale="2026-10-25T10:06:00.000Z"><point lat="52.1" lon="21.0" hae="100" ce="9999999" le="9999999"/><detail><takv os="34" version="5.4.0" device="PIXEL" platform="ATAK-CIV"/><contact endpoint="*:-1:stcp" callsign="ALFA1"/><__group role="Team Member" name="Cyan"/><status battery="88"/><track course="90.5" speed="1.2"/></detail></event>`

func TestFramerSplitsConcatenatedEvents(t *testing.T) {
	ping := `<event version="2.0" uid="ANDROID-abc-ping" type="t-x-c-t" how="m-g" time="2026-10-25T10:00:00Z" start="2026-10-25T10:00:00Z" stale="2026-10-25T10:00:10Z"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail/></event>`
	stream := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + sa + "\n  " + `<?xml version='1.0' encoding='ASCII' standalone='yes'?>` + ping
	f := NewFramer(strings.NewReader(stream))
	got1, err := f.Next()
	if err != nil {
		t.Fatal(err)
	}
	if string(got1) != sa {
		t.Fatalf("first event mismatch:\n%s", got1)
	}
	got2, err := f.Next()
	if err != nil {
		t.Fatal(err)
	}
	if string(got2) != ping {
		t.Fatalf("second event mismatch:\n%s", got2)
	}
	if _, err := f.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

// slowReader returns one byte per Read to simulate TCP fragmentation.
type slowReader struct{ s string }

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.s) == 0 {
		return 0, io.EOF
	}
	p[0] = r.s[0]
	r.s = r.s[1:]
	return 1, nil
}

func TestFramerFragmentedInput(t *testing.T) {
	f := NewFramer(&slowReader{s: sa + sa})
	for i := 0; i < 2; i++ {
		got, err := f.Next()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != sa {
			t.Fatalf("event %d mismatch", i)
		}
	}
}

func TestParseSA(t *testing.T) {
	ev, err := Parse([]byte(sa))
	if err != nil {
		t.Fatal(err)
	}
	if !ev.IsSA() || ev.Callsign != "ALFA1" || ev.GroupName != "Cyan" || ev.Battery != "88" || ev.TakvPlatform != "ATAK-CIV" {
		t.Fatalf("unexpected parse: %+v", ev)
	}
	if ev.Lat != 52.1 || ev.Lon != 21.0 || ev.Course != 90.5 {
		t.Fatalf("bad point/track: %+v", ev)
	}
	if ev.Stale.IsZero() {
		t.Fatal("stale not parsed")
	}
}

// A client's own SA is recognized by its app or team even without a contact endpoint; a marker
// (contact only) is not.
func TestSAWithoutEndpoint(t *testing.T) {
	own := `<event version="2.0" uid="IOS-1" type="a-f-G-U-C" how="m-g"><point lat="52.1" lon="21.0" hae="0" ce="5" le="5"/><detail><contact callsign="JS2088"/><__group name="Cyan" role="Team Member"/><takv platform="iTAK" device="iPhone" os="18" version="2.12"/></detail></event>`
	if ev, err := Parse([]byte(own)); err != nil || !ev.IsSA() {
		t.Fatalf("iTAK SA not recognized: %+v %v", ev, err)
	}
	marker := `<event version="2.0" uid="m-1" type="a-h-G" how="h-g-i-g-o"><point lat="52.1" lon="21.0" hae="0" ce="5" le="5"/><detail><contact callsign="Wrog 1"/></detail></event>`
	if ev, err := Parse([]byte(marker)); err != nil || ev.IsSA() {
		t.Fatalf("marker taken for an SA: %+v %v", ev, err)
	}
}

func TestParseChatWithDest(t *testing.T) {
	raw := `<event version="2.0" uid="GeoChat.A.B.1" type="b-t-f" how="h-g-i-g-o" time="2026-10-25T10:00:00Z" start="2026-10-25T10:00:00Z" stale="2026-10-26T10:00:00Z"><point lat="0" lon="0" hae="0" ce="9999999" le="9999999"/><detail><__chat parent="RootContactGroup" groupOwner="false" messageId="1" chatroom="BRAVO" id="B" senderCallsign="ALFA1"><chatgrp uid0="A" uid1="B" id="B"/></__chat><link uid="A" type="a-f-G-U-C" relation="p-p"/><remarks source="BAO.F.ATAK.A" to="B" time="2026-10-25T10:00:00Z">czesc &amp; hej</remarks><marti><dest callsign="BRAVO"/></marti></detail></event>`
	ev, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !ev.IsChat() || ev.ChatText() != "czesc & hej" || len(ev.Dest) != 1 || ev.Dest[0].Callsign != "BRAVO" {
		t.Fatalf("unexpected chat parse: %+v", ev)
	}
}

func TestServerChatParses(t *testing.T) {
	ev, err := Parse(ServerChat("SRV", "Serwer <1>", "All Chat Rooms", "", "m1", "Uwaga & test"))
	if err != nil {
		t.Fatal(err)
	}
	if !ev.IsChat() || ev.ChatText() != "Uwaga & test" || ev.Chat.SenderCallsign != "Serwer <1>" {
		t.Fatalf("server chat roundtrip failed: %+v", ev.Chat)
	}
}
