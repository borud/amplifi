package rpc

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

// Code tables shared by several areas. Option maps and enums use integer
// codes, keyed here by their decimal string as in decoded option maps.
var (
	// BandNames maps WifiBand codes.
	BandNames = map[string]string{"1": "2.4GHz", "2": "5GHz"}

	// NetworkNames maps WifiInterfaceRole codes.
	NetworkNames = map[string]string{
		"1": "setup", "2": "internal", "3": "main", "4": "guest",
		"5": "device", "6": "relay", "7": "friend", "8": "iot",
	}

	// ClientNetworks are the networks that carry clients; the others are
	// mesh backhaul and setup networks.
	ClientNetworks = map[string]bool{"3": true, "4": true, "5": true, "7": true, "8": true}
)

// roles maps NodeRole codes.
var roles = map[int64]amplifi.Role{1: amplifi.RoleRouter, 2: amplifi.RoleMeshPoint}

// Role converts a NodeRole code. Unknown codes give "#N".
func Role(code int64) amplifi.Role {
	if r, ok := roles[code]; ok {
		return r
	}
	return amplifi.Role("#" + strconv.FormatInt(code, 10))
}

// Redacted replaces secrets in redacted output.
const Redacted = "********"

// String returns m[key] as a string, or "" if it is missing or not a string.
func String(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// Int returns m[key] as an integer, or 0 if it is missing or not one.
func Int(m map[string]any, key string) int64 {
	n, _ := wire.ToInt64(m[key])
	return n
}

// Bool returns m[key] as a bool, or false if it is missing or not one.
func Bool(m map[string]any, key string) bool {
	b, _ := m[key].(bool)
	return b
}

// Bytes returns m[key] as bytes, or nil if it is missing or not binary.
func Bytes(m map[string]any, key string) []byte {
	b, _ := m[key].([]byte)
	return b
}

// MAC converts a binary MAC, as a []byte value or a raw-byte map key, to a
// MAC. Anything that is not 6 bytes yields nil.
func MAC(v any) amplifi.MAC {
	var b []byte
	switch t := v.(type) {
	case []byte:
		b = t
	case string:
		b = []byte(t)
	}
	if len(b) != 6 {
		return nil
	}
	return amplifi.MAC(append([]byte(nil), b...))
}

// Submaps yields the entries of v, a map, whose values are maps. Anything
// else in v, or a v that is not a map, is skipped.
func Submaps(v any) iter.Seq2[string, map[string]any] {
	m, _ := v.(map[string]any)
	return func(yield func(string, map[string]any) bool) {
		for k, e := range m {
			sub, ok := e.(map[string]any)
			if !ok {
				continue
			}
			if !yield(k, sub) {
				return
			}
		}
	}
}

// Put sets m[code] to *v when v is set and code is not empty.
func Put[T any](m map[string]any, code string, v *T) {
	if v == nil || code == "" {
		return
	}
	m[code] = *v
}

// PutInt is Put for integer types, stored as int64. Named types need this
// because the msgpack encoder writes a type with a MarshalText method, such
// as wifi.Encryption, as text.
func PutInt[T ~int](m map[string]any, code string, v *T) {
	if v == nil || code == "" {
		return
	}
	m[code] = int64(*v)
}

// Mask returns Redacted for a non-empty s and "" otherwise.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
}

// MaskFields returns a copy of m with the non-empty strings under codes
// replaced by Redacted. A nil m gives an empty map.
func MaskFields(m map[string]any, codes ...string) map[string]any {
	out := maps.Clone(m)
	if out == nil {
		out = map[string]any{}
	}
	for _, code := range codes {
		if s, ok := out[code].(string); ok && s != "" {
			out[code] = Redacted
		}
	}
	return out
}

// CodeName returns the name for an integer code in names, or "#code" when
// it is not known.
func CodeName(names map[string]string, code int64) string {
	return KeyName(names, strconv.FormatInt(code, 10))
}

// KeyName is CodeName for a code that is already a decimal string, such as
// an option map key.
func KeyName(names map[string]string, key string) string {
	if n, ok := names[key]; ok {
		return n
	}
	return "#" + key
}

// Name returns the name for the integer code under m[key], or "" if m has
// no such key.
func Name(names map[string]string, m map[string]any, key string) string {
	if _, ok := m[key]; !ok {
		return ""
	}
	return CodeName(names, Int(m, key))
}

// Code returns the integer code for name in names, ignoring case. "#N" is
// accepted for codes without a name. kind names the enum in the error.
func Code(kind string, names map[string]string, name string) (int64, error) {
	for k, n := range names {
		if strings.EqualFold(n, name) {
			return strconv.ParseInt(k, 10, 64)
		}
	}
	if num, ok := strings.CutPrefix(name, "#"); ok {
		if n, err := strconv.ParseInt(num, 10, 64); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("unknown %s %q (want one of %s)", kind, name, strings.Join(sortedValues(names), ", "))
}

// CompareNumeric orders strings numerically when both are integers, numbers
// before other strings, and other strings lexically. It suits option codes,
// IDs and port names.
func CompareNumeric(a, b string) int {
	x, errA := strconv.ParseInt(a, 10, 64)
	y, errB := strconv.ParseInt(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		return cmp.Compare(x, y)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func sortedValues(m map[string]string) []string {
	return slices.Sorted(maps.Values(m))
}
