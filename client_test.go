package amplifi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/borud/amplifi/internal/secretstream"
	"github.com/gorilla/websocket"
)

// testClient returns an encrypted Client connected to a local WebSocket
// server that runs serve on its end of the connection.
func testClient(t *testing.T, serve func(*websocket.Conn)) *Client {
	t.Helper()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		serve(conn)
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	stream, err := secretstream.New(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return &Client{conn: conn, stream: stream, log: slog.New(slog.DiscardHandler)}
}

func TestRecvRejectsPlaintextAfterLogin(t *testing.T) {
	c := testClient(t, func(conn *websocket.Conn) {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response","seqId":0,"payload":{}}`))
		_, _, _ = conn.ReadMessage()
	})

	if _, err := c.recv(); err == nil {
		t.Fatal("plaintext frame accepted on encrypted session")
	}
}

func TestCallDisconnectingClosed(t *testing.T) {
	c := testClient(t, func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.CallDisconnecting(ctx, IfaceDevice, "Reboot", nil); err != nil {
		t.Fatalf("closed connection: %v", err)
	}
}

func TestCallDisconnectingContextDone(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	c := testClient(t, func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
		<-done
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := c.CallDisconnecting(ctx, IfaceDevice, "Reboot", nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want deadline exceeded", err)
	}
}

func TestCallDisconnectingBadReply(t *testing.T) {
	c := testClient(t, func(conn *websocket.Conn) {
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x00})
		_, _, _ = conn.ReadMessage()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.CallDisconnecting(ctx, IfaceDevice, "Reboot", nil); err == nil {
		t.Fatal("undecryptable reply treated as success")
	}
}

// tlsServer starts a TLS WebSocket server and returns its address, the
// fingerprint of its certificate and a count of the requests it receives.
func tlsServer(t *testing.T) (addr, fp string, requests *atomic.Int32) {
	t.Helper()

	requests = new(atomic.Int32)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
	}))
	// A rejected handshake is expected; keep it out of the test output.
	srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	srv.StartTLS()
	t.Cleanup(srv.Close)

	sum := sha256.Sum256(srv.Certificate().Raw)
	fp = "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	return srv.Listener.Addr().String(), fp, requests
}

func TestConnectPinnedMatch(t *testing.T) {
	addr, fp, requests := tlsServer(t)

	c := newClient([]Option{WithCertificateFingerprint(fp)})
	if err := c.connect(context.Background(), addr); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if got := c.CertificateFingerprint(); got != fp {
		t.Fatalf("fingerprint = %q, want %q", got, fp)
	}
	if n := requests.Load(); n != 1 {
		t.Fatalf("server got %d requests, want 1", n)
	}
}

func TestConnectUnpinnedRecordsFingerprint(t *testing.T) {
	addr, fp, _ := tlsServer(t)

	c := newClient(nil)
	if err := c.connect(context.Background(), addr); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if got := c.CertificateFingerprint(); got != fp {
		t.Fatalf("fingerprint = %q, want %q", got, fp)
	}
}

func TestConnectPinnedMismatch(t *testing.T) {
	addr, fp, requests := tlsServer(t)
	const want = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	c := newClient([]Option{WithCertificateFingerprint(want)})
	err := c.connect(context.Background(), addr)
	mismatch, ok := errors.AsType[*CertificateMismatchError](err)
	if !ok {
		t.Fatalf("got %v, want a CertificateMismatchError", err)
	}
	if mismatch.Want != want || mismatch.Got != fp {
		t.Fatalf("got %+v, want Want %q, Got %q", mismatch, want, fp)
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("server got %d requests after a mismatch", n)
	}
}
