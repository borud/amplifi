package amplifi

import (
	"fmt"
	"net"
)

// MAC is a hardware address. Unlike net.HardwareAddr it encodes as the usual
// colon-separated string in JSON and other text formats.
type MAC net.HardwareAddr

// ParseMAC parses a colon-, dash- or dot-separated 6 byte MAC address.
func ParseMAC(s string) (MAC, error) {
	hw, err := net.ParseMAC(s)
	if err != nil {
		return nil, fmt.Errorf("invalid MAC %q: %w", s, err)
	}
	if len(hw) != 6 {
		return nil, fmt.Errorf("invalid MAC %q: %d bytes, want 6", s, len(hw))
	}
	return MAC(hw), nil
}

// String formats m as aa:bb:cc:dd:ee:ff, or "" if it is empty.
func (m MAC) String() string { return net.HardwareAddr(m).String() }

// MarshalText implements encoding.TextMarshaler.
func (m MAC) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler. Empty text yields an
// empty MAC.
func (m *MAC) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*m = nil
		return nil
	}
	parsed, err := ParseMAC(string(text))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// Equal reports whether m and o are the same address.
func (m MAC) Equal(o MAC) bool { return string(m) == string(o) }
