// Package discovery finds AmpliFi units on the local network using the
// Ubiquiti UDP discovery protocol on port 10001.
package discovery

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"time"

	"github.com/borud/amplifi"
)

// Port is the UDP port the Ubiquiti discovery service listens on.
const Port = 10001

// probe is the discovery request payload. It is a version-1 request with an
// empty command; units reply with a TLV-encoded description of themselves.
var probe = []byte{0x01, 0x00, 0x00, 0x00}

// Unit is a unit that answered a discovery probe.
type Unit struct {
	IP       netip.Addr
	MAC      amplifi.MAC
	HostName string
	Model    string
	Firmware string

	// Strings are the printable string TLVs without a named field.
	Strings []string
}

// String describes the unit on one line.
func (u Unit) String() string {
	return fmt.Sprintf("%s %s (%s, %s, %s)", u.HostName, u.IP, u.Model, u.Firmware, u.MAC)
}

// Scan broadcasts a probe and collects replies until ctx is done, returning
// the units sorted by IP. Give ctx a deadline; a scan without one runs until
// ctx is cancelled.
func Scan(ctx context.Context) ([]Unit, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}
	defer conn.Close()
	defer interruptReads(ctx, conn)()

	dst := netip.AddrPortFrom(netip.AddrFrom4([4]byte{255, 255, 255, 255}), Port)
	if _, err := conn.WriteToUDPAddrPort(probe, dst); err != nil {
		return nil, fmt.Errorf("send probe: %w", err)
	}

	seen := map[netip.Addr]Unit{}
	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if ctx.Err() != nil {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read reply: %w", err)
		}
		u := parseReply(buf[:n], from.Addr().Unmap())
		seen[u.IP] = u
	}

	units := make([]Unit, 0, len(seen))
	for _, u := range seen {
		units = append(units, u)
	}
	slices.SortFunc(units, func(a, b Unit) int { return a.IP.Compare(b.IP) })
	return units, nil
}

// Probe sends a probe directly to host and returns its reply, giving up when
// ctx is done. It is a quick way to learn the name and MAC of a unit whose
// address is already known. Units answer from an ephemeral port rather than
// 10001, so any reply from host's address is accepted.
func Probe(ctx context.Context, host string) (*Unit, error) {
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(host, strconv.Itoa(Port)))
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	dst := addr.AddrPort()
	ip := dst.Addr().Unmap()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}
	defer conn.Close()
	defer interruptReads(ctx, conn)()

	if _, err := conn.WriteToUDPAddrPort(probe, dst); err != nil {
		return nil, fmt.Errorf("send probe: %w", err)
	}

	buf := make([]byte, 4096)
	for {
		n, from, err := conn.ReadFromUDPAddrPort(buf)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("no reply from %q: %w", host, ctx.Err())
		}
		if err != nil {
			return nil, fmt.Errorf("read reply from %q: %w", host, err)
		}
		if from.Addr().Unmap() != ip {
			continue
		}
		u := parseReply(buf[:n], ip)
		return &u, nil
	}
}

// interruptReads makes pending and future reads on conn return once ctx is
// done. The returned function stops watching ctx.
func interruptReads(ctx context.Context, conn *net.UDPConn) func() bool {
	return context.AfterFunc(ctx, func() {
		// This only fails on a closed conn, whose reads have returned anyway.
		_ = conn.SetReadDeadline(time.Now())
	})
}

// parseReply decodes the TLV body of a discovery reply received from the
// address from, which is the unit's IP unless the reply names one. The format
// is a 4-byte header followed by entries of: type(1) length(2, big-endian)
// value(length).
func parseReply(b []byte, from netip.Addr) Unit {
	u := Unit{IP: from, Strings: []string{}}
	if len(b) < 4 {
		return u
	}

	for i := 4; i+3 <= len(b); {
		t := b[i]
		l := int(b[i+1])<<8 | int(b[i+2])
		i += 3
		if i+l > len(b) {
			break
		}
		v := b[i : i+l]
		i += l
		applyTLV(&u, t, v)
	}
	return u
}

func applyTLV(u *Unit, t byte, v []byte) {
	switch t {
	case 0x01:
		if len(v) == 6 {
			u.MAC = amplifi.MAC(append([]byte(nil), v...))
		}
	case 0x02:
		if len(v) == 10 {
			u.MAC = amplifi.MAC(append([]byte(nil), v[:6]...))
			u.IP = netip.AddrFrom4([4]byte(v[6:10]))
		}
	case 0x03:
		u.Firmware = string(v)
	case 0x0b:
		u.HostName = string(v)
	case 0x0c:
		u.Model = string(v)
	default:
		if s := printable(v); s != "" {
			u.Strings = append(u.Strings, s)
		}
	}
}

// printable returns v as a string if it looks like printable text.
func printable(v []byte) string {
	if len(v) == 0 {
		return ""
	}
	for _, c := range v {
		if c < 0x20 || c > 0x7e {
			return ""
		}
	}
	return string(v)
}
