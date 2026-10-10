package dhcp

import (
	"bytes"
	"context"
	"net/netip"
	"reflect"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

// fakeCaller answers Call from a table and records what each method was sent.
type fakeCaller struct {
	results map[string]any
	sent    map[string]any
}

func (f *fakeCaller) Call(_ context.Context, _, method string, arg any) (any, error) {
	if f.sent == nil {
		f.sent = map[string]any{}
	}
	f.sent[method] = arg
	return f.results[method], nil
}

func (f *fakeCaller) CallJSON(ctx context.Context, iface, method string, arg any) (any, error) {
	return f.Call(ctx, iface, method, arg)
}

func TestUpdatePool(t *testing.T) {
	ctx := context.Background()

	f := &fakeCaller{}
	if err := updatePool(ctx, f, "lan", PoolUpdate{}); err != nil || len(f.sent) != 0 {
		t.Fatalf("empty update: err %v, calls %v", err, f.sent)
	}

	f = &fakeCaller{results: map[string]any{
		"GetDhcpPools": map[string]any{
			"lan":   map[string]any{poolInterface: "lan", poolOffset: int8(100), "20": map[string]any{"runtime": 1}},
			"guest": map[string]any{poolInterface: "guest", "20": map[string]any{}},
		},
		"SetDhcpPools": true,
	}}
	offset := 50
	if err := updatePool(ctx, f, "lan", PoolUpdate{Offset: &offset}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"lan":   map[string]any{poolInterface: "lan", poolOffset: int64(50)},
		"guest": map[string]any{poolInterface: "guest"},
	}
	if got := f.sent["SetDhcpPools"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %#v\nwant %#v", got, want)
	}

	if err := updatePool(ctx, f, "iot", PoolUpdate{Offset: &offset}); err == nil {
		t.Fatal("missing pool accepted")
	}
}

func TestParsePools(t *testing.T) {
	m := map[string]any{
		"lan": map[string]any{
			poolInterface: "lan",
			poolIgnore:    false,
			poolOffset:    int8(100),
			poolLength:    uint8(150),
			poolLeaseTime: uint32(86400),
			poolDHCP4Mode: int8(2),
			poolDHCP6Mode: int8(4),
			poolRAMode:    int8(9),
		},
		"guest": map[string]any{poolIgnore: true},
	}
	got := parsePools(m)
	want := []Pool{
		{Name: "guest", Ignore: true},
		{Name: "lan", Interface: "lan", Offset: 100, Length: 150, LeaseTime: 86400, DHCP4Mode: "server", DHCP6Mode: "hybrid", RAMode: "#9"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestPoolOptions(t *testing.T) {
	offset, mode, ignore := 50, "relay", true
	got, err := PoolUpdate{Offset: &offset, DHCP4Mode: &mode, Ignore: &ignore}.options()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{poolOffset: int64(50), poolDHCP4Mode: int64(3), poolIgnore: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}

	bad := "on"
	if _, err := (PoolUpdate{NDPMode: &bad}).options(); err == nil {
		t.Fatal("unknown mode should fail")
	}

	got, err = Pool{Name: "guest", Length: 20, DHCP4Mode: "server"}.update().options()
	if err != nil {
		t.Fatal(err)
	}
	want = map[string]any{poolInterface: "guest", poolIgnore: false, poolLength: int64(20), poolDHCP4Mode: int64(2)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("new pool %#v", got)
	}
}

func TestStaticLeasesRoundTrip(t *testing.T) {
	l := StaticLease{MAC: amplifi.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b}, IP: netip.MustParseAddr("192.0.2.20"), Name: "printer", Description: "upstairs"}
	mac, f, err := leaseOptions(l)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f[leaseAddress].([]byte), []byte{192, 0, 2, 20}) {
		t.Fatalf("address: %v", f[leaseAddress])
	}

	packed, err := packLeases(map[string]any{string(mac): f})
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
	got := parseStaticLeases(v.(map[string]any))
	if !reflect.DeepEqual(got, []StaticLease{l}) {
		t.Fatalf("round trip: %+v", got)
	}

	if _, _, err := leaseOptions(StaticLease{IP: netip.MustParseAddr("192.0.2.1")}); err == nil {
		t.Fatal("missing MAC should fail")
	}
	if _, _, err := leaseOptions(StaticLease{MAC: l.MAC, IP: netip.MustParseAddr("2001:db8::1")}); err == nil {
		t.Fatal("IPv6 address should fail")
	}
}

func TestParseStaticLeasesSorted(t *testing.T) {
	m := map[string]any{
		"\x00\x00\x00\x00\x00\x02": map[string]any{leaseName: "b"},
		"\x00\x00\x00\x00\x00\x01": map[string]any{leaseName: "a", leaseAddress: []byte{192, 0, 2, 1}},
		"short":                    map[string]any{leaseName: "ignored"},
	}
	got := parseStaticLeases(m)
	if len(got) != 2 || got[0].Name != "a" || got[0].IP.String() != "192.0.2.1" || got[1].MAC.String() != "00:00:00:00:00:02" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestStaticLeaseUpdateOptions(t *testing.T) {
	ip, name := netip.MustParseAddr("192.0.2.9"), "nas"
	got, err := StaticLeaseUpdate{IP: &ip, Name: &name}.options()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{leaseAddress: []byte{192, 0, 2, 9}, leaseName: "nas"}) {
		t.Fatalf("got %#v", got)
	}
}
