package firmware

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/borud/amplifi"
)

func TestProgress(t *testing.T) {
	raw := `{"status":"downloading","nodes":{
		"00:00:5e:00:53:2b":{"check_result":{"code":16,"status":"downloading"},"info_result":{"type":"extender","current":{"version":"4.0.0"}}},
		"00:00:5e:00:53:1b":{"check_result":{"code":16,"status":"downloading"},"info_result":{"type":"router","current":{"version":"4.0.0"},"upgrade":{"version":"4.0.1","code":16},"last_check_time":1700000000}},
		"00:00:5e:00:53:0b":{"info_result":{"type":"extender"}}}}`
	var e progressEntry
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	p := e.progress()

	if p.State != StateDownloading || !p.State.Busy() || len(p.Units) != 3 {
		t.Fatalf("progress %+v", p)
	}
	router := p.Units[0]
	if router.Role != amplifi.RoleRouter || router.MAC.String() != "00:00:5e:00:53:1b" || router.Check.Code != CodeDownloading ||
		!router.HasUpgrade() || !router.LastCheck.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("router %+v", router)
	}
	if p.Units[1].MAC.String() != "00:00:5e:00:53:0b" || p.Units[2].MAC.String() != "00:00:5e:00:53:2b" ||
		p.Units[1].Role != amplifi.RoleMeshPoint || !p.Units[1].LastCheck.IsZero() {
		t.Fatalf("mesh points not sorted by MAC: %+v", p.Units[1:])
	}
}

func TestVersions(t *testing.T) {
	var e versionsEntry
	if err := json.Unmarshal([]byte(`{"status":"idle","nodes":{"00:00:5e:00:53:2b":{"type":"satellite"},"00:00:5e:00:53:3b":{"type":"router"}}}`), &e); err != nil {
		t.Fatal(err)
	}
	v := e.versions()
	if v.State != StateIdle || len(v.Units) != 2 || v.Units[0].Role != amplifi.RoleRouter || v.Units[1].Role != "satellite" {
		t.Fatalf("versions %+v", v)
	}
	if empty := (versionsEntry{}).versions(); empty.Units == nil {
		t.Fatal("nil units")
	}
}

func TestState(t *testing.T) {
	if StateIdle.Busy() || StateUpToDate.Busy() || !StateInProgress.Busy() {
		t.Fatal("Busy")
	}
	if s, err := ParseState("in_progress"); err != nil || s != StateInProgress {
		t.Fatalf("parse: %v, %v", s, err)
	}
	if _, err := ParseState("done"); err == nil {
		t.Fatal("unknown state accepted")
	}

	same := UnitVersion{Current: Version{Version: "4.0.1"}, Upgrade: Version{Version: "4.0.1"}}
	if same.HasUpgrade() || (UnitVersion{}).HasUpgrade() {
		t.Fatal("no upgrade reported as one")
	}
}
