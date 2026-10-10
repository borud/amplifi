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
	m := decode[Profile](t, `{
		"10": {"name": "Kids", "enabled": true, "paused": false, "tag": 3, "devices": ["aa:bb:cc:dd:ee:ff"]},
		"2": {"name": "Guests", "enabled": true, "paused": true}
	}`)

	got := profileList(m)

	tag := 3
	want := []Profile{
		{ID: "2", Name: "Guests", Enabled: true, Paused: true, Devices: []amplifi.MAC{}},
		{ID: "10", Name: "Kids", Enabled: true, Tag: &tag, Devices: []amplifi.MAC{mac(t, "aa:bb:cc:dd:ee:ff")}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestDeviceList(t *testing.T) {
	m := decode[Device](t, `{
		"aa:bb:cc:dd:ee:02": {"name": "tv", "dscp": 40},
		"AA:BB:CC:DD:EE:01": {"name": "phone", "tag": 1, "r2r_access_id": "x", "r2r_access_id_type": "Owner"}
	}`)

	got, err := deviceList(m)
	if err != nil {
		t.Fatal(err)
	}

	one, streaming := 1, DSCPStreaming
	want := []Device{
		{MAC: mac(t, "aa:bb:cc:dd:ee:01"), Name: "phone", Tag: &one, R2RAccessID: "x", R2RAccessIDType: "Owner"},
		{MAC: mac(t, "aa:bb:cc:dd:ee:02"), Name: "tv", DSCP: &streaming},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if _, err := deviceList(map[string]Device{"nope": {}}); err == nil {
		t.Error("bad MAC key accepted")
	}
}

func TestRuleList(t *testing.T) {
	m := decode[Rule](t, `{
		"10.5": {"description": "bed", "enabled": true, "start_time": "21:00", "duration": "10:00", "week_days": [1,2,3,4,5]},
		"2.99": {"enabled": false, "start_time": "08:00", "duration": "01:30"}
	}`)

	got := ruleList(m)

	want := []Rule{
		{ID: "2.99", StartTime: "08:00", Duration: "01:30", WeekDays: []int{}},
		{ID: "10.5", Description: "bed", Enabled: true, StartTime: "21:00", Duration: "10:00", WeekDays: []int{1, 2, 3, 4, 5}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got[1].ProfileID() != "10" {
		t.Errorf("profile id %q", got[1].ProfileID())
	}
}

func TestListEmpty(t *testing.T) {
	if got := ruleList(nil); got == nil || len(got) != 0 {
		t.Errorf("got %#v", got)
	}
	if got := profileList(nil); got == nil || len(got) != 0 {
		t.Errorf("got %#v", got)
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
	a := NewRuleID("7")
	time.Sleep(time.Microsecond)
	b := NewRuleID("7")
	if a == b {
		t.Fatalf("two IDs are both %q", a)
	}

	for _, id := range []string{a, b} {
		r := Rule{ID: id}
		_, n, _ := strings.Cut(id, ".")
		if _, err := strconv.ParseInt(n, 10, 32); r.ProfileID() != "7" || err != nil {
			t.Errorf("id %q is not 7.<int32>", id)
		}
		if compareRuleIDs(id, NewRuleID("10")) >= 0 || compareRuleIDs(NewRuleID("6"), id) >= 0 {
			t.Errorf("id %q does not sort by profile", id)
		}
	}
}
