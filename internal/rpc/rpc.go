// Package rpc holds the helpers the area packages (device, wifi, clients, ...)
// share on top of amplifi.Client's public Call and CallJSON: typed result
// decoding, read-modify-write setters, and access to the integer-keyed option
// maps the Device and ClusterNode interfaces use.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

// Null and TwoNulls are the msgpack nil and [nil, nil] arguments many getters
// expect, ready to pass to Call.
var (
	Null     = amplifi.RawMsgpack(wire.MsgpackNull)
	TwoNulls = amplifi.RawMsgpack(wire.MsgpackTwoNulls)
)

// Caller is the part of *amplifi.Client the helpers need.
type Caller interface {
	Call(ctx context.Context, iface, method string, arg any) (any, error)
	CallJSON(ctx context.Context, iface, method string, arg any) (any, error)
}

// Map calls a method that answers with a map, such as an option map.
func Map(ctx context.Context, c Caller, iface, method string, arg any) (map[string]any, error) {
	v, err := c.Call(ctx, iface, method, arg)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a map, got %T", method, v)
	}
	return m, nil
}

// ClusterMap calls a ClusterNode getter, which takes a msgpack nil and
// answers [metadata, payload], and returns the payload.
func ClusterMap(ctx context.Context, c Caller, method string) (map[string]any, error) {
	v, err := c.Call(ctx, amplifi.IfaceClusterNode, method, Null)
	if err != nil {
		return nil, err
	}
	m, err := wire.ClusterNodePayload(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return m, nil
}

// Value calls a method with a msgpack argument (nil for none) whose result is
// JSON, either as payload.value or as a msgpack string holding JSON, and
// decodes it into out.
func Value(ctx context.Context, c Caller, iface, method string, arg, out any) error {
	v, err := c.Call(ctx, iface, method, arg)
	if err != nil {
		return err
	}
	return Decode(method, v, out)
}

// JSON calls a method with a JSON argument (payload.value) and decodes the
// result into out. A nil out discards the result.
func JSON(ctx context.Context, c Caller, iface, method string, arg, out any) error {
	v, err := c.CallJSON(ctx, iface, method, arg)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return Decode(method, v, out)
}

// Decode converts a decoded result into out by way of JSON. A string holding
// JSON is parsed first, as some methods answer with one.
func Decode(method string, v, out any) error {
	b, err := json.Marshal(JSONString(v))
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

// JSONString returns v parsed as JSON if it is a string holding JSON, and v
// unchanged otherwise.
func JSONString(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return v
	}
	return out
}

// Set calls a setter with a msgpack payload (an option map, or a list for the
// Add*/Delete* methods) and checks its boolean result.
func Set(ctx context.Context, c Caller, iface, method string, payload any) error {
	v, err := c.Call(ctx, iface, method, payload)
	if err != nil {
		return err
	}
	return ExpectOK(method, v)
}

// SetCluster calls a ClusterNode setter, wrapping payload as
// [{timestamp}, payload].
func SetCluster(ctx context.Context, c Caller, method string, payload any) error {
	arg, err := wire.ClusterNodeArg(payload)
	if err != nil {
		return err
	}
	return Set(ctx, c, amplifi.IfaceClusterNode, method, amplifi.RawMsgpack(arg))
}

// Update does a read-modify-write of an option map: it reads the map with
// getMethod and getArg, applies changes (option code -> value) and writes the
// whole map back with setMethod.
func Update(ctx context.Context, c Caller, iface, getMethod string, getArg any, setMethod string, changes map[string]any) error {
	m, err := Map(ctx, c, iface, getMethod, getArg)
	if err != nil {
		return err
	}
	maps.Copy(m, changes)
	return Set(ctx, c, iface, setMethod, Flat(m))
}

// UpdateEntry does a read-modify-write of a map of option maps keyed by
// name, such as radios or interfaces: it applies changes to the entry under
// key and writes every entry back flattened. Empty changes do nothing.
func UpdateEntry(ctx context.Context, c Caller, iface, getMethod string, getArg any, setMethod, key string, changes map[string]any) error {
	if len(changes) == 0 {
		return nil
	}

	m, err := Map(ctx, c, iface, getMethod, getArg)
	if err != nil {
		return err
	}
	payload, err := applyEntry(m, key, changes)
	if err != nil {
		return err
	}
	return Set(ctx, c, iface, setMethod, payload)
}

// applyEntry returns m with changes applied to the entry under key and every
// entry flattened. Values of m that are not maps are dropped.
func applyEntry(m map[string]any, key string, changes map[string]any) (map[string]any, error) {
	if _, ok := m[key].(map[string]any); !ok {
		return nil, fmt.Errorf("no entry %q (have %s)", key, strings.Join(slices.Sorted(maps.Keys(m)), ", "))
	}

	out := make(map[string]any, len(m))
	for k, entry := range Submaps(m) {
		entry = Flat(entry)
		if k == key {
			maps.Copy(entry, changes)
		}
		out[k] = entry
	}
	return out, nil
}

// UpdateCluster is Update for a ClusterNode option map.
func UpdateCluster(ctx context.Context, c Caller, getMethod, setMethod string, changes map[string]any) error {
	m, err := ClusterMap(ctx, c, getMethod)
	if err != nil {
		return err
	}
	maps.Copy(m, changes)
	return SetCluster(ctx, c, setMethod, Flat(m))
}

// Flat drops nested maps from an option map. Setters take only scalars,
// binary values and lists, so nested maps are never sent back.
func Flat(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if _, nested := v.(map[string]any); nested {
			continue
		}
		out[k] = v
	}
	return out
}

// ExpectOK checks a setter's result: a msgpack boolean that must be true. A
// nil result is accepted, as some setters return nothing.
func ExpectOK(method string, v any) error {
	if ok, isBool := v.(bool); isBool && !ok {
		return fmt.Errorf("%s: unit refused the change", method)
	}
	return nil
}
