package firmware

import (
	"encoding/json"
	"testing"
)

func TestProgressDecode(t *testing.T) {
	raw := `{"status":"downloading","nodes":{"00:00:5e:00:53:1b":{"check_result":{"code":16,"status":"downloading"},"info_result":{"type":"router","current":{"version":"4.0.0"},"upgrade":{"version":"4.0.1","code":16}}}}}`
	var p Progress
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	n := p.Nodes["00:00:5e:00:53:1b"]
	if !p.Busy() || n.Check.Code != CodeDownloading || n.Info.Upgrade.Version != "4.0.1" || !n.Info.HasUpgrade() {
		t.Fatalf("decoded %+v", p)
	}

	p.Status = "idle"
	if p.Busy() {
		t.Fatal("idle reported busy")
	}

	same := NodeVersion{Current: Version{Version: "4.0.1"}, Upgrade: Version{Version: "4.0.1"}}
	if same.HasUpgrade() || (NodeVersion{}).HasUpgrade() {
		t.Fatal("no upgrade reported as one")
	}
}
