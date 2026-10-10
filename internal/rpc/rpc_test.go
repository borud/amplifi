package rpc

import (
	"context"
	"strings"
	"testing"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/wire"
)

// fakeCaller answers Call from a table and records what each method was sent.
// A method missing from results fails with a router error.
type fakeCaller struct {
	results map[string]any
	sent    map[string]any
}

func (f *fakeCaller) Call(_ context.Context, _, method string, arg any) (any, error) {
	if f.sent == nil {
		f.sent = map[string]any{}
	}
	f.sent[method] = arg
	v, ok := f.results[method]
	if !ok {
		return nil, &amplifi.RouterError{Code: 6, Msg: method}
	}
	return v, nil
}

func (f *fakeCaller) CallJSON(ctx context.Context, iface, method string, arg any) (any, error) {
	return f.Call(ctx, iface, method, arg)
}

func TestUpdateMergesAndFlattens(t *testing.T) {
	f := &fakeCaller{results: map[string]any{
		"GetX": map[string]any{"1": "a", "2": true, "9": map[string]any{"nested": 1}},
		"SetX": true,
	}}

	err := Update(context.Background(), f, amplifi.IfaceDevice, "GetX", Null, "SetX", map[string]any{"2": false})
	if err != nil {
		t.Fatal(err)
	}
	sent := f.sent["SetX"].(map[string]any)
	if sent["1"] != "a" || sent["2"] != false {
		t.Fatalf("unexpected payload: %v", sent)
	}
	if _, ok := sent["9"]; ok {
		t.Fatal("nested map was sent back")
	}
}

func TestUpdateFailedGetterSkipsSetter(t *testing.T) {
	f := &fakeCaller{results: map[string]any{"SetX": true}}

	err := Update(context.Background(), f, amplifi.IfaceDevice, "GetX", Null, "SetX", map[string]any{"1": 1})
	if !amplifi.IsRouterError(err) {
		t.Fatalf("err = %v, want the getter's router error", err)
	}
	if _, called := f.sent["SetX"]; called {
		t.Fatal("setter called after the getter failed")
	}
}

func TestSetResult(t *testing.T) {
	ctx := context.Background()

	f := &fakeCaller{results: map[string]any{"SetX": false}}
	if err := Set(ctx, f, amplifi.IfaceDevice, "SetX", map[string]any{}); err == nil {
		t.Fatal("false result not reported")
	}

	f = &fakeCaller{results: map[string]any{"SetX": nil}}
	if err := Set(ctx, f, amplifi.IfaceDevice, "SetX", map[string]any{}); err != nil {
		t.Fatalf("nil result: %v", err)
	}
}

func TestUpdateEntry(t *testing.T) {
	ctx := context.Background()
	entries := func() map[string]any {
		return map[string]any{
			"wifi0": map[string]any{"1": "wifi0", "2": true},
			"wifi1": map[string]any{"1": "wifi1", "2": true, "x": map[string]any{"runtime": 1}},
		}
	}

	f := &fakeCaller{results: map[string]any{"GetR": entries(), "SetR": true}}
	if err := UpdateEntry(ctx, f, amplifi.IfaceDevice, "GetR", nil, "SetR", "wifi1", map[string]any{"2": false}); err != nil {
		t.Fatal(err)
	}
	sent := f.sent["SetR"].(map[string]any)
	w1 := sent["wifi1"].(map[string]any)
	if w1["2"] != false || w1["1"] != "wifi1" {
		t.Fatalf("wifi1 = %v", w1)
	}
	if _, ok := w1["x"]; ok {
		t.Fatalf("nested map was sent back: %v", w1)
	}
	if sent["wifi0"].(map[string]any)["2"] != true {
		t.Fatalf("wifi0 changed: %v", sent["wifi0"])
	}

	f = &fakeCaller{results: map[string]any{"GetR": entries(), "SetR": true}}
	err := UpdateEntry(ctx, f, amplifi.IfaceDevice, "GetR", nil, "SetR", "wifi9", map[string]any{"2": false})
	if err == nil || !strings.Contains(err.Error(), "wifi0, wifi1") {
		t.Fatalf("missing entry: err = %v, want the available keys", err)
	}
	if _, called := f.sent["SetR"]; called {
		t.Fatal("setter called for a missing entry")
	}

	f = &fakeCaller{}
	if err := UpdateEntry(ctx, f, amplifi.IfaceDevice, "GetR", nil, "SetR", "wifi0", nil); err != nil || len(f.sent) != 0 {
		t.Fatalf("empty changes: err %v, calls %v", err, f.sent)
	}
}

func TestUpdateClusterWraps(t *testing.T) {
	f := &fakeCaller{results: map[string]any{
		"GetSiteConfig": []any{map[string]any{"1": int64(1)}, map[string]any{"12": "UTC"}},
		"SetSiteConfig": true,
	}}
	err := UpdateCluster(context.Background(), f, "GetSiteConfig", "SetSiteConfig", map[string]any{"12": "America/Chicago"})
	if err != nil {
		t.Fatal(err)
	}

	raw, ok := f.sent["SetSiteConfig"].(amplifi.RawMsgpack)
	if !ok {
		t.Fatalf("cluster setter arg is %T", f.sent["SetSiteConfig"])
	}
	v, err := wire.DecodeMsgpack(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, err := wire.ClusterNodePayload(v)
	if err != nil || p["12"] != "America/Chicago" {
		t.Fatalf("payload = %v, %v", p, err)
	}
}

func TestDecodeJSONString(t *testing.T) {
	var out struct {
		Status int `json:"inet_status"`
	}
	if err := Decode("InetCheck", `{"inet_status":3}`, &out); err != nil || out.Status != 3 {
		t.Fatalf("got %+v, %v", out, err)
	}
}
