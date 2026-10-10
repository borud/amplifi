package network

import (
	"bytes"
	"maps"
	"reflect"
	"testing"

	"github.com/borud/amplifi/internal/wire"
)

func TestPortForwardRoundTrip(t *testing.T) {
	r := PortForward{
		ID:      3,
		Name:    "web",
		Enabled: true,
		LANIP:   "192.168.1.10",
		Mappings: []PortMapping{
			{ID: 1, Protocol: "tcp", WANPort: 8080, LANPort: 80},
			{ID: 2, Protocol: "udp", WANPort: 5000, LANPort: 5000, WANCount: 10, LANCount: 10},
		},
	}
	packed, err := portfwdPack(r)
	if err != nil {
		t.Fatal(err)
	}

	b, err := wire.EncodeMsgpack(map[string]any{"3": packed})
	if err != nil {
		t.Fatal(err)
	}
	v, err := wire.DecodeMsgpack(b)
	if err != nil {
		t.Fatal(err)
	}
	got := parsePortForwards(v.(map[string]any))
	if !reflect.DeepEqual(got, []PortForward{r}) {
		t.Fatalf("got  %+v\nwant %+v", got, []PortForward{r})
	}

	rule := packed[0].(map[string]any)
	if !reflect.DeepEqual(rule[portfwdLANIP], []byte{192, 168, 1, 10}) || rule[portfwdRuleID] != int64(3) {
		t.Fatalf("rule: %#v", rule)
	}
	single := packed[1].(map[string]any)["1"].(map[string]any)
	if _, ok := single[portfwdMapWANRange]; ok {
		t.Fatalf("single port should have no range length: %#v", single)
	}
}

func TestPortForwardPackDefaults(t *testing.T) {
	packed, err := portfwdPack(PortForward{LANIP: "192.0.2.2", Mappings: []PortMapping{
		{Protocol: "tcp", WANPort: 22},
		{Protocol: "udp", WANPort: 22},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mappings := packed[1].(map[string]any)
	tcp := mappings["1"].(map[string]any)
	if tcp[portfwdMapProtocol] != int64(6) || tcp[portfwdMapLANPort] != int64(22) || tcp[portfwdMapID] != int64(1) {
		t.Fatalf("tcp: %#v", tcp)
	}
	if mappings["2"].(map[string]any)[portfwdMapProtocol] != int64(17) {
		t.Fatalf("udp: %#v", mappings["2"])
	}

	for _, bad := range []PortForward{
		{LANIP: "192.0.2", Mappings: []PortMapping{{Protocol: "tcp", WANPort: 1}}},
		{LANIP: "192.0.2.2", Mappings: []PortMapping{{Protocol: "sctp", WANPort: 1}}},
		{LANIP: "192.0.2.2", Mappings: []PortMapping{{Protocol: "tcp", WANPort: 0}}},
		{LANIP: "192.0.2.2", Mappings: []PortMapping{{Protocol: "tcp", WANPort: 1, LANPort: 70000}}},
	} {
		if _, err := portfwdPack(bad); err == nil {
			t.Errorf("portfwdPack(%+v) should fail", bad)
		}
	}
}

func TestParsePortForwardsLANPortDefault(t *testing.T) {
	m := map[string]any{
		"7": []any{
			map[string]any{portfwdName: "ssh", portfwdEnabled: false, portfwdLANIP: []byte{192, 0, 2, 5}},
			map[string]any{"1": map[string]any{portfwdMapProtocol: int8(6), portfwdMapWANPort: int8(22), portfwdMapLANPort: int8(-1)}},
		},
		"x": []any{},
	}
	got := parsePortForwards(m)
	want := []PortForward{{ID: 7, Name: "ssh", LANIP: "192.0.2.5", Mappings: []PortMapping{{ID: 1, Protocol: "tcp", WANPort: 22}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestPortForwardMergePartial(t *testing.T) {
	orig := []any{
		map[string]any{portfwdRuleID: int8(3), portfwdName: "web", portfwdEnabled: true, portfwdLANIP: []byte{192, 0, 2, 5}, "9": "keep"},
		map[string]any{"1": map[string]any{portfwdMapProtocol: int8(6), portfwdMapWANPort: int16(8080), "7": uint8(1)}},
	}
	before, err := wire.EncodeMsgpack(orig)
	if err != nil {
		t.Fatal(err)
	}

	off := false
	got, err := portfwdMerge(orig, PortForwardUpdate{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}

	want := []any{maps.Clone(orig[0].(map[string]any)), orig[1]}
	want[0].(map[string]any)[portfwdEnabled] = false
	gotBytes, err := wire.EncodeMsgpack(got)
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := wire.EncodeMsgpack(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}

	after, err := wire.EncodeMsgpack(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("merge modified the original rule")
	}
}

func TestPortForwardMergeMappings(t *testing.T) {
	orig := []any{
		map[string]any{portfwdName: "old"},
		map[string]any{"1": map[string]any{portfwdMapWANPort: int8(1)}},
	}

	got, err := portfwdMerge(orig, PortForwardUpdate{Mappings: []PortMapping{{Protocol: "udp", WANPort: 53}}})
	if err != nil {
		t.Fatal(err)
	}
	m := got[1].(map[string]any)["1"].(map[string]any)
	if m[portfwdMapProtocol] != int64(17) || m[portfwdMapWANPort] != int64(53) {
		t.Fatalf("mappings: %#v", got[1])
	}

	bad := "192.0.2"
	if _, err := portfwdMerge(orig, PortForwardUpdate{LANIP: &bad}); err == nil {
		t.Fatal("bad LAN IP accepted")
	}
	if _, err := portfwdMerge(orig, PortForwardUpdate{Mappings: []PortMapping{{Protocol: "sctp", WANPort: 1}}}); err == nil {
		t.Fatal("bad mapping accepted")
	}
	if _, err := portfwdMerge(nil, PortForwardUpdate{}); err == nil {
		t.Fatal("missing rule accepted")
	}
}

func TestPortForwardNextID(t *testing.T) {
	if n := portfwdNextID([]PortForward{{ID: 4}, {ID: 2}}); n != 5 {
		t.Fatalf("next id %d", n)
	}
	if n := portfwdNextID(nil); n != 1 {
		t.Fatalf("next id %d", n)
	}
}
