package rpc

import (
	"fmt"
	"net/netip"

	"github.com/borud/amplifi"
)

// Addr decodes a packed address: 4 or 16 bytes, or 5 or 17 bytes with a
// trailing prefix length, which is dropped. Anything else gives the zero
// Addr.
func Addr(v any) netip.Addr {
	return Prefix(v).Addr()
}

// Prefix decodes a packed address with a prefix length: 5 or 17 bytes, the
// address followed by the length. A bare 4 or 16 byte address gets a
// full-length prefix. Anything else gives the zero Prefix.
func Prefix(v any) netip.Prefix {
	b, _ := v.([]byte)

	bits := -1
	if len(b) == 5 || len(b) == 17 {
		bits = int(b[len(b)-1])
		b = b[:len(b)-1]
	}
	if len(b) != 4 && len(b) != 16 {
		return netip.Prefix{}
	}

	a, _ := netip.AddrFromSlice(b)
	a = a.Unmap()
	if bits < 0 {
		bits = a.BitLen()
	}
	return netip.PrefixFrom(a, bits)
}

// Addrs decodes a list of packed addresses, skipping invalid ones. A single
// binary value is accepted as a list of one.
func Addrs(v any) []netip.Addr {
	out := []netip.Addr{}
	for _, p := range Prefixes(v) {
		out = append(out, p.Addr())
	}
	return out
}

// Prefixes is Addrs for addresses with a prefix length, as Prefix decodes
// them.
func Prefixes(v any) []netip.Prefix {
	list, ok := v.([]any)
	if !ok {
		list = []any{v}
	}

	out := []netip.Prefix{}
	for _, item := range list {
		if p := Prefix(item); p.IsValid() {
			out = append(out, p)
		}
	}
	return out
}

// PackAddr packs an IPv4 address as 4 bytes. The zero Addr packs as empty
// binary, which clears the option.
func PackAddr(a netip.Addr) ([]byte, error) {
	if !a.IsValid() {
		return []byte{}, nil
	}
	if !a.Unmap().Is4() {
		return nil, fmt.Errorf("invalid IPv4 address %q", a)
	}
	b := a.Unmap().As4()
	return b[:], nil
}

// PackPrefix packs an IPv4 address and prefix length as 5 bytes: the
// address followed by the length. The zero Prefix packs as empty binary.
func PackPrefix(p netip.Prefix) ([]byte, error) {
	if !p.IsValid() {
		return []byte{}, nil
	}
	if !p.Addr().Unmap().Is4() {
		return nil, fmt.Errorf("invalid IPv4 address/prefix %q", p)
	}
	b := p.Addr().Unmap().As4()
	return append(b[:], byte(p.Bits())), nil
}

// PackMAC packs a MAC as 6 byte binary. An empty MAC packs as empty binary,
// which clears a MAC override.
func PackMAC(m amplifi.MAC) ([]byte, error) {
	if len(m) == 0 {
		return []byte{}, nil
	}
	if len(m) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q", m.String())
	}
	return []byte(m), nil
}
