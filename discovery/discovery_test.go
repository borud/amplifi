package discovery

import (
	"net/netip"
	"testing"
)

func TestParseReply(t *testing.T) {
	reply := []byte{
		0x01, 0x00, 0x00, 0x00,
		0x02, 0x00, 0x0a, 0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c, 192, 0, 2, 10,
		0x0b, 0x00, 0x06, 'r', 'o', 'u', 't', 'e', 'r',
		0x0c, 0x00, 0x03, 'A', 'F', 'i',
	}
	from := netip.MustParseAddr("192.0.2.99")

	u := parseReply(reply, from)
	if u.IP != netip.MustParseAddr("192.0.2.10") {
		t.Fatalf("IP = %v, want the address in the reply", u.IP)
	}
	if u.HostName != "router" || u.Model != "AFi" || u.MAC.String() != "00:00:5e:00:53:0c" {
		t.Fatalf("got %+v", u)
	}

	if u := parseReply(reply[:4], from); u.IP != from {
		t.Fatalf("IP = %v, want the sender's address", u.IP)
	}
}
