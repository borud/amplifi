package network

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
)

func TestNetPackIP(t *testing.T) {
	b, err := packIP("192.168.1.254")
	if err != nil || !bytes.Equal(b, []byte{192, 168, 1, 254}) {
		t.Fatalf("got %v, %v", b, err)
	}

	b, err = packCIDR("192.0.2.1/24")
	if err != nil || !bytes.Equal(b, []byte{192, 0, 2, 1, 24}) {
		t.Fatalf("got %v, %v", b, err)
	}

	b, err = packMAC(amplifi.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b})
	if err != nil || !bytes.Equal(b, []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b}) {
		t.Fatalf("got %v, %v", b, err)
	}

	if b, err := packIP(""); err != nil || b == nil || len(b) != 0 {
		t.Fatalf("empty: got %v, %v", b, err)
	}

	for _, bad := range []string{"192.0.2", "::1", "192.0.2.256", "x"} {
		if _, err := packIP(bad); err == nil {
			t.Errorf("packIP(%q) should fail", bad)
		}
	}
	for _, bad := range []string{"192.0.2.1", "192.0.2.1/33", "::1/64"} {
		if _, err := packCIDR(bad); err == nil {
			t.Errorf("packCIDR(%q) should fail", bad)
		}
	}
	if _, err := packMAC(amplifi.MAC{0x74, 0x83, 0xc2}); err == nil {
		t.Error("short MAC should fail")
	}
}

func TestNetAddr(t *testing.T) {
	v6 := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	cases := []struct {
		in   any
		want string
	}{
		{[]byte{192, 0, 2, 1}, "192.0.2.1"},
		{[]byte{192, 168, 128, 1, 17}, "192.168.128.1/17"},
		{v6, "2001:db8::1"},
		{append(append([]byte{}, v6...), 64), "2001:db8::1/64"},
		{[]byte{1, 2}, "0102"},
		{[]byte{}, ""},
		{nil, ""},
		{"192.0.2.1", ""},
	}
	for _, c := range cases {
		if got := formatAddr(c.in); got != c.want {
			t.Errorf("formatAddr(%v) = %q, want %q", c.in, got, c.want)
		}
	}

	got := formatAddrs([]any{[]byte{1, 1, 1, 1}, []byte{8, 8, 8, 8}})
	if !reflect.DeepEqual(got, []string{"1.1.1.1", "8.8.8.8"}) {
		t.Fatalf("formatAddrs list: %v", got)
	}
	if got := formatAddrs([]byte{9, 9, 9, 9}); !reflect.DeepEqual(got, []string{"9.9.9.9"}) {
		t.Fatalf("formatAddrs single: %v", got)
	}
	if got := formatAddrs(nil); got == nil || len(got) != 0 {
		t.Fatalf("formatAddrs nil: %#v", got)
	}
}

func TestParseNetworkConfig(t *testing.T) {
	m := map[string]any{
		wanType:           int8(5),
		wanStaticAddr:     []byte{},
		wanDNS:            []any{[]byte{1, 1, 1, 1}},
		wanUsername:       "user",
		wanPassword:       "secret",
		wanUp:             true,
		wanRuntimeAddress: []byte{198, 51, 100, 3, 22},
		wanRuntimeGateway: []byte{198, 51, 100, 1},
		wanRuntimeDNS:     []byte{198, 51, 100, 2},
		wanMAC:            []byte{0x02, 0, 0, 0, 0, 1},
		wanBridgeMode:     false,
		lanGateway:        []byte{192, 168, 1, 1, 24},
		guestGateway:      []byte{192, 168, 2, 1, 24},
		iotGateway:        []byte{192, 168, 3, 1, 24},
		wanVLAN:           uint16(102),
		wan6Protocol:      int8(4),
		hwNAT:             true,
		"99":              "ignored",
	}
	got := parseConfig(m)
	want := &Configuration{
		WANType:            "pppoe",
		WANDNS:             []string{"1.1.1.1"},
		WANUsername:        "user",
		WANPassword:        "secret",
		WANVLAN:            102,
		WANMAC:             amplifi.MAC{0x02, 0, 0, 0, 0, 1},
		WAN6Protocol:       "dhcpv6",
		HWNAT:              true,
		LANGateway:         "192.168.1.1/24",
		GuestGateway:       "192.168.2.1/24",
		IoTGateway:         "192.168.3.1/24",
		WANUp:              true,
		RuntimeWANAddress:  "198.51.100.3/22",
		RuntimeWANGateway:  "198.51.100.1",
		RuntimeWANDNS:      []string{"198.51.100.2"},
		RuntimeWAN6Address: []string{},
		RuntimeWAN6Gateway: []string{},
		RuntimeWAN6DNS:     []string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
}

func TestConfigUpdateOptions(t *testing.T) {
	typ, addr, gw, lan := "static", "203.0.113.10/29", "203.0.113.9", "198.51.100.1/24"
	var mac amplifi.MAC
	vlan, nat := 7, false
	ch := ConfigUpdate{
		WANType:       &typ,
		WANStaticAddr: &addr,
		WANGateway:    &gw,
		WANDNS:        []string{"1.1.1.1", "9.9.9.9"},
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
		back.LANGateway != lan || back.WANVLAN != 7 || !reflect.DeepEqual(back.WANDNS, []string{"1.1.1.1", "9.9.9.9"}) {
		t.Fatalf("round trip: %+v", back)
	}

	bad := "bogus"
	if _, err := (ConfigUpdate{WAN6Protocol: &bad}).options(); err == nil {
		t.Fatal("unknown protocol should fail")
	}
	if _, err := (ConfigUpdate{WANDNS: []string{"1.1.1.1", "nope"}}).options(); err == nil {
		t.Fatal("bad DNS should fail")
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
			ifAddress:  []byte{192, 168, 1, 1, 24},
		},
		"junk": "not a map",
	}
	got := parseInterfaces(m)
	if len(got) != 2 || got[0].Name != "lan" || got[1].Name != "wan" {
		t.Fatalf("unexpected interfaces: %+v", got)
	}
	if got[0].Address != "192.168.1.1/24" || got[0].Protocol != "static" || got[0].Device != "br-lan" {
		t.Fatalf("lan: %+v", got[0])
	}
	if got[1].Protocol != "dhcp" || !got[1].Up || got[1].RuntimeAddress != "198.51.100.3/22" || got[1].RuntimeDNS[0] != "198.51.100.2" {
		t.Fatalf("wan: %+v", got[1])
	}

	enabled, addr := false, "192.0.2.1/8"
	opts, err := InterfaceUpdate{Enabled: &enabled, Address: &addr}.options()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(opts, map[string]any{ifEnabled: false, ifAddress: []byte{192, 0, 2, 1, 8}}) {
		t.Fatalf("options: %#v", opts)
	}
}

func TestRedacted(t *testing.T) {
	cfg := Configuration{WANPassword: "secret"}
	if r := cfg.Redacted(); r.WANPassword != rpc.Redacted || cfg.WANPassword != "secret" {
		t.Fatalf("config: %q, original %q", r.WANPassword, cfg.WANPassword)
	}
	if r := (Configuration{}).Redacted(); r.WANPassword != "" {
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
