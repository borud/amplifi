package wire

import (
	"math"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestDecodeMsgpackKeys(t *testing.T) {
	var b []byte
	enc := func(v any) {
		p, err := msgpack.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, p...)
	}

	// A map with an int key, a string key and a binary (MAC) key whose value
	// is an array, as in GetPeerList.
	b = append(b, 0x83)
	enc(1)
	enc("AmpliFi Router HD")
	enc("x")
	enc(true)
	enc([]byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c})
	enc([]any{"router", 1})

	v, err := DecodeMsgpack(b)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", v)
	}
	if m["1"] != "AmpliFi Router HD" || m["x"] != true {
		t.Fatalf("unexpected map: %v", m)
	}
	peer, ok := m["\x00\x00\x5e\x00\x53\x0c"].([]any)
	if !ok || len(peer) != 2 || peer[0] != "router" {
		t.Fatalf("binary key not decoded: %v", m)
	}
}

func TestEncodeMsgpackRoundTrip(t *testing.T) {
	in := map[string]any{
		"5":    "ssid",
		"7":    true,
		"20":   int64(36),
		"name": []byte{192, 0, 2, 1},
		"list": []any{int64(1), "x"},
	}
	b, err := EncodeMsgpack(in)
	if err != nil {
		t.Fatal(err)
	}

	// Numeric keys are option codes and must be packed as integers.
	var raw map[any]any
	if err := msgpack.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw[int8(5)]; !ok {
		t.Fatalf("key 5 not packed as an integer: %v", raw)
	}

	out, err := DecodeMsgpack(b)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["5"] != "ssid" || m["7"] != true || string(m["name"].([]byte)) != "\xc0\x00\x02\x01" {
		t.Fatalf("round trip mismatch: %v", m)
	}
	if n, _ := ToInt64(m["20"]); n != 36 {
		t.Fatalf("int mismatch: %v", m["20"])
	}
}

func TestClusterNodeArg(t *testing.T) {
	b, err := ClusterNodeArg(map[string]any{"1": "x"})
	if err != nil {
		t.Fatal(err)
	}
	v, err := DecodeMsgpack(b)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ClusterNodePayload(v)
	if err != nil || p["1"] != "x" {
		t.Fatalf("payload = %v, %v", p, err)
	}
	if _, ok := v.([]any)[0].(map[string]any)["2"]; !ok {
		t.Fatal("missing LastUpdateTimestamp")
	}
}

func TestEncodeKey(t *testing.T) {
	tests := []struct {
		key  string
		want any
	}{
		{"12", int8(12)},
		{"-1", int8(-1)},
		{"0", int8(0)},
		{"01", "01"},
		{"+5", "+5"},
		{"-0", "-0"},
		{"1e3", "1e3"},
		{"name", "name"},
	}
	for _, tt := range tests {
		b, err := EncodeMsgpack(map[string]any{tt.key: true})
		if err != nil {
			t.Fatal(err)
		}
		var raw map[any]any
		if err := msgpack.Unmarshal(b, &raw); err != nil {
			t.Fatal(err)
		}
		if _, ok := raw[tt.want]; !ok || len(raw) != 1 {
			t.Errorf("key %q packed as %#v, want %#v", tt.key, raw, tt.want)
		}
	}
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		in   any
		want int64
		ok   bool
	}{
		{int8(-8), -8, true},
		{int16(-16), -16, true},
		{int32(-32), -32, true},
		{int64(math.MinInt64), math.MinInt64, true},
		{int(-1), -1, true},
		{uint8(8), 8, true},
		{uint16(16), 16, true},
		{uint32(math.MaxUint32), math.MaxUint32, true},
		{uint64(math.MaxInt64), math.MaxInt64, true},
		{uint64(math.MaxInt64 + 1), 0, false},
		{float64(1), 0, false},
		{"1", 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, ok := ToInt64(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ToInt64(%T %v) = %d, %v, want %d, %v", tt.in, tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestResponseFault(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		code    int
		msg     string
		ok      bool
	}{
		{"no payload", nil, 0, "", false},
		{"no error", map[string]any{"value": 1}, 0, "", false},
		{"error not an object", map[string]any{"error": "x"}, 0, "", false},
		{"full", map[string]any{"error": map[string]any{"code": float64(7), "msg": "denied"}}, 7, "denied", true},
		{"empty error", map[string]any{"error": map[string]any{}}, 0, "", true},
	}
	for _, tt := range tests {
		code, msg, ok := Response{Payload: tt.payload}.Fault()
		if code != tt.code || msg != tt.msg || ok != tt.ok {
			t.Errorf("%s: got %d, %q, %v", tt.name, code, msg, ok)
		}
	}
}

func TestResponseDecodeMsgpack(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    any
		wantErr bool
	}{
		{"no msgpack", map[string]any{}, nil, false},
		{"empty", map[string]any{"msgpack": ""}, nil, false},
		{"bad base64", map[string]any{"msgpack": "!!!"}, nil, true},
		{"truncated msgpack", map[string]any{"msgpack": "kQ=="}, nil, true},
		{"string with trailing newline", map[string]any{"msgpack": "o2FiYw==\n"}, "abc", false},
	}
	for _, tt := range tests {
		got, err := Response{Payload: tt.payload}.DecodeMsgpack()
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("%s: got %v, %v", tt.name, got, err)
		}
	}
}

func TestClusterNodePayloadErrors(t *testing.T) {
	tests := []struct {
		name string
		in   any
	}{
		{"nil", nil},
		{"not an array", map[string]any{}},
		{"too short", []any{map[string]any{}}},
		{"payload not a map", []any{map[string]any{}, "x"}},
	}
	for _, tt := range tests {
		if _, err := ClusterNodePayload(tt.in); err == nil {
			t.Errorf("%s: no error", tt.name)
		}
	}
}
