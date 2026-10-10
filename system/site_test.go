package system

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDecodeSiteAndRedact(t *testing.T) {
	m := map[string]any{
		siteCountry:          "US",
		siteZoneName:         "America/Chicago",
		siteAcceptDFS:        true,
		siteGuestClientLimit: int8(10),
		siteChannel5:         int8(-1),
		siteWebRTCAuthKey:    "secret",
		"99":                 uint16(7),
	}
	s := decodeSite(m)
	if s.Country != "US" || s.ZoneName != "America/Chicago" || !s.AcceptDFS || s.GuestClientLimit != 10 || s.Channel5 != -1 {
		t.Fatalf("decoded %+v", s)
	}
	if s.Secrets["webrtc_auth_key"] != "secret" {
		t.Fatalf("secrets %v", s.Secrets)
	}

	r := s.Redacted()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(`"secret"`)) || bytes.Contains(b, []byte("null")) {
		t.Fatalf("redacted config leaks a secret or has nulls: %s", b)
	}
	if _, ok := s.Fields[siteWebRTCAuthKey]; !ok || len(s.Secrets) != 1 {
		t.Fatal("Redacted modified the original")
	}
}

func TestSiteUpdateChanges(t *testing.T) {
	zone, limit, off := "America/Chicago", 5, false
	got := SiteUpdate{ZoneName: &zone, GuestClientLimit: &limit, WPSFromLCD: &off}.changes()

	want := map[string]any{siteZoneName: "America/Chicago", siteGuestClientLimit: 5, siteWPSFromLCD: false}
	if len(got) != len(want) {
		t.Fatalf("changes %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("changes[%s] = %v, want %v", k, got[k], v)
		}
	}
}
