package portforward

import (
	"bytes"
	"maps"
	"net/netip"
	"reflect"
	"testing"

	"github.com/borud/amplifi/internal/wire"
)

func TestRuleRoundTrip(t *testing.T) {
	r := Rule{
		ID:      3,
		Name:    "web",
		Enabled: true,
		LANIP:   netip.MustParseAddr("192.0.2.10"),
		Mappings: []Mapping{
			{ID: 1, Protocol: TCP, WANPort: 8080, LANPort: 80},
			{ID: 2, Protocol: UDP, WANPort: 5000, LANPort: 5000, WANCount: 10, LANCount: 10},
		},
	}
	packed, err := pack(r)
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
	got := parseRules(v.(map[string]any))
	if !reflect.DeepEqual(got, []Rule{r}) {
		t.Fatalf("got  %+v\nwant %+v", got, []Rule{r})
	}

	rule := packed[0].(map[string]any)
	if !reflect.DeepEqual(rule[ruleLANIP], []byte{192, 0, 2, 10}) || rule[ruleID] != int64(3) {
		t.Fatalf("rule: %#v", rule)
	}
	single := packed[1].(map[string]any)["1"].(map[string]any)
	if _, ok := single[mapWANRange]; ok {
		t.Fatalf("single port should have no range length: %#v", single)
	}
}

func TestPackDefaults(t *testing.T) {
	packed, err := pack(Rule{LANIP: netip.MustParseAddr("192.0.2.2"), Mappings: []Mapping{
		{Protocol: TCP, WANPort: 22},
		{Protocol: UDP, WANPort: 22},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mappings := packed[1].(map[string]any)
	tcp := mappings["1"].(map[string]any)
	if tcp[mapProtocol] != int64(6) || tcp[mapLANPort] != int64(22) || tcp[mapID] != int64(1) {
		t.Fatalf("tcp: %#v", tcp)
	}
	if mappings["2"].(map[string]any)[mapProtocol] != int64(17) {
		t.Fatalf("udp: %#v", mappings["2"])
	}

	for _, bad := range []Rule{
		{LANIP: netip.MustParseAddr("2001:db8::1"), Mappings: []Mapping{{Protocol: TCP, WANPort: 1}}},
		{LANIP: netip.MustParseAddr("192.0.2.2"), Mappings: []Mapping{{Protocol: 132, WANPort: 1}}},
		{LANIP: netip.MustParseAddr("192.0.2.2"), Mappings: []Mapping{{Protocol: TCP, WANPort: 0}}},
		{LANIP: netip.MustParseAddr("192.0.2.2"), Mappings: []Mapping{{Protocol: TCP, WANPort: 1, LANPort: 70000}}},
	} {
		if _, err := pack(bad); err == nil {
			t.Errorf("pack(%+v) should fail", bad)
		}
	}
}

func TestParseRulesLANPortDefault(t *testing.T) {
	m := map[string]any{
		"7": []any{
			map[string]any{ruleName: "ssh", ruleEnabled: false, ruleLANIP: []byte{192, 0, 2, 5}},
			map[string]any{"1": map[string]any{mapProtocol: int8(6), mapWANPort: int8(22), mapLANPort: int8(-1)}},
		},
		"x": []any{},
	}
	got := parseRules(m)
	want := []Rule{{ID: 7, Name: "ssh", LANIP: netip.MustParseAddr("192.0.2.5"), Mappings: []Mapping{{ID: 1, Protocol: TCP, WANPort: 22}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestMergePartial(t *testing.T) {
	orig := []any{
		map[string]any{ruleID: int8(3), ruleName: "web", ruleEnabled: true, ruleLANIP: []byte{192, 0, 2, 5}, "9": "keep"},
		map[string]any{"1": map[string]any{mapProtocol: int8(6), mapWANPort: int16(8080), "7": uint8(1)}},
	}
	before, err := wire.EncodeMsgpack(orig)
	if err != nil {
		t.Fatal(err)
	}

	off := false
	got, err := merge(orig, RuleUpdate{Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}

	want := []any{maps.Clone(orig[0].(map[string]any)), orig[1]}
	want[0].(map[string]any)[ruleEnabled] = false
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

func TestMergeMappings(t *testing.T) {
	orig := []any{
		map[string]any{ruleName: "old"},
		map[string]any{"1": map[string]any{mapWANPort: int8(1)}},
	}

	got, err := merge(orig, RuleUpdate{Mappings: []Mapping{{Protocol: UDP, WANPort: 53}}})
	if err != nil {
		t.Fatal(err)
	}
	m := got[1].(map[string]any)["1"].(map[string]any)
	if m[mapProtocol] != int64(17) || m[mapWANPort] != int64(53) {
		t.Fatalf("mappings: %#v", got[1])
	}

	bad := netip.MustParseAddr("2001:db8::1")
	if _, err := merge(orig, RuleUpdate{LANIP: &bad}); err == nil {
		t.Fatal("bad LAN IP accepted")
	}
	if _, err := merge(orig, RuleUpdate{Mappings: []Mapping{{Protocol: 132, WANPort: 1}}}); err == nil {
		t.Fatal("bad mapping accepted")
	}
	if _, err := merge(nil, RuleUpdate{}); err == nil {
		t.Fatal("missing rule accepted")
	}
}

func TestProtocol(t *testing.T) {
	if p, err := ParseProtocol("UDP"); err != nil || p != UDP || p.String() != "udp" {
		t.Fatalf("parse: %v, %v", p, err)
	}
	if _, err := ParseProtocol("sctp"); err == nil {
		t.Fatal("unknown protocol accepted")
	}
	if Protocol(132).String() != "#132" {
		t.Fatal("unknown protocol name")
	}
}

func TestNextID(t *testing.T) {
	if n := nextID([]Rule{{ID: 4}, {ID: 2}}); n != 5 {
		t.Fatalf("next id %d", n)
	}
	if n := nextID(nil); n != 1 {
		t.Fatalf("next id %d", n)
	}
}
