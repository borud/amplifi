// Package amplifi controls Ubiquiti AmpliFi routers and mesh points over their
// local control protocol.
// No cloud account is involved.
//
// A unit is any AmpliFi box. The router is the unit connected to the
// internet; mesh points are the others.
//
// This package holds the session: Dial logs in with the admin password and
// returns an encrypted Client. The functional areas live in their own
// packages, which take the client as their second argument:
//
//	c, err := amplifi.Dial(ctx, "192.0.2.10", password)
//	if err != nil {
//		return err
//	}
//	defer c.Close()
//
//	board, err := device.Board(ctx, c)
//	peers, err := mesh.Peers(ctx, c)
//	cfg, err := wifi.Config(ctx, c)
//
// The area packages are device, mesh, clients, wifi, network (including DHCP
// and port forwarding), access, magiclink, firmware and system; discovery
// finds units on the LAN. Client.Call, CallJSON and Property reach any method
// directly, and Client.Events streams the events the unit pushes.
//
// The wire protocol is a WebSocket on port 9016 carrying AllJoyn-style JSON
// method calls, authenticated with SRP-6a and encrypted with libsodium's
// secretstream construction.
//
// Units use self-signed TLS certificates, so the certificate chain is not
// verified. To detect a replaced or impersonated unit, save
// Client.CertificateFingerprint after the first login and pass it to later
// dials with WithCertificateFingerprint.
package amplifi

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi/internal/secretstream"
	"github.com/borud/amplifi/internal/srp"
	"github.com/borud/amplifi/internal/wire"
	"github.com/gorilla/websocket"
)

// Port is the TCP port the control WebSocket listens on, on the router and on
// every mesh point.
const Port = 9016

// subprotocol is the required WebSocket subprotocol.
const subprotocol = "uh-ws-proto"

// Client is an authenticated, encrypted control session with one unit. It is
// not safe for concurrent use.
type Client struct {
	conn   *websocket.Conn
	stream *secretstream.Stream
	seq    int
	log    *slog.Logger
	hash   []byte

	// pin is the required certificate fingerprint, or "" for none.
	pin string
	// fingerprint is the fingerprint of the unit's certificate.
	fingerprint string
}

// Option configures Dial.
type Option func(*Client)

// WithLogger sets the logger used for protocol tracing. The default discards
// all output.
func WithLogger(log *slog.Logger) Option {
	return func(c *Client) { c.log = log }
}

// WithCertificateFingerprint makes Dial fail with a CertificateMismatchError,
// before sending anything, unless the unit's TLS certificate has fingerprint
// fp, in the format of Client.CertificateFingerprint.
func WithCertificateFingerprint(fp string) Option {
	return func(c *Client) { c.pin = fp }
}

// Dial connects to the unit at host, logs in with the admin password and
// switches the session to encrypted framing. The certificate chain is not
// verified because units use self-signed certificates; see
// WithCertificateFingerprint.
func Dial(ctx context.Context, host, password string, opts ...Option) (*Client, error) {
	return dial(ctx, host, secret{password: password}, opts)
}

// DialWithHash is Dial with a password hash from Client.PasswordHash instead
// of the password. It fails with ErrAuth if the password has since changed or
// host is a different unit.
func DialWithHash(ctx context.Context, host string, hash []byte, opts ...Option) (*Client, error) {
	return dial(ctx, host, secret{x: new(big.Int).SetBytes(hash)}, opts)
}

// PasswordHash returns the hash this session logged in with. It lets
// DialWithHash log in to the same unit later without the password. The hash
// cannot be turned back into the password, but it grants the same access, so
// store it as carefully as the password itself.
func (c *Client) PasswordHash() []byte { return c.hash }

// CertificateFingerprint returns the fingerprint of the unit's TLS
// certificate in the format SSH uses: "SHA256:" followed by the unpadded
// base64 SHA-256 digest of the certificate's DER encoding.
func (c *Client) CertificateFingerprint() string { return c.fingerprint }

// secret is what a login proves knowledge of: the password, or the SRP x
// derived from it.
type secret struct {
	password string
	x        *big.Int
}

// step answers the router's SRP challenge with whichever secret is set.
func (s secret) step(salt, b *big.Int) (*srp.Result, error) {
	if s.x != nil {
		return srp.StepX(s.x, salt, b)
	}
	return srp.Step(s.password, salt, b)
}

func dial(ctx context.Context, host string, sec secret, opts []Option) (*Client, error) {
	c := newClient(opts)
	if err := c.connect(ctx, net.JoinHostPort(host, strconv.Itoa(Port))); err != nil {
		return nil, err
	}

	if err := c.login(ctx, sec); err != nil {
		c.conn.Close()
		return nil, err
	}
	return c, nil
}

func newClient(opts []Option) *Client {
	c := &Client{log: slog.New(slog.DiscardHandler)}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// connect opens the WebSocket to addr. A pinned fingerprint is checked during
// the TLS handshake, so a mismatch fails before any request is sent.
func (c *Client) connect(ctx context.Context, addr string) error {
	u := url.URL{Scheme: "wss", Host: addr, Path: "/"}
	dialer := websocket.Dialer{
		Subprotocols: []string{subprotocol},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // units use a self-signed cert; verifyConnection pins it
			VerifyConnection:   c.verifyConnection,
		},
	}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", u.String(), err)
	}
	c.conn = conn
	c.log.Debug("connected", "addr", addr, "fingerprint", c.fingerprint)
	return nil
}

// verifyConnection records the fingerprint of the unit's certificate and
// checks it against the pinned one, if any.
func (c *Client) verifyConnection(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("unit sent no certificate")
	}
	c.fingerprint = fingerprint(cs.PeerCertificates[0].Raw)

	if c.pin != "" && c.pin != c.fingerprint {
		return &CertificateMismatchError{Want: c.pin, Got: c.fingerprint}
	}
	return nil
}

// fingerprint formats the SHA-256 fingerprint of a DER certificate.
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Close closes the session.
func (c *Client) Close() error { return c.conn.Close() }

// login runs the SRP-6a handshake and verifies the router's evidence. On
// success the session switches to encrypted framing.
func (c *Client) login(ctx context.Context, sec secret) error {
	first, err := c.callJSON(ctx, wire.IfaceAuth, "SrpServerAuth", map[string]any{
		"user": srp.User,
		"caps": 1,
	})
	if err != nil {
		return fmt.Errorf("srp step 1: %w", err)
	}

	salt, err := b64Int(first, "s")
	if err != nil {
		return err
	}
	bPub, err := b64Int(first, "B")
	if err != nil {
		return err
	}
	c.log.Debug("srp challenge received", "caps", first["caps"])

	res, err := sec.step(salt, bPub)
	if err != nil {
		return fmt.Errorf("srp: %w", err)
	}

	second, err := c.callJSON(ctx, wire.IfaceAuth, "SrpServerAuth", map[string]any{
		"A": b64(res.A),
		"M": b64(res.M1),
	})
	if err != nil {
		if _, ok := errors.AsType[*RouterError](err); ok {
			return fmt.Errorf("%w: %w", ErrAuth, err)
		}
		return fmt.Errorf("srp step 2: %w", err)
	}

	hamk, err := b64Int(second, "HAMK")
	if err != nil {
		return err
	}
	if hamk.Cmp(res.M2) != 0 {
		return fmt.Errorf("%w: router evidence mismatch", ErrAuth)
	}

	stream, err := secretstream.New(res.SessionKey)
	if err != nil {
		return fmt.Errorf("start encryption: %w", err)
	}
	c.stream = stream
	// The SRP x is the reusable password hash; see DialWithHash.
	c.hash = res.X.Bytes()
	c.log.Debug("authenticated, encryption enabled")
	return nil
}

// callJSON sends a method call with a JSON value payload (payload.value) and
// returns the decoded response value object.
func (c *Client) callJSON(ctx context.Context, iface, method string, value map[string]any) (map[string]any, error) {
	resp, err := c.call(ctx, iface, method, map[string]any{"value": value})
	if err != nil {
		return nil, err
	}
	v := resp.Value()
	if v == nil {
		return nil, fmt.Errorf("%s.%s: response had no value", iface, method)
	}
	return v, nil
}

// call sends one method call and returns the matching response, skipping
// any unsolicited signal messages.
func (c *Client) call(ctx context.Context, iface, method string, payload map[string]any) (wire.Response, error) {
	return c.do(ctx, wire.Request{Type: "call", Iface: iface, Method: method, Payload: payload})
}

// do sends req and returns the matching response, skipping any unsolicited
// signal messages.
func (c *Client) do(ctx context.Context, req wire.Request) (wire.Response, error) {
	defer c.bind(ctx)()

	seq, err := c.send(req)
	if err != nil {
		return wire.Response{}, err
	}

	for {
		resp, err := c.recv()
		if err != nil {
			return wire.Response{}, err
		}
		if resp.IsSignal() {
			c.log.Debug("ignoring signal", "signal", resp.Signal)
			continue
		}
		if !resp.IsResponse() || resp.SeqID != seq {
			c.log.Debug("ignoring out-of-band message", "type", resp.Type, "seqId", resp.SeqID)
			continue
		}
		if code, msg, ok := resp.Fault(); ok {
			return wire.Response{}, fmt.Errorf("%s: %w", requestName(req), &RouterError{Code: code, Msg: msg})
		}
		return resp, nil
	}
}

// requestName names a request for error messages, e.g. "Device.Reboot".
func requestName(req wire.Request) string {
	name := req.Method
	if name == "" {
		name = req.Property
	}
	return strings.TrimPrefix(req.Iface, wire.IfacePrefix) + "." + name
}

// bind applies ctx's deadline and cancellation to the connection until the
// returned release func is called. A call interrupted this way leaves the
// session unusable.
func (c *Client) bind(ctx context.Context) (release func()) {
	nc := c.conn.NetConn()
	deadline, _ := ctx.Deadline()
	_ = nc.SetDeadline(deadline)

	fired := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = nc.SetDeadline(time.Now())
		close(fired)
	})
	return func() {
		if !stop() {
			<-fired
		}
		_ = nc.SetDeadline(time.Time{})
	}
}

// setReadDeadline sets the read deadline to t, or to ctx's deadline if that
// is earlier, without undoing a cancellation applied by bind. It reports
// whether ctx's deadline was the earlier one.
func (c *Client) setReadDeadline(ctx context.Context, t time.Time) (capped bool) {
	if d, ok := ctx.Deadline(); ok && d.Before(t) {
		t = d
		capped = true
	}
	_ = c.conn.SetReadDeadline(t)

	// bind's AfterFunc may have fired before the line above.
	if ctx.Err() != nil {
		_ = c.conn.SetReadDeadline(time.Now())
	}
	return capped
}

// send writes one request, assigning its sequence number, and returns that
// number. Messages are plaintext text frames until login completes, encrypted
// binary frames after.
func (c *Client) send(req wire.Request) (int, error) {
	req.SeqID = c.seq
	c.seq++

	body, err := json.Marshal(req)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	if c.stream == nil {
		return req.SeqID, c.conn.WriteMessage(websocket.TextMessage, body)
	}
	sealed, err := c.stream.Seal(body)
	if err != nil {
		return 0, fmt.Errorf("encrypt request: %w", err)
	}
	return req.SeqID, c.conn.WriteMessage(websocket.BinaryMessage, sealed)
}

// recv reads one message. Before login it accepts only plaintext text
// frames; after, only encrypted binary frames, since the encryption is what
// authenticates the unit.
func (c *Client) recv() (wire.Response, error) {
	mt, data, err := c.conn.ReadMessage()
	if err != nil {
		return wire.Response{}, fmt.Errorf("read message: %w", err)
	}

	if mt == websocket.TextMessage && c.stream != nil {
		return wire.Response{}, errors.New("plaintext frame on encrypted session")
	}
	if mt == websocket.BinaryMessage {
		if c.stream == nil {
			return wire.Response{}, errors.New("binary frame before encryption was enabled")
		}
		data, err = c.stream.Open(data)
		if err != nil {
			return wire.Response{}, fmt.Errorf("decrypt message: %w", err)
		}
	}

	var resp wire.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return wire.Response{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}

// b64 encodes an unsigned big integer as standard base64.
func b64(x *big.Int) string { return base64.StdEncoding.EncodeToString(x.Bytes()) }

// b64Int decodes a base64 field of a response into an unsigned big integer.
func b64Int(m map[string]any, key string) (*big.Int, error) {
	s, ok := m[key].(string)
	if !ok {
		return nil, fmt.Errorf("response missing %q", key)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("decode %q: %w", key, err)
	}
	return new(big.Int).SetBytes(raw), nil
}
