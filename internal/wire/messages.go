// Package wire defines the JSON envelopes exchanged on the AmpliFi control
// WebSocket.
package wire

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// IfacePrefix is the common prefix of the AllJoyn interface names.
const IfacePrefix = "com.ubnt.UnifiHome."

// Interface names used internally, as sent in the "iface" field. The public
// set is amplifi.Iface*.
const (
	IfaceAuth        = IfacePrefix + "Auth"
	IfaceFingerprint = IfacePrefix + "ClusterNode.fingerprint"
)

var (
	// MsgpackNull is a msgpack nil, the argument getters such as
	// GetWifiClientCount expect.
	MsgpackNull = []byte{0xc0}

	// MsgpackTwoNulls is a msgpack [nil, nil].
	MsgpackTwoNulls = []byte{0x92, 0xc0, 0xc0}
)

// Request is the JSON envelope for a method call (Type "call") or a
// property access (Type "getProperty" or "setProperty").
type Request struct {
	Type     string         `json:"type"`
	Iface    string         `json:"iface"`
	SeqID    int            `json:"seqId"`
	Method   string         `json:"method,omitempty"`
	Property string         `json:"property,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
}

// Response is the JSON envelope the router returns, either a reply to a call
// or an unsolicited signal.
type Response struct {
	Type    string         `json:"type"`
	Iface   string         `json:"iface"`
	SeqID   int            `json:"seqId"`
	Signal  string         `json:"signal"`
	Payload map[string]any `json:"payload"`
}

// IsSignal reports whether r is an unsolicited event.
func (r Response) IsSignal() bool { return r.Type == "signal" }

// IsResponse reports whether r is a reply to a call.
func (r Response) IsResponse() bool { return r.Type == "response" }

// Fault returns the router error carried in payload.error, if any.
func (r Response) Fault() (code int, msg string, ok bool) {
	e, ok := r.Payload["error"].(map[string]any)
	if !ok {
		return 0, "", false
	}
	if c, ok := e["code"].(float64); ok {
		code = int(c)
	}
	msg, _ = e["msg"].(string)
	return code, msg, true
}

// Value returns the decoded payload.value object, if present.
func (r Response) Value() map[string]any {
	v, _ := r.Payload["value"].(map[string]any)
	return v
}

// Msgpack returns the base64 string under payload.msgpack, if present.
func (r Response) Msgpack() (string, bool) {
	s, ok := r.Payload["msgpack"].(string)
	return s, ok
}

// DecodeMsgpack decodes payload.msgpack (see DecodeMsgpack). It returns nil
// when the payload carries no msgpack or an empty one.
func (r Response) DecodeMsgpack() (any, error) {
	raw, ok := r.Msgpack()
	if !ok || raw == "" {
		return nil, nil
	}
	packed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("decode msgpack base64: %w", err)
	}
	return DecodeMsgpack(packed)
}
