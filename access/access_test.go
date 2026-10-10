package access

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/borud/amplifi"
)

func decode[T any](t *testing.T, s string) map[string]T {
	t.Helper()
	var m map[string]T
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func mac(t *testing.T, s string) amplifi.MAC {
	t.Helper()
	m, err := amplifi.ParseMAC(s)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestProfileList(t *testing.T) {
	m := decode[profileEntry](t, `{
		"10": {"name": "Kids", "enabled": true, "paused": false, "tag": 3, "devices": ["00:00:5e:00:53:01"]},
		"2": {"name": "Guests", "enabled": true, "paused": true}
	}`)

	got := profileList(m)

	tag := 3
	want := []Profile{
		{ID: "2", Name: "Guests", Enabled: true, Paused: true, Clients: []amplifi.MAC{}},
		{ID: "10", Name: "Kids", Enabled: true, Tag: &tag, Clients: []amplifi.MAC{mac(t, "00:00:5e:00:53:01")}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if id := nextProfileID(got); id != "11" {
		t.Errorf("next profile ID %q", id)
	}
	if id := nextProfileID(nil); id != "1" {
		t.Errorf("first profile ID %q", id)
	}
}

func TestClientList(t *testing.T) {
	m := decode[Client](t, `{
		"00:00:5e:00:53:02": {"name": "tv", "dscp": 40},
		"00:00:5E:00:53:01": {"name": "phone", "tag": 1, "r2r_access_id": "x", "r2r_access_id_type": "Owner"}
	}`)

	got, err := clientList(m)
	if err != nil {
		t.Fatal(err)
	}

	one, streaming := 1, DSCPStreaming
	want := []Client{
		{MAC: mac(t, "00:00:5e:00:53:01"), Name: "phone", Tag: &one, R2RAccessID: "x", R2RAccessIDType: "Owner"},
		{MAC: mac(t, "00:00:5e:00:53:02"), Name: "tv", DSCP: &streaming},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if _, err := clientList(map[string]Client{"nope": {}}); err == nil {
		t.Error("bad MAC key accepted")
	}
}

func TestRuleList(t *testing.T) {
	m := decode[ruleEntry](t, `{
		"10.5": {"description": "bed", "enabled": true, "start_time": "21:00", "duration": "10:00", "week_days": [1,5,7]},
		"2.99": {"enabled": false, "start_time": "08:00", "duration": "24:00", "week_days": [0, 8]}
	}`)

	got, err := ruleList(m)
	if err != nil {
		t.Fatal(err)
	}

	want := []Rule{
		{ID: "2.99", ProfileID: "2", Start: 8 * time.Hour, Duration: 24 * time.Hour, WeekDays: []time.Weekday{}},
		{ID: "10.5", ProfileID: "10", Description: "bed", Enabled: true, Start: 21 * time.Hour, Duration: 10 * time.Hour,
			WeekDays: []time.Weekday{time.Monday, time.Friday, time.Sunday}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if _, err := ruleList(decode[ruleEntry](t, `{"1.1": {"start_time": "25:00", "duration": "01:00"}}`)); err == nil {
		t.Error("bad start time accepted")
	}

	b, err := json.Marshal(got[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"start":75600`) || !strings.Contains(string(b), `"week_days":["Monday","Friday","Sunday"]`) {
		t.Errorf("JSON: %s", b)
	}
}

func TestParseClock(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		{"00:00", 0, true},
		{"07:30", 7*time.Hour + 30*time.Minute, true},
		{"24:00", 24 * time.Hour, true},
		{"24:01", 0, false},
		{"12:60", 0, false},
		{"1230", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, err := parseClock(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("parseClock(%q) = %v, %v", tt.in, got, err)
		}
	}
}

func TestRuleUpdateOptions(t *testing.T) {
	start, duration := 21*time.Hour+30*time.Minute, 9*time.Hour
	got, err := RuleUpdate{Start: &start, Duration: &duration, WeekDays: []time.Weekday{time.Sunday, time.Saturday}}.options()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"start_time": "21:30", "duration": "09:00", "week_days": []int{7, 6}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v", got)
	}

	for _, bad := range []RuleUpdate{
		{Start: new(24 * time.Hour)},
		{Start: new(-time.Minute)},
		{Duration: new(time.Duration(0))},
		{Duration: new(25 * time.Hour)},
	} {
		if _, err := bad.options(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}

	all, err := (Rule{Duration: time.Hour}).update().options()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all["week_days"], []int{}) || all["start_time"] != "00:00" || all["enabled"] != false {
		t.Errorf("new rule body %#v", all)
	}
}

func TestCompareRuleIDs(t *testing.T) {
	if compareRuleIDs("2.9", "10.1") >= 0 || compareRuleIDs("2.10", "2.9") <= 0 || compareRuleIDs("3.1", "3.1") != 0 {
		t.Error("compareRuleIDs")
	}
}

func TestDeleteKeys(t *testing.T) {
	got := deleteKeys([]string{"3", "x"}, true)
	want := map[string]any{"keys": []any{3, "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}

	got = deleteKeys([]string{"3"}, false)
	want = map[string]any{"keys": []any{"3"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestNewRuleID(t *testing.T) {
	a := newRuleID("7")
	time.Sleep(time.Microsecond)
	b := newRuleID("7")
	if a == b {
		t.Fatalf("two IDs are both %q", a)
	}

	for _, id := range []string{a, b} {
		p, n, _ := strings.Cut(id, ".")
		if _, err := strconv.ParseInt(n, 10, 32); p != "7" || err != nil {
			t.Errorf("id %q is not 7.<int32>", id)
		}
		if compareRuleIDs(id, newRuleID("10")) >= 0 || compareRuleIDs(newRuleID("6"), id) >= 0 {
			t.Errorf("id %q does not sort by profile", id)
		}
	}
}
