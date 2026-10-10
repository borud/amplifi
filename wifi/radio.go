package wifi

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
	"github.com/borud/amplifi/internal/wire"
)

// WifiRadioConfigOption codes.
const (
	radioDevice           = "1"
	radioEnabled          = "2"
	radioUp               = "3"
	radioChannel          = "4"
	radioMode             = "5"
	radioBand             = "6"
	radioBandwidth        = "7"
	radioCountry          = "8"
	radioRuntimeChannel   = "9"
	radioLegacy11b        = "10"
	radioMaxBandwidth     = "12"
	radioDFSSupported     = "13"
	radioRuntimeBandwidth = "14"
)

// WifiInterfaceConfigOption codes.
const (
	ifName           = "1"
	ifDevice         = "2"
	ifEnabled        = "3"
	ifMAC            = "4"
	ifBSSID          = "5"
	ifSSID           = "6"
	ifPassword       = "7"
	ifEncryption     = "8"
	ifOperationMode  = "9"
	ifRole           = "10"
	ifBandSteering   = "11"
	ifRouterSteering = "12"
)

// Radio is one radio of the unit. Channel ChannelAuto (or 0) means
// automatic.
type Radio struct {
	Name             string         `json:"name"`
	Device           string         `json:"device"`
	Enabled          bool           `json:"enabled"`
	Up               bool           `json:"up"`
	Band             string         `json:"band"`
	Mode             string         `json:"mode"`
	Channel          int            `json:"channel"`
	RuntimeChannel   int            `json:"runtime_channel"`
	Bandwidth        Bandwidth      `json:"bandwidth"`
	MaxBandwidth     Bandwidth      `json:"max_bandwidth"`
	RuntimeBandwidth Bandwidth      `json:"runtime_bandwidth"`
	Country          string         `json:"country"`
	Legacy11b        bool           `json:"legacy_11b"`
	DFSSupported     bool           `json:"dfs_supported"`
	Fields           map[string]any `json:"fields"`
}

// RadioUpdate changes a radio. Nil fields are left as they are.
type RadioUpdate struct {
	Enabled   *bool
	Channel   *int
	Bandwidth *Bandwidth
	Country   *string
	Legacy11b *bool
}

// Radios lists the unit's radios (Device.GetWifiRadios), sorted by name.
func Radios(ctx context.Context, c *amplifi.Client) ([]Radio, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiRadios", nil)
	if err != nil {
		return nil, fmt.Errorf("wifi radios: %w", err)
	}
	return parseRadios(m), nil
}

func parseRadios(m map[string]any) []Radio {
	radios := make([]Radio, 0, len(m))
	for _, key := range slices.Sorted(maps.Keys(m)) {
		r, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		radios = append(radios, Radio{
			Name:             key,
			Device:           rpc.String(r, radioDevice),
			Enabled:          rpc.Bool(r, radioEnabled),
			Up:               rpc.Bool(r, radioUp),
			Band:             rpc.Name(rpc.BandNames, r, radioBand),
			Mode:             rpc.Name(modeNames, r, radioMode),
			Channel:          int(rpc.Int(r, radioChannel)),
			RuntimeChannel:   int(rpc.Int(r, radioRuntimeChannel)),
			Bandwidth:        Bandwidth(rpc.Int(r, radioBandwidth)),
			MaxBandwidth:     Bandwidth(rpc.Int(r, radioMaxBandwidth)),
			RuntimeBandwidth: Bandwidth(rpc.Int(r, radioRuntimeBandwidth)),
			Country:          rpc.String(r, radioCountry),
			Legacy11b:        rpc.Bool(r, radioLegacy11b),
			DFSSupported:     rpc.Bool(r, radioDFSSupported),
			Fields:           r,
		})
	}
	return radios
}

// UpdateRadio changes the radio called name with a read-modify-write of
// Device.GetWifiRadios / SetWifiRadios. The whole radio map is written back,
// keyed by radio name, in the getter's shape; that argument shape is
// untested.
func UpdateRadio(ctx context.Context, c *amplifi.Client, name string, u RadioUpdate) error {
	changes := map[string]any{}
	rpc.Put(changes, radioEnabled, u.Enabled)
	rpc.PutInt(changes, radioChannel, u.Channel)
	rpc.PutInt(changes, radioBandwidth, u.Bandwidth)
	rpc.Put(changes, radioCountry, u.Country)
	rpc.Put(changes, radioLegacy11b, u.Legacy11b)
	if err := rpc.UpdateEntry(ctx, c, amplifi.IfaceDevice, "GetWifiRadios", nil, "SetWifiRadios", name, changes); err != nil {
		return fmt.Errorf("update radio %q: %w", name, err)
	}
	return nil
}

// Interface is one Wi-Fi interface (a network on a radio).
type Interface struct {
	Key            string         `json:"key"`
	Name           string         `json:"name"`
	Device         string         `json:"device"`
	Enabled        bool           `json:"enabled"`
	MAC            amplifi.MAC    `json:"mac,omitempty"`
	BSSID          amplifi.MAC    `json:"bssid,omitempty"`
	SSID           string         `json:"ssid"`
	Password       string         `json:"password,omitempty"`
	Encryption     Encryption     `json:"encryption"`
	OperationMode  string         `json:"operation_mode"`
	Network        string         `json:"network"`
	BandSteering   bool           `json:"band_steering"`
	RouterSteering bool           `json:"router_steering"`
	Fields         map[string]any `json:"fields"`
}

// Redacted returns a copy of i with the password masked, including in
// Fields.
func (i Interface) Redacted() Interface {
	i.Password = rpc.Mask(i.Password)
	i.Fields = rpc.MaskFields(i.Fields, ifPassword)
	return i
}

// InterfaceUpdate changes an interface. Nil fields are left as they are.
type InterfaceUpdate struct {
	Enabled        *bool
	SSID           *string
	Password       *string
	Encryption     *Encryption
	BandSteering   *bool
	RouterSteering *bool
}

// Interfaces lists the unit's Wi-Fi interfaces (Device.GetWifiInterfaces),
// sorted by key. They include the passwords; see Interface.Redacted.
func Interfaces(ctx context.Context, c *amplifi.Client) ([]Interface, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiInterfaces", nil)
	if err != nil {
		return nil, fmt.Errorf("wifi interfaces: %w", err)
	}
	return parseInterfaces(m), nil
}

func parseInterfaces(m map[string]any) []Interface {
	ifaces := make([]Interface, 0, len(m))
	for _, key := range slices.Sorted(maps.Keys(m)) {
		f, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		ifaces = append(ifaces, Interface{
			Key:            key,
			Name:           rpc.String(f, ifName),
			Device:         rpc.String(f, ifDevice),
			Enabled:        rpc.Bool(f, ifEnabled),
			MAC:            macValue(f[ifMAC]),
			BSSID:          macValue(f[ifBSSID]),
			SSID:           rpc.String(f, ifSSID),
			Password:       rpc.String(f, ifPassword),
			Encryption:     Encryption(rpc.Int(f, ifEncryption)),
			OperationMode:  rpc.Name(opModeNames, f, ifOperationMode),
			Network:        rpc.Name(rpc.NetworkNames, f, ifRole),
			BandSteering:   rpc.Bool(f, ifBandSteering),
			RouterSteering: rpc.Bool(f, ifRouterSteering),
			Fields:         f,
		})
	}
	return ifaces
}

// macValue converts a MAC that is binary (6 bytes) or text, as units have
// reported the BSSID as text; anything else yields nil.
func macValue(v any) amplifi.MAC {
	if s, ok := v.(string); ok && len(s) != 6 {
		mac, _ := amplifi.ParseMAC(s)
		return mac
	}
	return rpc.MAC(v)
}

// UpdateInterface changes the interface with the given key with a
// read-modify-write of Device.GetWifiInterfaces / SetWifiInterfaces. As with
// UpdateRadio, the argument shape is untested.
func UpdateInterface(ctx context.Context, c *amplifi.Client, key string, u InterfaceUpdate) error {
	changes := map[string]any{}
	rpc.Put(changes, ifEnabled, u.Enabled)
	rpc.Put(changes, ifSSID, u.SSID)
	rpc.Put(changes, ifPassword, u.Password)
	rpc.PutInt(changes, ifEncryption, u.Encryption)
	rpc.Put(changes, ifBandSteering, u.BandSteering)
	rpc.Put(changes, ifRouterSteering, u.RouterSteering)
	if err := rpc.UpdateEntry(ctx, c, amplifi.IfaceDevice, "GetWifiInterfaces", nil, "SetWifiInterfaces", key, changes); err != nil {
		return fmt.Errorf("update interface %q: %w", key, err)
	}
	return nil
}

// AllowedChannels lists the channels the unit can use
// (Device.GetWifiChannels).
func AllowedChannels(ctx context.Context, c *amplifi.Client) ([]int, error) {
	v, err := c.Call(ctx, amplifi.IfaceDevice, "GetWifiChannels", nil)
	if err != nil {
		return nil, fmt.Errorf("wifi channels: %w", err)
	}
	return ints(v), nil
}

// ints converts a decoded msgpack list of integers, skipping anything else.
// A nil or non-list value gives an empty list.
func ints(v any) []int {
	list, _ := v.([]any)
	out := make([]int, 0, len(list))
	for _, item := range list {
		if n, ok := wire.ToInt64(item); ok {
			out = append(out, int(n))
		}
	}
	return out
}

// Channel describes one channel of a band: its frequency, transmit power,
// width and DFS state. The DFS fields are nil when
// the unit leaves them out; DFSTime and DFSCACTime are the router's raw
// values and their units are unconfirmed.
type Channel struct {
	Band       string `json:"band"`
	Number     int    `json:"number"`
	FreqMHz    int    `json:"freq_mhz"`
	TxPower    int    `json:"tx_power"`
	Width      int    `json:"width"`
	Radar      bool   `json:"radar"`
	DFSState   *int   `json:"dfs_state,omitempty"`
	DFSTime    *int   `json:"dfs_time,omitempty"`
	DFSCACTime *int   `json:"dfs_cac_time,omitempty"`
}

// channelEntry is one channel as GetWifiChannelsInfo sends it.
type channelEntry struct {
	Channel    int  `json:"chan"`
	Freq       int  `json:"mhz"`
	Power      int  `json:"tx_power"`
	Width      int  `json:"width"`
	Radar      bool `json:"radar"`
	DFSState   *int `json:"dfs_state"`
	DFSTime    *int `json:"dfs_time"`
	DFSCACTime *int `json:"dfs_cac_time"`
}

// Channels fetches the details of every channel (Device.GetWifiChannelsInfo,
// a JSON answer), sorted by band and channel number.
func Channels(ctx context.Context, c *amplifi.Client) ([]Channel, error) {
	var m map[string][]channelEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceDevice, "GetWifiChannelsInfo", nil, &m); err != nil {
		return nil, fmt.Errorf("wifi channel info: %w", err)
	}
	return channelList(m), nil
}

// channelList flattens channels keyed by band, as the unit names the band.
func channelList(m map[string][]channelEntry) []Channel {
	out := []Channel{}
	for band, list := range m {
		for _, e := range list {
			out = append(out, Channel{
				Band:       band,
				Number:     e.Channel,
				FreqMHz:    e.Freq,
				TxPower:    e.Power,
				Width:      e.Width,
				Radar:      e.Radar,
				DFSState:   e.DFSState,
				DFSTime:    e.DFSTime,
				DFSCACTime: e.DFSCACTime,
			})
		}
	}
	slices.SortFunc(out, func(a, b Channel) int { return cmp.Or(cmp.Compare(a.Band, b.Band), cmp.Compare(a.Number, b.Number)) })
	return out
}

// Countries lists the country codes the unit supports
// (Device.GetWifiCountries).
func Countries(ctx context.Context, c *amplifi.Client) ([]string, error) {
	v, err := c.Call(ctx, amplifi.IfaceDevice, "GetWifiCountries", nil)
	if err != nil {
		return nil, fmt.Errorf("wifi countries: %w", err)
	}
	return countryCodes(v), nil
}

// countryCodes splits the unit's answer, one string of concatenated
// two-letter codes, into codes.
func countryCodes(v any) []string {
	s, _ := v.(string)
	codes := make([]string, 0, len(s)/2)
	for i := 0; i+2 <= len(s); i += 2 {
		codes = append(codes, s[i:i+2])
	}
	return codes
}

// GetRadar returns the DFS radar events the unit has seen
// (Device.GetRadarInfo), as the unit reports them. It is an empty map when
// there are none (the unit answers JSON {}); the shape of a non-empty answer
// is unknown.
func GetRadar(ctx context.Context, c *amplifi.Client) (map[string]any, error) {
	v, err := c.Call(ctx, amplifi.IfaceDevice, "GetRadarInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("radar info: %w", err)
	}
	m, _ := rpc.JSONString(v).(map[string]any)
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}
