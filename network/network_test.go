package network

import (
	"context"
	"net/netip"
	"reflect"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
)

func TestParseNetworkConfig(t *testing.T) {
	m := map[string]any{
		wanType:           int8(5),
		wanStaticAddr:     []byte{},
		wanDNS:            []any{[]byte{1, 1, 1, 1}},
		wanUsername:       "user",
		wanPassword:       "secret",
		wanUp:             true,
		wanRuntimeAddress: []byte{198, 51, 100, 3, 22},
		wanGateway:        []byte{198, 51, 100, 1},
		wanRuntimeGateway: []byte{198, 51, 100, 1},
		wanRuntimeDNS:     []byte{198, 51, 100, 2},
		wanMAC:            []byte{0x02, 0, 0, 0, 0, 1},
		wanBridgeMode:     false,
		lanGateway:        []byte{192, 0, 2, 1, 24},
		guestGateway:      []byte{192, 0, 2, 129, 26},
		iotGateway:        []byte{192, 0, 2, 193},
		wanVLAN:           uint16(102),
		wan6Protocol:      int8(4),
		hwNAT:             true,
		"99":              "ignored",
	}
	got := parseConfig(m)
	want := &Config{
		WANType:            "pppoe",
		WANGateway:         netip.MustParseAddr("198.51.100.1"),
		WANDNS:             []netip.Addr{netip.MustParseAddr("1.1.1.1")},
		WANUsername:        "user",
		WANPassword:        "secret",
		WANVLAN:            102,
		WANMAC:             amplifi.MAC{0x02, 0, 0, 0, 0, 1},
		WAN6Protocol:       "dhcpv6",
		HWNAT:              true,
		LANGateway:         netip.MustParsePrefix("192.0.2.1/24"),
		GuestGateway:       netip.MustParsePrefix("192.0.2.129/26"),
		IoTGateway:         netip.MustParsePrefix("192.0.2.193/32"),
		WANUp:              true,
		RuntimeWANAddress:  netip.MustParsePrefix("198.51.100.3/22"),
		RuntimeWANGateway:  netip.MustParseAddr("198.51.100.1"),
		RuntimeWANDNS:      []netip.Addr{netip.MustParseAddr("198.51.100.2")},
		RuntimeWAN6Address: []netip.Prefix{},
		RuntimeWAN6Gateway: []netip.Addr{},
		RuntimeWAN6DNS:     []netip.Addr{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestConfigUpdateOptions(t *testing.T) {
	typ := "static"
	addr, gw, lan := netip.MustParsePrefix("203.0.113.10/29"), netip.MustParseAddr("203.0.113.9"), netip.MustParsePrefix("198.51.100.1/24")
	dns := []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("9.9.9.9")}
	var mac amplifi.MAC
	vlan, nat := 7, false
	ch := ConfigUpdate{
		WANType:       &typ,
		WANStaticAddr: &addr,
		WANGateway:    &gw,
		WANDNS:        dns,
		WANVLAN:       &vlan,
		WANMAC:        &mac,
		HWNAT:         &nat,
		LANGateway:    &lan,
	}
	got, err := ch.options()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		wanType:       int64(2),
		wanStaticAddr: []byte{203, 0, 113, 10, 29},
		wanGateway:    []byte{203, 0, 113, 9},
		wanDNS:        []any{[]byte{1, 1, 1, 1}, []byte{9, 9, 9, 9}},
		wanVLAN:       int64(7),
		wanMAC:        []byte{},
		hwNAT:         false,
		lanGateway:    []byte{198, 51, 100, 1, 24},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}

	// The packed form round-trips through the wire decoder.
	packed, err := wire.EncodeMsgpack(got)
	if err != nil {
		t.Fatal(err)
	}
	v, err := wire.DecodeMsgpack(packed)
	if err != nil {
		t.Fatal(err)
	}
	back := parseConfig(v.(map[string]any))
	if back.WANType != "static" || back.WANStaticAddr != addr || back.WANGateway != gw ||
		back.LANGateway != lan || back.WANVLAN != 7 || !reflect.DeepEqual(back.WANDNS, dns) {
		t.Fatalf("round trip: %+v", back)
	}

	bad := "bogus"
	if _, err := (ConfigUpdate{WAN6Protocol: &bad}).options(); err == nil {
		t.Fatal("unknown protocol should fail")
	}
	if _, err := (ConfigUpdate{WANDNS: []netip.Addr{netip.MustParseAddr("2001:db8::53")}}).options(); err == nil {
		t.Fatal("IPv6 DNS should fail")
	}
	if got, err := (ConfigUpdate{WANGateway: &netip.Addr{}}).options(); err != nil || len(got[wanGateway].([]byte)) != 0 {
		t.Fatalf("zero gateway should clear: %v, %v", got, err)
	}
	off := "off"
	if got, err := (ConfigUpdate{WAN6Protocol: &off}).options(); err != nil || got[wan6Protocol] != int64(1) {
		t.Fatalf("off: %v, %v", got, err)
	}
}

func TestParseNetworkInterfaces(t *testing.T) {
	m := map[string]any{
		"wan": map[string]any{
			ifDevice:         "eth0",
			ifEnabled:        true,
			ifProtocol:       int8(3),
			ifUp:             true,
			ifRuntimeAddress: []byte{198, 51, 100, 3, 22},
			ifRuntimeDNS:     []any{[]byte{198, 51, 100, 2}},
		},
		"lan": map[string]any{
			ifDevice:   "br-lan",
			ifProtocol: int8(2),
			ifAddress:  []byte{192, 0, 2, 1, 24},
		},
		"junk": "not a map",
	}
	got := parseInterfaces(m)
	if len(got) != 2 || got[0].Name != "lan" || got[1].Name != "wan" {
		t.Fatalf("unexpected interfaces: %+v", got)
	}
	if got[0].Address.String() != "192.0.2.1/24" || got[0].Protocol != "static" || got[0].Device != "br-lan" {
		t.Fatalf("lan: %+v", got[0])
	}
	if got[1].Protocol != "dhcp" || !got[1].Up || got[1].RuntimeAddress.String() != "198.51.100.3/22" || got[1].RuntimeDNS[0].String() != "198.51.100.2" {
		t.Fatalf("wan: %+v", got[1])
	}

	enabled, addr := false, netip.MustParsePrefix("192.0.2.1/8")
	opts, err := InterfaceUpdate{Enabled: &enabled, Address: &addr}.options()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts, map[string]any{ifEnabled: false, ifAddress: []byte{192, 0, 2, 1, 8}}) {
		t.Fatalf("options: %#v", opts)
	}
}

func TestRedacted(t *testing.T) {
	cfg := Config{WANPassword: "secret"}
	if r := cfg.Redacted(); r.WANPassword != rpc.Redacted || cfg.WANPassword != "secret" {
		t.Fatalf("config: %q, original %q", r.WANPassword, cfg.WANPassword)
	}
	if r := (Config{}).Redacted(); r.WANPassword != "" {
		t.Fatalf("empty password masked: %q", r.WANPassword)
	}
	if r := (Interface{Password: "secret"}).Redacted(); r.Password != rpc.Redacted {
		t.Fatalf("interface: %q", r.Password)
	}
}

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

func TestUpdateInterface(t *testing.T) {
	ctx := context.Background()

	f := &fakeCaller{}
	if err := updateInterface(ctx, f, "lan", InterfaceUpdate{}); err != nil || len(f.sent) != 0 {
		t.Fatalf("empty update: err %v, calls %v", err, f.sent)
	}

	f = &fakeCaller{results: map[string]any{
		"GetNetworkInterfaces": map[string]any{
			"lan": map[string]any{ifDevice: "br-lan", ifEnabled: true, "13": map[string]any{"runtime": 1}},
			"wan": map[string]any{ifDevice: "eth0", "13": map[string]any{"runtime": 2}},
		},
		"SetNetworkInterfaces": true,
	}}
	off := false
	if err := updateInterface(ctx, f, "lan", InterfaceUpdate{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"lan": map[string]any{ifDevice: "br-lan", ifEnabled: false},
		"wan": map[string]any{ifDevice: "eth0"},
	}
	if got := f.sent["SetNetworkInterfaces"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("sent %#v\nwant %#v", got, want)
	}
}
