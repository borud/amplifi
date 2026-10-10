package amplifi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/borud/amplifi/internal/wire"
	"github.com/gorilla/websocket"
)

// Interface names accepted by Call, CallJSON, Property and SetProperty. The
// short names expand to the full com.ubnt.UnifiHome.* form.
const (
	IfaceDevice      = "Device"
	IfaceClusterNode = "ClusterNode"
	IfaceFwUpdate    = "FwUpdate"
	IfaceMagicLink   = "MagicLink"
	IfaceUnsecure    = "Unsecure"
)

// disconnectWait bounds how long CallDisconnecting waits for a reply.
const disconnectWait = 5 * time.Second

// fullIface expands a short interface name such as "Device" to
// "com.ubnt.UnifiHome.Device". Full names are returned unchanged.
func fullIface(iface string) string {
	if strings.HasPrefix(iface, wire.IfacePrefix) {
		return iface
	}
	return wire.IfacePrefix + iface
}

// RawMsgpack is an already encoded msgpack argument for Call.
type RawMsgpack []byte

// Call invokes any method with a msgpack argument and returns the decoded
// result. It is the escape hatch for methods without a typed wrapper.
//
// In arg, map[string]any keys that are decimal numbers become integer option
// codes and []byte becomes binary. A nil arg sends no argument. Raw msgpack
// can be passed as RawMsgpack. The result is a decoded msgpack value (maps
// are map[string]any keyed as described for Event.Payload) or, for methods
// that answer in JSON, the JSON value.
func (c *Client) Call(ctx context.Context, iface, method string, arg any) (any, error) {
	packed, err := packArg(arg)
	if err != nil {
		return nil, err
	}
	resp, err := c.call(ctx, fullIface(iface), method, map[string]any{"msgpack": base64.StdEncoding.EncodeToString(packed)})
	if err != nil {
		return nil, err
	}
	return responseValue(resp)
}

// CallJSON invokes a method that takes a JSON argument (payload.value).
func (c *Client) CallJSON(ctx context.Context, iface, method string, arg any) (any, error) {
	resp, err := c.call(ctx, fullIface(iface), method, map[string]any{"value": arg})
	if err != nil {
		return nil, err
	}
	return responseValue(resp)
}

// CallDisconnecting invokes a method after which the unit may drop the
// connection instead of replying, such as Reboot or FactoryReset. Once the
// request is sent, a closed connection or no reply within a few seconds
// counts as success, since a unit going down may do either; a router error in
// a reply is returned. The session is unusable afterwards.
func (c *Client) CallDisconnecting(ctx context.Context, iface, method string, arg any) error {
	packed, err := packArg(arg)
	if err != nil {
		return err
	}
	defer c.bind(ctx)()

	req := wire.Request{Type: "call", Iface: fullIface(iface), Method: method, Payload: map[string]any{"msgpack": base64.StdEncoding.EncodeToString(packed)}}
	seq, err := c.send(req)
	if err != nil {
		return err
	}

	capped := c.setReadDeadline(ctx, time.Now().Add(disconnectWait))
	for {
		resp, err := c.recv()
		if err != nil {
			return c.disconnectResult(ctx, req, capped, err)
		}
		if !resp.IsResponse() || resp.SeqID != seq {
			continue
		}
		if code, msg, ok := resp.Fault(); ok {
			return fmt.Errorf("%s: %w", requestName(req), &RouterError{Code: code, Msg: msg})
		}
		return nil
	}
}

// disconnectResult maps a read error after a disconnecting request to the
// call's result. capped reports whether ctx's deadline, rather than
// disconnectWait, bounded the read.
func (c *Client) disconnectResult(ctx context.Context, req wire.Request, capped bool, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", requestName(req), ctx.Err())
	}

	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		if capped {
			return fmt.Errorf("%s: %w", requestName(req), context.DeadlineExceeded)
		}
		c.log.Debug("no reply after request", "method", req.Method)
		return nil
	}

	_, closed := errors.AsType[*websocket.CloseError](err)
	if closed ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) {
		c.log.Debug("connection closed after request", "method", req.Method, "err", err)
		return nil
	}
	return fmt.Errorf("%s: %w", requestName(req), err)
}

// Property reads a property such as Device.FriendlyName or Unsecure.Uptime.
func (c *Client) Property(ctx context.Context, iface, name string) (any, error) {
	resp, err := c.do(ctx, wire.Request{Type: "getProperty", Iface: fullIface(iface), Property: name})
	if err != nil {
		return nil, err
	}
	return resp.Payload["value"], nil
}

// SetProperty writes a property.
func (c *Client) SetProperty(ctx context.Context, iface, name string, value any) error {
	_, err := c.do(ctx, wire.Request{
		Type:     "setProperty",
		Iface:    fullIface(iface),
		Property: name,
		Payload:  map[string]any{"value": value},
	})
	return err
}

// Ubus calls an OpenWrt ubus method on the unit through Device.UbusCall, for
// example Ubus(ctx, "perfd", "mac-table", nil). args is sent as JSON. The
// router answers with JSON, which is returned decoded.
func (c *Client) Ubus(ctx context.Context, path, method string, args map[string]any) (any, error) {
	if args == nil {
		args = map[string]any{}
	}
	argJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("ubus args: %w", err)
	}

	// The argument is three consecutive msgpack strings (packUbusCall), not
	// an array.
	var packed []byte
	for _, s := range []string{path, method, string(argJSON)} {
		b, err := wire.EncodeMsgpack(s)
		if err != nil {
			return nil, err
		}
		packed = append(packed, b...)
	}

	v, err := c.Call(ctx, IfaceDevice, "UbusCall", RawMsgpack(packed))
	if err != nil {
		return nil, err
	}
	s, ok := v.(string)
	if !ok {
		return v, nil
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return s, nil
	}
	return out, nil
}

// packArg encodes a Call argument.
func packArg(arg any) ([]byte, error) {
	switch a := arg.(type) {
	case nil:
		return nil, nil
	case RawMsgpack:
		return a, nil
	default:
		return wire.EncodeMsgpack(arg)
	}
}

// responseValue returns a response's JSON value, or its decoded msgpack.
func responseValue(resp wire.Response) (any, error) {
	if v, ok := resp.Payload["value"]; ok {
		return v, nil
	}
	return resp.DecodeMsgpack()
}
