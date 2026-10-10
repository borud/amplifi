package mesh

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

func TestParsePeer(t *testing.T) {
	mac := string([]byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b})
	master := []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c}
	p := parsePeer(mac, []any{"office", int8(2), []byte{192, 0, 2, 15}, uint32(3600), int8(0), int8(2), int8(-61), master})

	if p.Name != "office" || p.Role != "extender" || p.IP.String() != "192.0.2.15" ||
		p.Uptime != time.Hour || p.Band != "5GHz" || p.RSSI != -61 ||
		p.MAC.String() != "00:00:5e:00:53:1b" || p.Master.String() != "00:00:5e:00:53:0c" {
		t.Fatalf("unexpected peer: %+v", p)
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"mac":"00:00:5e:00:53:1b"`) || !strings.Contains(string(b), `"uptime":3600`) {
		t.Fatalf("unexpected JSON: %s", b)
	}
}

func TestParsePeerShort(t *testing.T) {
	p := parsePeer("\x00\x00\x00\x00\x00\x01", []any{"router", int8(1)})
	if p.Role != "router" || p.IP != nil || p.Band != defaultPeerBand || p.RSSI != defaultPeerRSSI || p.Master != nil {
		t.Fatalf("unexpected peer: %+v", p)
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "master") {
		t.Fatalf("unexpected master in JSON: %s", b)
	}
}

func TestParseNodes(t *testing.T) {
	m := map[string]any{
		"\x00\x00\x00\x00\x00\x02": []any{"office", int8(2), "AFi-P-HD"},
		"\x00\x00\x00\x00\x00\x01": []any{"router", int8(1)},
		"\x00\x00\x00\x00\x00\x03": []any{"broken"},
		"\x00\x00\x00\x00\x00\x04": []any{int8(1), int8(2)},
	}
	nodes := parseNodes(m)
	if len(nodes) != 2 {
		t.Fatalf("got %d nodes, want 2: %+v", len(nodes), nodes)
	}
	if nodes[0].Name != "router" || nodes[0].Role != "router" || nodes[0].Platform != "" {
		t.Errorf("unexpected router: %+v", nodes[0])
	}
	if nodes[1].Name != "office" || nodes[1].Role != "extender" || nodes[1].Platform != "AFi-P-HD" ||
		nodes[1].MAC.String() != "00:00:00:00:00:02" {
		t.Errorf("unexpected extender: %+v", nodes[1])
	}
}

func TestParseSignals(t *testing.T) {
	m := map[string]any{
		"2": map[string]any{
			"6": map[string]any{"\x00\x00\x00\x00\x00\x02": int8(-61)},
		},
		"1": map[string]any{
			"6": map[string]any{"\x00\x00\x00\x00\x00\x02": int8(-70)},
			"x": "ignored",
		},
	}
	sig := parseSignals(m)
	if len(sig) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(sig), sig)
	}
	if sig[0].Band != "2.4GHz" || sig[0].Network != "relay" || sig[0].Quality != -70 ||
		sig[1].Band != "5GHz" || sig[1].Quality != -61 || sig[1].MAC.String() != "00:00:00:00:00:02" {
		t.Fatalf("unexpected signals: %+v", sig)
	}
}

func TestFlattenPorts(t *testing.T) {
	var info map[string]map[string]portInfo
	raw := `{"00:00:5e:00:53:0c": {"10": {"link": false}, "2": {"link": true, "link_speed": 1000, "rx_bitrate": 12, "tx_bitrate": 34}}}`
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		t.Fatal(err)
	}
	ports, err := flattenPorts(info)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0].Port != "2" || !ports[0].Link || ports[0].LinkSpeed != 1000 ||
		ports[0].RxBitrate != 12 || ports[0].TxBitrate != 34 || ports[1].Port != "10" || ports[1].Link ||
		ports[0].Node.String() != "00:00:5e:00:53:0c" {
		t.Fatalf("unexpected ports: %+v", ports)
	}

	if _, err := flattenPorts(map[string]map[string]portInfo{"nope": {"1": {}}}); err == nil {
		t.Fatal("expected an error for a unit key that is not a MAC")
	}
}

func TestCollectMACs(t *testing.T) {
	a := []byte{0, 0, 0, 0, 0, 2}
	macs, err := collectMACs([]any{a, "x", []byte{1}})
	if err != nil || len(macs) != 1 || macs[0].String() != "00:00:00:00:00:02" {
		t.Fatalf("array: %v %v", macs, err)
	}
	macs, err = collectMACs(map[string]any{string(a): true})
	if err != nil || len(macs) != 1 {
		t.Fatalf("map: %v %v", macs, err)
	}
	if macs, err := collectMACs(nil); err != nil || len(macs) != 0 {
		t.Fatalf("nil: %v %v", macs, err)
	}
	if _, err := collectMACs("x"); err == nil {
		t.Fatal("expected error for a string")
	}
}

func TestMACArray(t *testing.T) {
	b, err := wire.EncodeMsgpack(macArray([]amplifi.MAC{{1, 2, 3, 4, 5, 6}}))
	if err != nil {
		t.Fatal(err)
	}
	// fixarray(1), bin8 len 6, bytes
	want := []byte{0x91, 0xc4, 0x06, 1, 2, 3, 4, 5, 6}
	if string(b) != string(want) {
		t.Fatalf("got % x, want % x", b, want)
	}
}
