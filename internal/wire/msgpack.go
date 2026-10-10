package wire

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	"github.com/vmihailenco/msgpack/v5/msgpcode"
)

// DecodeMsgpack decodes a msgpack payload into plain Go values. Maps become
// map[string]any whatever their key type: integer option codes become their
// decimal string ("1"), and binary keys (such as the 6 byte MACs keying a peer
// list) become a string of the raw bytes, since Go maps cannot have []byte
// keys.
func DecodeMsgpack(b []byte) (any, error) {
	v, err := decode(msgpack.NewDecoder(bytes.NewReader(b)))
	if err != nil {
		return nil, fmt.Errorf("decode msgpack: %w", err)
	}
	return v, nil
}

func decode(d *msgpack.Decoder) (any, error) {
	c, err := d.PeekCode()
	if err != nil {
		return nil, err
	}

	switch {
	case msgpcode.IsFixedMap(c), c == msgpcode.Map16, c == msgpcode.Map32:
		return decodeMap(d)
	case msgpcode.IsFixedArray(c), c == msgpcode.Array16, c == msgpcode.Array32:
		return decodeArray(d)
	default:
		return d.DecodeInterface()
	}
}

func decodeMap(d *msgpack.Decoder) (map[string]any, error) {
	n, err := d.DecodeMapLen()
	if err != nil {
		return nil, err
	}

	m := make(map[string]any, max(n, 0))
	for range n {
		k, err := decode(d)
		if err != nil {
			return nil, err
		}
		v, err := decode(d)
		if err != nil {
			return nil, err
		}
		m[keyString(k)] = v
	}
	return m, nil
}

func decodeArray(d *msgpack.Decoder) ([]any, error) {
	n, err := d.DecodeArrayLen()
	if err != nil {
		return nil, err
	}

	a := make([]any, 0, max(n, 0))
	for range n {
		v, err := decode(d)
		if err != nil {
			return nil, err
		}
		a = append(a, v)
	}
	return a, nil
}

func keyString(k any) string {
	if n, ok := ToInt64(k); ok {
		return strconv.FormatInt(n, 10)
	}
	switch v := k.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// ToInt64 coerces the integer types msgpack decodes into. It reports false
// for non-integers and for uint64 values above math.MaxInt64.
func ToInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	default:
		return 0, false
	}
}

// EncodeMsgpack encodes v as the inverse of DecodeMsgpack: map keys that are decimal numbers
// are packed as integers (option codes), other keys as strings; []byte is
// packed as binary and integers in their most compact form.
func EncodeMsgpack(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactInts(true)
	if err := encode(enc, v); err != nil {
		return nil, fmt.Errorf("encode msgpack: %w", err)
	}
	return buf.Bytes(), nil
}

func encode(e *msgpack.Encoder, v any) error {
	switch t := v.(type) {
	case map[string]any:
		if err := e.EncodeMapLen(len(t)); err != nil {
			return err
		}
		for _, k := range slices.Sorted(maps.Keys(t)) {
			if err := encodeKey(e, k); err != nil {
				return err
			}
			if err := encode(e, t[k]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		if err := e.EncodeArrayLen(len(t)); err != nil {
			return err
		}
		for _, item := range t {
			if err := encode(e, item); err != nil {
				return err
			}
		}
		return nil
	default:
		return e.Encode(v)
	}
}

// encodeKey packs k as an integer if it is the canonical decimal form of one,
// so "01" and "+5" stay strings.
func encodeKey(e *msgpack.Encoder, k string) error {
	if n, err := strconv.ParseInt(k, 10, 64); err == nil && strconv.FormatInt(n, 10) == k {
		return e.EncodeInt(n)
	}
	return e.EncodeString(k)
}

// distributedLastUpdateTimestamp is DistributedConfigOption.LastUpdateTimestamp.
const distributedLastUpdateTimestamp = "2"

// ClusterNodeArg wraps a ClusterNode setter payload as
// [{LastUpdateTimestamp: now in µs}, payload].
func ClusterNodeArg(payload any) ([]byte, error) {
	return EncodeMsgpack([]any{
		map[string]any{distributedLastUpdateTimestamp: time.Now().UnixMicro()},
		payload,
	})
}

// ClusterNodePayload extracts the payload map from a ClusterNode getter
// result, which is [cluster metadata, payload] (ClusterNodeArray).
func ClusterNodePayload(v any) (map[string]any, error) {
	a, ok := v.([]any)
	if !ok || len(a) < 2 {
		return nil, fmt.Errorf("expected [metadata, payload], got %T", v)
	}
	m, ok := a[1].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected payload map, got %T", a[1])
	}
	return m, nil
}
