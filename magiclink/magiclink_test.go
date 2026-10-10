package magiclink

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

func TestClientList(t *testing.T) {
	raw := `{
		"b": {"friendly_name": "laptop", "access_mode": "WAN", "mac": "aa:bb:cc:dd:ee:ff",
		      "pub_ipv4_addr": "1.2.3.4", "ipv4_addr": "192.0.2.5", "online": true, "conn_type": "P2P",
		      "owner_device_id": "o", "type": "Owner", "joined": 1700000000, "last_seen": 1700000100,
		      "platform": "IOS", "state": "CONNECTED", "rx_rate": 10, "tx_rate": 20, "uptime": 30},
		"a": {"friendly_name": "phone", "access_mode": "FULL", "online": false, "joined": 1}
	}`
	var m map[string]Client
	if err := rpc.Decode("RtGetClients", raw, &m); err != nil {
		t.Fatal(err)
	}

	got := clientList(m)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("got %+v", got)
	}
	b := got[1]
	if b.FriendlyName != "laptop" || b.AccessMode != AccessWAN || b.PublicIP != "1.2.3.4" || !b.Online ||
		b.MAC.String() != "aa:bb:cc:dd:ee:ff" || b.ConnectionType != "P2P" || b.Joined != 1700000000 ||
		*b.LastSeen != 1700000100 || *b.Uptime != 30 {
		t.Errorf("got %+v", b)
	}
}

func TestPairingList(t *testing.T) {
	var m map[string]Pairing
	if err := json.Unmarshal([]byte(`{"AA:BB:CC:DD:EE:FF": {"friendly_name": "Hallway", "state": "pending"}}`), &m); err != nil {
		t.Fatal(err)
	}

	got := pairingList(m)
	mac, _ := amplifi.ParseMAC("aa:bb:cc:dd:ee:ff")
	want := []Pairing{{PeerID: "AA:BB:CC:DD:EE:FF", MAC: mac, FriendlyName: "Hallway", State: "pending"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
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
