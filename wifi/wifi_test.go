package wifi

import (
	"fmt"
	"slices"
	"testing"

	"github.com/borud/amplifi/internal/rpc"
)

func TestParseWifiConfig(t *testing.T) {
	m := map[string]any{
		"3": true, "5": "home", "8": "secret", "11": int8(6), "22": false,
		"4": true, "7": "visitors", "10": "guestpw", "13": uint8(4), "23": true,
		"61": true, "63": "things", "64": int8(4),
		"14": true, "16": int8(2), "18": "US", "19": int8(0), "20": int16(36),
		"26": int16(44), "38": true, "39": int8(1), "36": false,
	}
	w := parseConfig(m)

	if w.Main.SSID != "home" || !w.Main.Enabled || w.Main.Encryption != EncryptionWPA3 {
		t.Fatalf("main = %+v", w.Main)
	}
	if w.Guest.SSID != "visitors" || !w.Guest.Hidden || w.Guest.Encryption != EncryptionWPA2 {
		t.Fatalf("guest = %+v", w.Guest)
	}
	if w.IoT.SSID != "things" || w.IoT.Password != "" {
		t.Fatalf("iot = %+v", w.IoT)
	}
	if !w.BandSteering || w.ActiveBand != "5GHz" || w.Channel5 != 36 || w.RuntimeChannel5 != 44 {
		t.Fatalf("config = %+v", w)
	}
	if !w.FastRoaming || w.PMF.String() != "optional" {
		t.Fatalf("802.11 = %v %v", w.FastRoaming, w.PMF)
	}

	r := w.Redacted()
	if r.Main.Password != rpc.Redacted || r.Fields["8"] != rpc.Redacted || r.Fields["10"] != rpc.Redacted {
		t.Fatalf("passwords not redacted: %+v", r)
	}
	if w.Main.Password != "secret" || w.Fields["8"] != "secret" {
		t.Fatalf("Redacted changed the original: %+v", w)
	}
}

func TestConfigUpdateChanges(t *testing.T) {
	ssid := "new"
	enc, err := ParseEncryption("WPA2-WPA3")
	if err != nil {
		t.Fatal(err)
	}
	hidden := true
	band := "2.4"
	ch := 0

	got, err := ConfigUpdate{
		Main:       NetworkUpdate{SSID: &ssid, Encryption: &enc},
		Guest:      NetworkUpdate{Hidden: &hidden},
		ActiveBand: &band,
		Channel24:  &ch,
	}.changes()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"5": "new", "11": int64(5), "23": true, "16": int64(1), "19": int64(0)}
	if len(got) != len(want) {
		t.Fatalf("changes = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("changes[%s] = %#v, want %#v", k, got[k], v)
		}
	}

	pw := "x"
	if _, err := (ConfigUpdate{IoT: NetworkUpdate{Password: &pw}}).changes(); err == nil {
		t.Fatal("iot password accepted")
	}
	if _, err := ParseEncryption("rot13"); err == nil {
		t.Fatal("bad encryption accepted")
	}
	if e, _ := ParseEncryption("psk2"); e != EncryptionWPA2 {
		t.Fatalf("alias psk2 = %v", e)
	}
	if b, _ := ParseBandwidth("80MHz"); b.String() != "80" {
		t.Fatalf("bandwidth = %v", b)
	}
}

func TestParseWifiRadios(t *testing.T) {
	m := map[string]any{
		"wifi0": map[string]any{"1": "wifi0", "2": true, "4": int8(6), "6": int8(1), "5": int8(4), "7": int8(3)},
		"wifi1": map[string]any{"1": "wifi1", "2": true, "6": int8(2), "5": int8(5)},
	}
	radios := parseRadios(m)
	if len(radios) != 2 || radios[0].Name != "wifi0" || radios[0].Band != "2.4GHz" || radios[0].Mode != "n" || radios[0].Bandwidth.String() != "40" {
		t.Fatalf("radios = %+v", radios)
	}
}

func TestParseWifiInterfaces(t *testing.T) {
	m := map[string]any{
		"ath0": map[string]any{
			"1": "ath0", "3": true, "4": []byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x0c},
			"6": "home", "7": "secret", "8": int8(4), "9": int8(1), "10": int8(3),
		},
	}
	ifs := parseInterfaces(m)
	if len(ifs) != 1 {
		t.Fatalf("interfaces = %+v", ifs)
	}
	i := ifs[0]
	if i.MAC.String() != "00:00:5e:00:53:0c" || i.Network != "main" || i.OperationMode != "ap" || i.Encryption != EncryptionWPA2 {
		t.Fatalf("interface = %+v", i)
	}
	r := i.Redacted()
	if r.Password != rpc.Redacted || r.Fields["7"] != rpc.Redacted || i.Fields["7"] != "secret" {
		t.Fatalf("not redacted: %+v", r)
	}
}

func TestWifiListsAndScan(t *testing.T) {
	if got := ints([]any{int8(1), int16(36), "x"}); !slices.Equal(got, []int{1, 36}) {
		t.Fatalf("ints = %v", got)
	}
	if got := countryCodes("NOSEUS"); !slices.Equal(got, []string{"NO", "SE", "US"}) {
		t.Fatalf("countries = %v", got)
	}

	scan := parseScan(map[string]any{
		"2": map[string]any{"3": map[string]any{"\x00\x00\x5e\x00\x53\x0c": int8(-60)}},
		"1": map[string]any{"4": map[string]any{"\x00\x01\x02\x03\x04\x05": int8(-70)}},
	})
	want := []string{"2.4GHz guest 00:01:02:03:04:05 -70", "5GHz main 00:00:5e:00:53:0c -60"}
	got := make([]string, len(scan))
	for i, e := range scan {
		got[i] = fmt.Sprintf("%s %s %s %d", e.Band, e.Network, e.MAC, e.Quality)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("scan = %v", got)
	}
}

func TestMACValueAndPMF(t *testing.T) {
	if got := macValue("00:00:5e:00:53:0c").String(); got != "00:00:5e:00:53:0c" {
		t.Fatalf("text MAC = %q", got)
	}
	if got := macValue([]byte{0, 1, 2, 3, 4, 5}).String(); got != "00:01:02:03:04:05" {
		t.Fatalf("binary MAC = %q", got)
	}
	if got := macValue(nil); got != nil {
		t.Fatalf("missing MAC = %v", got)
	}
	if p, err := ParsePMF("Disabled"); err != nil || p != PMFOff {
		t.Fatalf("pmf = %v, %v", p, err)
	}
}

func TestChannelList(t *testing.T) {
	var m map[string][]channelEntry
	raw := `{"5g": [{"chan": 100, "mhz": 5500, "dfs_state": 2}, {"chan": 36, "mhz": 5180}], "2g": [{"chan": 1, "mhz": 2412}]}`
	if err := rpc.Decode("GetWifiChannelsInfo", raw, &m); err != nil {
		t.Fatal(err)
	}

	got := channelList(m)
	want := []string{"2g/1/2412", "5g/36/5180", "5g/100/5500"}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i, ch := range got {
		if s := fmt.Sprintf("%s/%d/%d", ch.Band, ch.Number, ch.FreqMHz); s != want[i] {
			t.Errorf("channel %d = %s, want %s", i, s, want[i])
		}
	}
	if got[2].DFSState == nil || *got[2].DFSState != 2 || got[1].DFSState != nil {
		t.Errorf("DFS state: %+v", got[1:])
	}
	if empty := channelList(nil); empty == nil {
		t.Error("nil list")
	}
}
