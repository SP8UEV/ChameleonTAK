package cot

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
	"time"
)

type Dest struct {
	Callsign string
	UID      string
	Mission  string
}

type Chat struct {
	ID             string
	Chatroom       string
	SenderCallsign string
	Parent         string
	MessageID      string
}

// Event is a parsed CoT event. Raw keeps the exact bytes received so events are relayed unchanged.
type Event struct {
	Raw []byte

	UID   string
	Type  string
	How   string
	Time  time.Time
	Start time.Time
	Stale time.Time

	Lat, Lon, Hae float64

	HasContact bool
	Callsign   string
	Endpoint   string
	Phone      string

	GroupName string
	GroupRole string

	Battery string
	Course  float64
	Speed   float64

	TakvDevice   string
	TakvPlatform string
	TakvOS       string
	TakvVersion  string

	Dest    []Dest
	Chat    *Chat
	Remarks string
	Links   []Link
}

type Link struct {
	UID      string
	Type     string
	Relation string
}

type xmlEvent struct {
	XMLName xml.Name `xml:"event"`
	UID     string   `xml:"uid,attr"`
	Type    string   `xml:"type,attr"`
	How     string   `xml:"how,attr"`
	Time    string   `xml:"time,attr"`
	Start   string   `xml:"start,attr"`
	Stale   string   `xml:"stale,attr"`
	Point   struct {
		Lat string `xml:"lat,attr"`
		Lon string `xml:"lon,attr"`
		Hae string `xml:"hae,attr"`
	} `xml:"point"`
	Detail struct {
		Contact *struct {
			Callsign string `xml:"callsign,attr"`
			Endpoint string `xml:"endpoint,attr"`
			Phone    string `xml:"phone,attr"`
		} `xml:"contact"`
		Group *struct {
			Name string `xml:"name,attr"`
			Role string `xml:"role,attr"`
		} `xml:"__group"`
		Status *struct {
			Battery string `xml:"battery,attr"`
		} `xml:"status"`
		Track *struct {
			Course string `xml:"course,attr"`
			Speed  string `xml:"speed,attr"`
		} `xml:"track"`
		Takv *struct {
			Device   string `xml:"device,attr"`
			Platform string `xml:"platform,attr"`
			OS       string `xml:"os,attr"`
			Version  string `xml:"version,attr"`
		} `xml:"takv"`
		Marti *struct {
			Dest []struct {
				Callsign string `xml:"callsign,attr"`
				UID      string `xml:"uid,attr"`
				Mission  string `xml:"mission,attr"`
			} `xml:"dest"`
		} `xml:"marti"`
		Chat *struct {
			ID             string `xml:"id,attr"`
			Chatroom       string `xml:"chatroom,attr"`
			SenderCallsign string `xml:"senderCallsign,attr"`
			Parent         string `xml:"parent,attr"`
			MessageID      string `xml:"messageId,attr"`
		} `xml:"__chat"`
		Remarks *struct {
			Text string `xml:",chardata"`
		} `xml:"remarks"`
		Links []struct {
			UID      string `xml:"uid,attr"`
			Type     string `xml:"type,attr"`
			Relation string `xml:"relation,attr"`
		} `xml:"link"`
	} `xml:"detail"`
}

// Parse decodes an <event> element. Unknown detail elements are ignored but preserved in Raw.
func Parse(raw []byte) (*Event, error) {
	var x xmlEvent
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	e := &Event{
		Raw:   raw,
		UID:   x.UID,
		Type:  x.Type,
		How:   x.How,
		Time:  ParseTime(x.Time),
		Start: ParseTime(x.Start),
		Stale: ParseTime(x.Stale),
		Lat:   atof(x.Point.Lat),
		Lon:   atof(x.Point.Lon),
		Hae:   atof(x.Point.Hae),
	}
	d := &x.Detail
	if d.Contact != nil {
		e.HasContact = true
		e.Callsign, e.Endpoint, e.Phone = d.Contact.Callsign, d.Contact.Endpoint, d.Contact.Phone
	}
	if d.Group != nil {
		e.GroupName, e.GroupRole = d.Group.Name, d.Group.Role
	}
	if d.Status != nil {
		e.Battery = d.Status.Battery
	}
	if d.Track != nil {
		e.Course, e.Speed = atof(d.Track.Course), atof(d.Track.Speed)
	}
	if d.Takv != nil {
		e.TakvDevice, e.TakvPlatform, e.TakvOS, e.TakvVersion = d.Takv.Device, d.Takv.Platform, d.Takv.OS, d.Takv.Version
	}
	if d.Marti != nil {
		for _, ds := range d.Marti.Dest {
			e.Dest = append(e.Dest, Dest{Callsign: ds.Callsign, UID: ds.UID, Mission: ds.Mission})
		}
	}
	if d.Chat != nil {
		e.Chat = &Chat{ID: d.Chat.ID, Chatroom: d.Chat.Chatroom, SenderCallsign: d.Chat.SenderCallsign, Parent: d.Chat.Parent, MessageID: d.Chat.MessageID}
	}
	if d.Remarks != nil {
		e.Remarks = strings.TrimSpace(d.Remarks.Text)
	}
	for _, l := range d.Links {
		e.Links = append(e.Links, Link{UID: l.UID, Type: l.Type, Relation: l.Relation})
	}
	return e, nil
}

// IsSA reports whether the event is a client's own situational-awareness (position) report.
func (e *Event) IsSA() bool {
	return strings.HasPrefix(e.Type, "a-") && e.HasContact && e.Endpoint != "" && (e.TakvPlatform != "" || e.GroupName != "")
}

func (e *Event) IsPing() bool { return e.Type == "t-x-c-t" }

func (e *Event) IsChat() bool { return strings.HasPrefix(e.Type, "b-t-f") && e.Chat != nil }

func (e *Event) IsDelete() bool { return e.Type == "t-x-d-d" }

// IsControl covers protocol negotiation and other server-directed messages that must not be relayed.
func (e *Event) IsControl() bool {
	return strings.HasPrefix(e.Type, "t-x-takp") || e.Type == "t-x-c-t" || e.Type == "t-x-c-t-r"
}

// ChatText extracts the message body of a GeoChat event.
func (e *Event) ChatText() string { return e.Remarks }

const timeLayout = "2006-01-02T15:04:05.000Z"

func FormatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func ParseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", strings.TrimSuffix(s, "Z")); err == nil {
		return t
	}
	return time.Time{}
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// Escape escapes text for use inside XML attributes or character data.
func Escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
