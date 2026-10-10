package network

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

func TestUpdateDHCPPool(t *testing.T) {
	ctx := context.Background()

	f := &fakeCaller{}
	if err := updateDHCPPool(ctx, f, "lan", DHCPPoolUpdate{}); err != nil || len(f.sent) != 0 {
		t.Fatalf("empty update: err %v, calls %v", err, f.sent)
	}

	f = &fakeCaller{results: map[string]any{
		"GetDhcpPools": map[string]any{
			"lan":   map[string]any{dhcpPoolInterface: "lan", dhcpPoolOffset: int8(100), "20": map[string]any{"runtime": 1}},
			"guest": map[string]any{dhcpPoolInterface: "guest", "20": map[string]any{}},
		},
		"SetDhcpPools": true,
	}}
	offset := 50
	if err := updateDHCPPool(ctx, f, "lan", DHCPPoolUpdate{Offset: &offset}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"lan":   map[string]any{dhcpPoolInterface: "lan", dhcpPoolOffset: int64(50)},
		"guest": map[string]any{dhcpPoolInterface: "guest"},
	}
	if got := f.sent["SetDhcpPools"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %#v\nwant %#v", got, want)
	}

	if err := updateDHCPPool(ctx, f, "iot", DHCPPoolUpdate{Offset: &offset}); err == nil {
		t.Fatal("missing pool accepted")
	}
}

func TestParseDHCPPools(t *testing.T) {
	m := map[string]any{
		"lan": map[string]any{
			dhcpPoolInterface: "lan",
			dhcpPoolIgnore:    false,
			dhcpPoolOffset:    int8(100),
			dhcpPoolLength:    uint8(150),
			dhcpPoolLeaseTime: uint32(86400),
			dhcpPoolDHCP4Mode: int8(2),
			dhcpPoolDHCP6Mode: int8(4),
			dhcpPoolRAMode:    int8(9),
		},
		"guest": map[string]any{dhcpPoolIgnore: true},
	}
	got := parseDHCPPools(m)
	want := []DHCPPool{
		{Name: "guest", Ignore: true},
		{Name: "lan", Interface: "lan", Offset: 100, Length: 150, LeaseTime: 86400, DHCP4Mode: "server", DHCP6Mode: "hybrid", RAMode: "#9"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestDHCPPoolUpdateOptions(t *testing.T) {
	offset, mode, ignore := 50, "relay", true
	got, err := DHCPPoolUpdate{Offset: &offset, DHCP4Mode: &mode, Ignore: &ignore}.options()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{dhcpPoolOffset: int64(50), dhcpPoolDHCP4Mode: int64(3), dhcpPoolIgnore: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}

	bad := "on"
	if _, err := (DHCPPoolUpdate{NDPMode: &bad}).options(); err == nil {
		t.Fatal("unknown mode should fail")
	}
}

func TestDHCPStaticLeasesRoundTrip(t *testing.T) {
	l := DHCPStaticLease{MAC: amplifi.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b}, IP: "192.168.1.20", Name: "printer", Description: "upstairs"}
	mac, f, err := dhcpLeaseOptions(l)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f[dhcpLeaseAddress].([]byte), []byte{192, 168, 1, 20}) {
		t.Fatalf("address: %v", f[dhcpLeaseAddress])
	}

	packed, err := dhcpPackLeases(map[string]any{string(mac): f})
	if err != nil {
		t.Fatal(err)
	}
	// fixmap(1), bin8 of 6 bytes for the key.
	if !bytes.HasPrefix(packed, []byte{0x81, 0xc4, 0x06, 0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b}) {
		t.Fatalf("key not packed as binary: % x", packed)
	}

	v, err := wire.DecodeMsgpack(packed)
	if err != nil {
		t.Fatal(err)
	}
	got := parseDHCPStaticLeases(v.(map[string]any))
	if !reflect.DeepEqual(got, []DHCPStaticLease{l}) {
		t.Fatalf("round trip: %+v", got)
	}

	if _, _, err := dhcpLeaseOptions(DHCPStaticLease{IP: "192.0.2.1"}); err == nil {
		t.Fatal("missing MAC should fail")
	}
	if _, _, err := dhcpLeaseOptions(DHCPStaticLease{MAC: l.MAC, IP: "192.0.2"}); err == nil {
		t.Fatal("bad IP should fail")
	}
}

func TestParseDHCPStaticLeasesSorted(t *testing.T) {
	m := map[string]any{
		"\x00\x00\x00\x00\x00\x02": map[string]any{dhcpLeaseName: "b"},
		"\x00\x00\x00\x00\x00\x01": map[string]any{dhcpLeaseName: "a", dhcpLeaseAddress: []byte{192, 0, 2, 1}},
		"short":                    map[string]any{dhcpLeaseName: "ignored"},
	}
	got := parseDHCPStaticLeases(m)
	if len(got) != 2 || got[0].Name != "a" || got[0].IP != "192.0.2.1" || got[1].MAC.String() != "00:00:00:00:00:02" {
		t.Fatalf("unexpected: %+v", got)
	}
}
