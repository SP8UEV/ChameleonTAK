package system

import "testing"

func TestLoopbackHex(t *testing.T) {
	for h, want := range map[string]bool{
		"0100007F":                         true,  // 127.0.0.1
		"00000000":                         false, // 0.0.0.0
		"6601A8C0":                         false, // 192.168.1.102
		"00000000000000000000000001000000": true,  // ::1
		"00000000000000000000000000000000": false, // ::
	} {
		if got := loopbackHex(h); got != want {
			t.Errorf("%s: %v, want %v", h, got, want)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	if got := NormalizeMAC("DC:A6:32:01:02:03"); got != "dc:a6:32:01:02:03" {
		t.Fatal(got)
	}
	if NormalizeMAC("00:00:00:00:00:00") != "" || NormalizeMAC("nope") != "" {
		t.Fatal("invalid MAC accepted")
	}
}
