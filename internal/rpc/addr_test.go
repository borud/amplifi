package rpc

import (
	"bytes"
	"net/netip"
	"slices"
	"testing"

	"github.com/borud/amplifi"
)

func TestPrefix(t *testing.T) {
	v6 := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	mapped := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 192, 0, 2, 1}
	tests := []struct {
		in   any
		want string
	}{
		{[]byte{192, 0, 2, 1}, "192.0.2.1/32"},
		{[]byte{192, 0, 2, 1, 24}, "192.0.2.1/24"},
		{v6, "2001:db8::1/128"},
		{append(slices.Clone(v6), 64), "2001:db8::1/64"},
		{mapped, "192.0.2.1/32"},
		{[]byte{1, 2}, "invalid Prefix"},
		{[]byte{}, "invalid Prefix"},
		{nil, "invalid Prefix"},
		{"192.0.2.1", "invalid Prefix"},
	}
	for _, tt := range tests {
		if got := Prefix(tt.in).String(); got != tt.want {
			t.Errorf("Prefix(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}

	if got := Addr([]byte{192, 0, 2, 1, 24}); got != netip.MustParseAddr("192.0.2.1") {
		t.Errorf("Addr with prefix = %v", got)
	}
	if got := Addr(nil); got.IsValid() {
		t.Errorf("Addr(nil) = %v", got)
	}
}

func TestAddrs(t *testing.T) {
	got := Addrs([]any{[]byte{192, 0, 2, 1}, "x", []byte{192, 0, 2, 2}})
	want := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")}
	if !slices.Equal(got, want) {
		t.Fatalf("list: %v", got)
	}
	if got := Addrs([]byte{192, 0, 2, 9}); len(got) != 1 || got[0].String() != "192.0.2.9" {
		t.Fatalf("single: %v", got)
	}
	if got := Addrs(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil: %#v", got)
	}
	if got := Prefixes([]any{[]byte{192, 0, 2, 1, 24}}); len(got) != 1 || got[0].String() != "192.0.2.1/24" {
		t.Fatalf("prefixes: %v", got)
	}
}

func TestPack(t *testing.T) {
	b, err := PackAddr(netip.MustParseAddr("192.0.2.254"))
	if err != nil || !bytes.Equal(b, []byte{192, 0, 2, 254}) {
		t.Fatalf("addr: %v, %v", b, err)
	}
	b, err = PackPrefix(netip.MustParsePrefix("192.0.2.1/24"))
	if err != nil || !bytes.Equal(b, []byte{192, 0, 2, 1, 24}) {
		t.Fatalf("prefix: %v, %v", b, err)
	}
	b, err = PackMAC(amplifi.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b})
	if err != nil || !bytes.Equal(b, []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x1b}) {
		t.Fatalf("mac: %v, %v", b, err)
	}

	for _, empty := range [][]byte{must(PackAddr(netip.Addr{})), must(PackPrefix(netip.Prefix{})), must(PackMAC(nil))} {
		if empty == nil || len(empty) != 0 {
			t.Fatalf("zero value packed as %#v", empty)
		}
	}

	if _, err := PackAddr(netip.MustParseAddr("2001:db8::1")); err == nil {
		t.Error("IPv6 address accepted")
	}
	if _, err := PackPrefix(netip.MustParsePrefix("2001:db8::/64")); err == nil {
		t.Error("IPv6 prefix accepted")
	}
	if _, err := PackMAC(amplifi.MAC{0x00, 0x00, 0x5e}); err == nil {
		t.Error("short MAC accepted")
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
