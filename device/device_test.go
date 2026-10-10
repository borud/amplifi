package device

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/borud/amplifi"
)

func TestParseMACs(t *testing.T) {
	m := map[string]any{
		"11": []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0b},
		"2":  []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c},
		"3":  []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0d},
		"99": []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0e},
		"4":  "not binary",
	}
	macs := parseMACs(m)

	want := []struct{ role, mac string }{
		{"internal 2.4GHz", "00:00:5e:00:53:0c"},
		{"internal 5GHz", "00:00:5e:00:53:0d"},
		{"lan", "00:00:5e:00:53:0b"},
		{"#99", "00:00:5e:00:53:0e"},
	}
	if len(macs) != len(want) {
		t.Fatalf("got %d MACs, want %d: %+v", len(macs), len(want), macs)
	}
	for i, w := range want {
		if macs[i].Role != w.role || macs[i].MAC.String() != w.mac {
			t.Errorf("mac %d: got %s %s, want %s %s", i, macs[i].Role, macs[i].MAC, w.role, w.mac)
		}
	}

	b, err := json.Marshal(macs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"role":"internal 2.4GHz","mac":"00:00:5e:00:53:0c"}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
}

func TestParseConfig(t *testing.T) {
	cfg := parseConfig(map[string]any{
		"1":  true,
		"3":  int8(14),
		"4":  "router",
		"10": uint8(2),
		"11": "US",
		"15": int8(1),
		"17": int8(3),
		"99": "extra",
	})
	if !cfg.InitialSetupDone || cfg.FirmwareUpgradeAvailable || cfg.ProtocolVersion != 14 ||
		cfg.FriendlyName != "router" || cfg.BoardRevision != 2 || cfg.Country != "US" ||
		cfg.Role != amplifi.RoleRouter || cfg.ConnectivityMode != "wired" || cfg.Fields["99"] != "extra" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	if cfg := parseConfig(map[string]any{"15": int8(2)}); cfg.Role != amplifi.RoleMeshPoint {
		t.Fatalf("mesh point role: %q", cfg.Role)
	}
	if cfg := parseConfig(map[string]any{}); cfg.Role != "" || cfg.ConnectivityMode != "" {
		t.Fatalf("missing options should be empty: %+v", cfg)
	}
}

func TestParseDisplay(t *testing.T) {
	d := parseDisplay(map[string]any{
		"1": int8(50), "2": int8(10), "3": uint8(80), "4": int8(20), "5": int8(1),
		"6": int16(300), "7": int16(600), "8": int16(1320), "9": int16(420), "10": true,
	})
	want := Display{
		SystemSoundVolume: 50, TapSoundVolume: 10, LCDBrightness: 80, LEDBrightness: 20, ClockType: 1,
		ClockTimeout: 300, OffTimeout: 600, NightStart: 22 * time.Hour, NightEnd: 7 * time.Hour, NightMode: true,
	}
	if *d != want {
		t.Fatalf("got %+v, want %+v", *d, want)
	}

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"night_start":79200`) || !strings.Contains(string(b), `"night_end":25200`) {
		t.Fatalf("night times not in seconds: %s", b)
	}
}

func TestDisplayUpdateOptions(t *testing.T) {
	lcd, off, start := 40, false, 21*time.Hour+30*time.Minute+59*time.Second
	changes := DisplayUpdate{LCDBrightness: &lcd, NightMode: &off, NightStart: &start}.options()
	if len(changes) != 3 || changes["3"] != 40 || changes["10"] != false || changes["8"] != int64(1290) {
		t.Fatalf("unexpected changes: %v", changes)
	}
	if len(DisplayUpdate{}.options()) != 0 {
		t.Fatal("empty update should have no changes")
	}
}

func TestHealth(t *testing.T) {
	var c InternetCheck
	if err := json.Unmarshal([]byte(`{"inet_status":3,"ipv6_inet_status":1}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.Health != HealthGreen || c.IPv6Health != HealthRed || Health(9).String() != "#9" {
		t.Fatalf("got %v %v", c.Health, c.IPv6Health)
	}
	if h, err := ParseHealth("Yellow"); err != nil || h != HealthYellow {
		t.Fatalf("parse: %v, %v", h, err)
	}
	if _, err := ParseHealth("blue"); err == nil {
		t.Fatal("unknown health accepted")
	}
}
