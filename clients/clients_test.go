package clients

import (
	"maps"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/borud/amplifi"
)

var (
	clientsTestUnit = string([]byte{0x02, 0, 0, 0, 0, 0x01})
	clientsTestMAC  = string([]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01})
)

func TestClientsParseAssociationsWithUnits(t *testing.T) {
	m := map[string]any{
		clientsTestUnit: map[string]any{
			"2": map[string]any{
				"3": map[string]any{
					clientsTestMAC: map[string]any{
						"2":  int8(5),
						"3":  int8(-55),
						"15": []byte{192, 0, 2, 42},
						"17": "phone",
						"18": []byte("Pixel"),
						"19": uint32(866700),
						"20": uint32(433300),
					},
				},
			},
		},
	}

	got := parseAssociations(m)
	if len(got) != 1 {
		t.Fatalf("got %d clients, want 1", len(got))
	}
	w := got[0]
	if w.MAC.String() != "aa:bb:cc:dd:ee:01" || w.Unit.String() != "02:00:00:00:00:01" {
		t.Errorf("mac %s unit %s", w.MAC, w.Unit)
	}
	if w.Band != "5GHz" || w.Network != "main" || w.Mode != "802.11ac" {
		t.Errorf("band %q network %q mode %q", w.Band, w.Network, w.Mode)
	}
	if w.Signal != -55 || w.RxBitrate != 866700 || w.TxBitrate != 433300 {
		t.Errorf("signal %d rx %d tx %d", w.Signal, w.RxBitrate, w.TxBitrate)
	}
	if w.IP != netip.MustParseAddr("192.0.2.42") || w.HostName != "phone" || w.Description != "Pixel" {
		t.Errorf("ip %s host %q desc %q", w.IP, w.HostName, w.Description)
	}
}

func TestClientsParseAssociationsWithoutUnits(t *testing.T) {
	m := map[string]any{
		"1": map[string]any{
			"4": map[string]any{clientsTestMAC: map[string]any{"3": int8(40)}},
		},
	}

	got := parseAssociations(m)
	if len(got) != 1 {
		t.Fatalf("got %d clients, want 1", len(got))
	}
	if got[0].Unit != nil || got[0].Band != "2.4GHz" || got[0].Network != "guest" || got[0].Signal != 40 {
		t.Errorf("unexpected %+v", got[0])
	}
}

func TestClientsParseSignals(t *testing.T) {
	m := map[string]any{
		clientsTestUnit: map[string]any{
			"2": map[string]any{"3": map[string]any{clientsTestMAC: uint8(70), "short": uint8(1)}},
		},
	}

	got := parseSignals(m)
	if len(got) != 1 {
		t.Fatalf("got %d signals, want 1", len(got))
	}
	if got[0].Quality != 70 || got[0].Band != "5GHz" || got[0].Unit.String() != "02:00:00:00:00:01" {
		t.Errorf("unexpected %+v", got[0])
	}
}

func TestClientsCurrent(t *testing.T) {
	info := map[string]map[string]any{
		"aa:bb:cc:dd:ee:01": {"connection": "wireless", "custom_info": map[string]any{"fingerprint": map[string]any{"device_id": float64(3900)}},
			"description": "Camera", "host_name": "cam1", "icon_id": float64(3900), "ip": "192.0.2.22", "peer": "02:00:00:00:00:01"},
		"aa:bb:cc:dd:ee:02": {"connection": "ethernet", "host_name": "laptop", "icon_id": float64(3798), "ip": "192.0.2.23", "port": float64(0)},
		"aa:bb:cc:dd:ee:03": {"connection": "ethernet", "port": float64(1)},
		"aa:bb:cc:dd:ee:04": {"connection": "wireless", "peer": "02:00:00:00:00:99"},
		"aa:bb:cc:dd:ee:05": {"connection": "wireless"},
	}
	custom := map[string]map[string]any{
		"aa:bb:cc:dd:ee:05": {"fingerprint": map[string]any{"device_id": float64(1)}},
	}
	router := amplifi.MAC{0x02, 0, 0, 0, 0, 0xff}
	assocs := []Association{
		{MAC: amplifi.MAC(clientsTestMAC), Unit: amplifi.MAC(clientsTestUnit), Band: "5GHz", Network: "main"},
		{MAC: amplifi.MAC{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x05}, Unit: router, Band: "2.4GHz", Network: "iot"},
		{MAC: amplifi.MAC{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x06}, Unit: router, Band: "2.4GHz", Network: "guest", HostName: "tv"},
	}
	units := map[string]string{"02:00:00:00:00:01": "hall", "": "router"}

	got := current(info, custom, assocs, units)
	if len(got) != 6 {
		t.Fatalf("got %d clients, want 6: %+v", len(got), got)
	}
	byMAC := map[string]Client{}
	for _, n := range got {
		byMAC[n.MAC.String()] = n
	}

	cam := byMAC["aa:bb:cc:dd:ee:01"]
	if cam.Name != "Camera" || cam.HostName != "cam1" || cam.Connection != "5GHz" || cam.UnitName != "hall" || !cam.Online {
		t.Errorf("cam: %+v", cam)
	}
	if cam.IP != netip.MustParseAddr("192.0.2.22") || cam.IconID != 3900 || cam.Custom == nil || cam.WiFi == nil {
		t.Errorf("cam: ip %s icon %d custom %v wifi %v", cam.IP, cam.IconID, cam.Custom, cam.WiFi)
	}

	mac := byMAC["aa:bb:cc:dd:ee:02"]
	if mac.Name != "laptop" || mac.Connection != "wired" || mac.Port == nil || *mac.Port != 0 || mac.UnitName != "router" {
		t.Errorf("laptop: %+v", mac)
	}

	bare := byMAC["aa:bb:cc:dd:ee:03"]
	if bare.Name != "aa:bb:cc:dd:ee:03" || bare.Port == nil || *bare.Port != 1 {
		t.Errorf("bare wired: %+v", bare)
	}

	if n := byMAC["aa:bb:cc:dd:ee:04"]; n.Connection != "wifi" || n.UnitName != "router" {
		t.Errorf("unknown peer: %+v", n)
	}

	iot := byMAC["aa:bb:cc:dd:ee:05"]
	if iot.Connection != "2.4GHz" || iot.Unit != nil || iot.UnitName != "router" || iot.Custom == nil {
		t.Errorf("router client: %+v", iot)
	}

	if tv := byMAC["aa:bb:cc:dd:ee:06"]; tv.Name != "tv" || tv.Network != "guest" || !tv.Online {
		t.Errorf("wifi only: %+v", tv)
	}
}

func TestClientsEntryMap(t *testing.T) {
	want := "00:00:5e:00:53:3c"
	for _, v := range []any{
		map[string]any{want: map[string]any{"fingerprint": map[string]any{"device_id": float64(3900)}}},
		`{"00:00:5e:00:53:3c": {"fingerprint": {"device_id": 3900}}}`,
	} {
		m, err := entryMap("test", v)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := m[want]; !ok || len(m) != 1 {
			t.Errorf("entryMap(%v) = %v", v, m)
		}
	}

	if m, err := entryMap("test", nil); err != nil || len(m) != 0 {
		t.Errorf("nil: %v %v", m, err)
	}
	if _, err := entryMap("test", []any{}); err == nil {
		t.Error("expected an error for a list")
	}
}

func TestClientsHistory(t *testing.T) {
	hist := map[string]map[string]any{
		"aa:bb:cc:dd:ee:01": {"connection": "ethernet", "last_seen": float64(1791593475), "times_seen": float64(1212460)},
		"aa:bb:cc:dd:ee:02": {"connection": "wireless", "last_seen": float64(1791593000), "times_seen": float64(5)},
		"aa:bb:cc:dd:ee:03": {"connection": "ethernet", "host_name": "desktop-01", "last_seen": float64(1791590000), "times_seen": float64(7)},
	}
	current := map[string]map[string]any{
		"aa:bb:cc:dd:ee:02": {"connection": "wireless"},
	}

	got := history(hist, current)
	if len(got) != 3 {
		t.Fatalf("got %d clients, want 3", len(got))
	}
	byMAC := map[string]Client{}
	for _, n := range got {
		byMAC[n.MAC.String()] = n
	}

	wired := byMAC["aa:bb:cc:dd:ee:01"]
	if wired.Name != "aa:bb:cc:dd:ee:01" || wired.Connection != "wired" || wired.Online || wired.UnitName != "" {
		t.Errorf("wired: %+v", wired)
	}
	if !wired.LastSeen.Equal(time.Unix(1791593475, 0)) || wired.TimesSeen != 1212460 {
		t.Errorf("wired last seen %v times %d", wired.LastSeen, wired.TimesSeen)
	}
	if w := byMAC["aa:bb:cc:dd:ee:02"]; w.Connection != "wifi" || !w.Online {
		t.Errorf("wifi: %+v", w)
	}
	if h := byMAC["aa:bb:cc:dd:ee:03"]; h.Name != "desktop-01" {
		t.Errorf("named: %+v", h)
	}
}

func TestClientsUnitName(t *testing.T) {
	units := map[string]string{"02:00:00:00:00:01": "hall", "": "router"}
	tests := []struct {
		unit amplifi.MAC
		want string
	}{
		{amplifi.MAC(clientsTestUnit), "hall"},
		{amplifi.MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c}, "router"},
		{nil, "router"},
	}
	for _, tt := range tests {
		if got := unitName(units, tt.unit); got != tt.want {
			t.Errorf("unitName(%s) = %q, want %q", tt.unit, got, tt.want)
		}
	}
}

func TestDescend(t *testing.T) {
	m := map[string]any{
		"1": map[string]any{"3": int8(2), "4": int8(1)},
		"2": map[string]any{"3": int8(3)},
		"9": "not a map",
	}

	got := map[string]any{}
	for _, p := range descend([]path{{v: m}}, 2) {
		got[strings.Join(p.keys, "/")] = p.v
	}
	want := map[string]any{"1/3": int8(2), "1/4": int8(1), "2/3": int8(3)}
	if !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestApplyFieldsUnknownConnection(t *testing.T) {
	var nc Client
	applyFields(&nc, map[string]any{"connection": "plc"})
	if nc.Connection != "plc" {
		t.Errorf("connection %q", nc.Connection)
	}
}
