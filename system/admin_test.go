package system

import (
	"bytes"
	"testing"
	"time"

	"github.com/borud/amplifi/internal/wire"
)

func TestAdminPasswordArgLayout(t *testing.T) {
	arg, err := wire.ClusterNodeArg(adminUsersPayload("hunter2"))
	if err != nil {
		t.Fatal(err)
	}

	// [ {2: <µs timestamp>}, {"admin": {2: 1, 3: "hunter2"}} ], with the
	// timestamp packed as a 64-bit integer.
	prefix := []byte{0x92, 0x81, 0x02}
	suffix := append([]byte{0x81, 0xa5}, "admin"...)
	suffix = append(suffix, 0x82, 0x02, 0x01, 0x03, 0xa7)
	suffix = append(suffix, "hunter2"...)

	if !bytes.HasPrefix(arg, prefix) || !bytes.HasSuffix(arg, suffix) {
		t.Fatalf("layout: % x", arg)
	}
	if n := len(arg) - len(prefix) - len(suffix); n != 9 {
		t.Fatalf("timestamp is %d bytes, want 9: % x", n, arg)
	}

	v, err := wire.DecodeMsgpack(arg)
	if err != nil {
		t.Fatal(err)
	}
	meta := v.([]any)[0].(map[string]any)
	ts, ok := wire.ToInt64(meta["2"])
	if !ok || time.Since(time.UnixMicro(ts)).Abs() > time.Minute {
		t.Fatalf("timestamp %v is not now in µs", meta["2"])
	}
}
