package amplifi

import (
	"context"
	"iter"
	"strings"
	"time"

	"github.com/borud/amplifi/internal/wire"
	"github.com/gorilla/websocket"
)

// pingInterval is the WebSocket keepalive interval.
const pingInterval = 10 * time.Second

// pongWait is how long Events waits for any frame, pong or message, before
// giving up on the unit.
const pongWait = 3 * pingInterval

// Event is a message the unit pushes on its own, such as a client joining
// (ClusterNode.ClientMacDiscovered) or a configuration change
// (ClusterNode.WifiConfigChanged). The protocol calls these signals.
type Event struct {
	Time      time.Time
	Interface string // e.g. "ClusterNode"
	Name      string // e.g. "ClientMacDiscovered"

	// Payload is the decoded payload: a JSON value or a decoded msgpack
	// value, or nil if the event carries none. In msgpack payloads binary
	// values are []byte, and binary map keys are strings of the raw bytes.
	Payload any
}

// Events streams the unit's events until ctx is done, keeping the session
// alive with periodic pings. Iteration ends with a non-nil error if the
// connection fails or the unit stops answering pings, and silently when ctx
// is done. The session must not be used for calls while iterating.
func (c *Client) Events(ctx context.Context) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		defer c.bind(ctx)()
		c.conn.SetPongHandler(func(string) error {
			c.setReadDeadline(ctx, time.Now().Add(pongWait))
			return nil
		})
		defer c.conn.SetPongHandler(nil)
		defer c.keepalive()()

		for {
			c.setReadDeadline(ctx, time.Now().Add(pongWait))
			resp, err := c.recv()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				yield(Event{}, err)
				return
			}
			if !resp.IsSignal() {
				continue
			}
			if !yield(c.event(resp), nil) {
				return
			}
		}
	}
}

// Signals is Events, kept until mesh.Ports moves to Events; removed after
// merge.
func (c *Client) Signals(ctx context.Context) iter.Seq2[Event, error] { return c.Events(ctx) }

// event converts a signal message.
func (c *Client) event(resp wire.Response) Event {
	e := Event{
		Time:      time.Now(),
		Interface: strings.TrimPrefix(resp.Iface, wire.IfacePrefix),
		Name:      resp.Signal,
	}

	if v, ok := resp.Payload["value"]; ok {
		e.Payload = v
		return e
	}

	v, err := resp.DecodeMsgpack()
	if err != nil {
		c.log.Debug("undecodable event payload", "event", resp.Signal, "err", err)
		return e
	}
	e.Payload = v
	return e
}

// keepalive pings the unit every pingInterval until the returned stop func is
// called.
func (c *Client) keepalive() (stop func()) {
	done := make(chan struct{})
	go c.pingLoop(done)
	return func() { close(done) }
}

func (c *Client) pingLoop(done <-chan struct{}) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			c.ping()
		}
	}
}

func (c *Client) ping() {
	err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(pingInterval))
	if err != nil {
		c.log.Debug("ping failed", "err", err)
	}
}
