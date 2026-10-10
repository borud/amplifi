package rpc

import (
	"maps"
	"testing"

	"github.com/borud/amplifi"
)

func TestCodeName(t *testing.T) {
	tests := []struct {
		code int64
		want string
	}{
		{1, "2.4GHz"},
		{2, "5GHz"},
		{9, "#9"},
	}
	for _, tt := range tests {
		if got := CodeName(BandNames, tt.code); got != tt.want {
			t.Errorf("CodeName(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestName(t *testing.T) {
	m := map[string]any{"15": int8(2), "16": int8(7)}
	tests := []struct {
		key  string
		want string
	}{
		{"15", "5GHz"},
		{"16", "#7"},
		{"17", ""},
	}
	for _, tt := range tests {
		if got := Name(BandNames, m, tt.key); got != tt.want {
			t.Errorf("Name(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestRole(t *testing.T) {
	tests := []struct {
		code int64
		want amplifi.Role
	}{
		{1, amplifi.RoleRouter},
		{2, amplifi.RoleMeshPoint},
		{3, "#3"},
	}
	for _, tt := range tests {
		if got := Role(tt.code); got != tt.want {
			t.Errorf("Role(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestCode(t *testing.T) {
	tests := []struct {
		name    string
		want    int64
		wantErr bool
	}{
		{"Guest", 4, false},
		{"iot", 8, false},
		{"#12", 12, false},
		{"#x", 0, true},
		{"nope", 0, true},
	}
	for _, tt := range tests {
		got, err := Code("network", NetworkNames, tt.name)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("Code(%q) = %d, %v; want %d, error %t", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestCompareNumeric(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"10", "9", 1},
		{"9", "10", -1},
		{"2", "eth-0", -1},
		{"eth-0", "2", 1},
		{"a", "b", -1},
		{"7", "7", 0},
	}
	for _, tt := range tests {
		if got := CompareNumeric(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareNumeric(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestMAC(t *testing.T) {
	tests := []struct {
		in   any
		want string
	}{
		{"\x00\x00\x5e\x00\x53\x0c", "00:00:5e:00:53:0c"},
		{[]byte{0, 1, 2, 3, 4, 5}, "00:01:02:03:04:05"},
		{[]byte{1}, ""},
		{"00:00:5e:00:53:0c", ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := MAC(tt.in).String(); got != tt.want {
			t.Errorf("MAC(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

type testEnum int

func (testEnum) MarshalText() ([]byte, error) { return []byte("text"), nil }

func TestPut(t *testing.T) {
	s, n, e := "x", 3, testEnum(5)
	m := map[string]any{}
	Put(m, "1", &s)
	Put(m, "2", (*string)(nil))
	Put(m, "", &s)
	PutInt(m, "3", &n)
	PutInt(m, "4", &e)
	PutInt(m, "5", (*int)(nil))

	want := map[string]any{"1": "x", "3": int64(3), "4": int64(5)}
	if !maps.Equal(m, want) {
		t.Fatalf("got %#v, want %#v", m, want)
	}
}

func TestMask(t *testing.T) {
	if Mask("secret") != Redacted || Mask("") != "" {
		t.Fatal("Mask")
	}

	orig := map[string]any{"7": "secret", "8": "", "9": "keep"}
	got := MaskFields(orig, "7", "8", "10")
	want := map[string]any{"7": Redacted, "8": "", "9": "keep"}
	if !maps.Equal(got, want) || orig["7"] != "secret" {
		t.Fatalf("got %v (original %v)", got, orig)
	}
	if got := MaskFields(nil, "7"); got == nil || len(got) != 0 {
		t.Fatalf("nil: %#v", got)
	}
}

func TestSubmaps(t *testing.T) {
	v := map[string]any{"a": map[string]any{"x": 1}, "b": "scalar", "c": map[string]any{}}
	got := map[string]int{}
	for k, sub := range Submaps(v) {
		got[k] = len(sub)
	}
	if !maps.Equal(got, map[string]int{"a": 1, "c": 0}) {
		t.Fatalf("got %v", got)
	}
	for k := range Submaps("not a map") {
		t.Fatalf("yielded %q for a non-map", k)
	}
}
