package teleport

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/borud/amplifi/internal/rpc"
)

func TestClientList(t *testing.T) {
	raw := `{
		"b": {"friendly_name": "laptop", "access_mode": "WAN", "mac": "00:00:5e:00:53:01",
		      "pub_ipv4_addr": "198.51.100.4", "ipv4_addr": "192.0.2.5", "online": true, "conn_type": "P2P",
		      "owner_device_id": "o", "type": "Owner", "joined": 1700000000, "last_seen": 1700000100,
		      "platform": "IOS", "state": "CONNECTED", "rx_rate": 10, "tx_rate": 20, "uptime": 30, "duration": 90},
		"a": {"friendly_name": "phone", "access_mode": "FULL", "online": false, "ipv4_addr": "n/a"}
	}`
	var m map[string]clientEntry
	if err := rpc.Decode("RtGetClients", raw, &m); err != nil {
		t.Fatal(err)
	}

	got := clientList(m)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %+v", got)
	}
	a, b := got[0], got[1]
	if a.IP.IsValid() || !a.Joined.IsZero() || !a.LastSeen.IsZero() {
		t.Errorf("a: %+v", a)
	}
	if b.Name != "laptop" || b.AccessMode != AccessWAN || b.PublicIP.String() != "198.51.100.4" || b.IP.String() != "192.0.2.5" ||
		!b.Online || b.MAC.String() != "00:00:5e:00:53:01" || b.ConnectionType != "P2P" ||
		!b.Joined.Equal(time.Unix(1700000000, 0)) || !b.LastSeen.Equal(time.Unix(1700000100, 0)) ||
		b.Uptime != 30*time.Second || b.Duration != 90*time.Second || b.RxRate != 10 {
		t.Errorf("b: %+v", b)
	}

	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"uptime":30`) || !strings.Contains(string(out), `"duration":90`) {
		t.Errorf("durations not in seconds: %s", out)
	}
}

func TestInviteEntry(t *testing.T) {
	inv, err := inviteEntry{Code: "ABC", Duration: 86400, Sessions: 2, Remaining: 1, Expiration: 1700000000}.invite()
	if err != nil {
		t.Fatal(err)
	}
	if inv.Code != "ABC" || inv.Duration != 24*time.Hour || inv.Sessions != 2 || inv.Remaining != 1 ||
		!inv.Expires.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("got %+v", inv)
	}

	if inv, err := (inviteEntry{}).invite(); err != nil || !inv.Expires.IsZero() {
		t.Fatalf("empty: %+v, %v", inv, err)
	}
	refused := false
	if _, err := (inviteEntry{Success: &refused}).invite(); err == nil {
		t.Fatal("refusal not reported")
	}
}

func TestResultCheck(t *testing.T) {
	if err := (result{Success: true}).check("M"); err != nil {
		t.Error(err)
	}
	if err := (result{}).check("M"); err == nil {
		t.Error("expected error")
	}
}
