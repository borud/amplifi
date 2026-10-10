// Package device reads and changes the state of the unit a session is
// connected to: its board, running firmware, throughput, internet check, MAC
// addresses, identity configuration, properties and display settings, and
// reboots, upgrades or factory resets it.
package device

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// Option codes of the Device interface's option maps.
// Maps are keyed by the decimal string of the code.
const (
	boardName     = "1"
	boardRevision = "2"

	firmwareType            = "1"
	firmwareCurrentVersion  = "2"
	firmwareCurrentRevision = "3"
	firmwareUpgradeVersion  = "4"
	firmwareUpgradeRevision = "5"

	perfRx    = "1"
	perfTx    = "2"
	perfRxMax = "3"
	perfTxMax = "4"
)

// Board is a unit's system board description. Fields holds every option
// code returned, so unmapped ones are still visible.
type Board struct {
	Name     string         `json:"name"`
	Revision int            `json:"revision"`
	Fields   map[string]any `json:"fields"`
}

// GetBoard fetches the unit's board description.
func GetBoard(ctx context.Context, c *amplifi.Client) (*Board, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetSystemBoardInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("board info: %w", err)
	}
	return &Board{
		Name:     rpc.String(m, boardName),
		Revision: int(rpc.Int(m, boardRevision)),
		Fields:   m,
	}, nil
}

// Firmware describes the unit's own running firmware and, when one is
// pending, the upgrade the unit has found. Mesh-wide updates are in package
// firmware.
type Firmware struct {
	Type            string `json:"type"`
	Version         string `json:"version"`
	Revision        string `json:"revision"`
	UpgradeVersion  string `json:"upgrade_version"`
	UpgradeRevision string `json:"upgrade_revision"`
}

// GetFirmware fetches the unit's firmware versions.
func GetFirmware(ctx context.Context, c *amplifi.Client) (*Firmware, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetFirmwareInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("firmware info: %w", err)
	}
	return &Firmware{
		Type:            rpc.String(m, firmwareType),
		Version:         rpc.String(m, firmwareCurrentVersion),
		Revision:        rpc.String(m, firmwareCurrentRevision),
		UpgradeVersion:  rpc.String(m, firmwareUpgradeVersion),
		UpgradeRevision: rpc.String(m, firmwareUpgradeRevision),
	}, nil
}

// Throughput is the unit's current and peak WAN throughput.
type Throughput struct {
	RxKbps    int `json:"rx_kbps"`
	TxKbps    int `json:"tx_kbps"`
	RxMaxKbps int `json:"rx_max_kbps"`
	TxMaxKbps int `json:"tx_max_kbps"`
}

// GetThroughput fetches the unit's throughput counters.
func GetThroughput(ctx context.Context, c *amplifi.Client) (*Throughput, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetPerformanceInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("throughput: %w", err)
	}
	return &Throughput{
		RxKbps:    int(rpc.Int(m, perfRx)),
		TxKbps:    int(rpc.Int(m, perfTx)),
		RxMaxKbps: int(rpc.Int(m, perfRxMax)),
		TxMaxKbps: int(rpc.Int(m, perfTxMax)),
	}, nil
}

// Health is the traffic light result of an internet check.
type Health int

// Health values.
const (
	HealthRed    Health = 1
	HealthYellow Health = 2
	HealthGreen  Health = 3
)

var healthNames = map[string]string{"1": "red", "2": "yellow", "3": "green"}

// String returns the health's name, or "#N" for an unknown value.
func (h Health) String() string { return rpc.CodeName(healthNames, int64(h)) }

// MarshalText renders the health by name.
func (h Health) MarshalText() ([]byte, error) { return []byte(h.String()), nil }

// ParseHealth parses a health name: red, yellow or green.
func ParseHealth(s string) (Health, error) {
	n, err := rpc.Code("health", healthNames, strings.TrimSpace(s))
	return Health(n), err
}

// InternetCheck is the result of the unit's internet connectivity check,
// with the unit's own text for each value. The unit of LinkSpeed is
// unconfirmed.
type InternetCheck struct {
	Health         Health `json:"inet_status"`
	HealthText     string `json:"inet_status_str"`
	Reason         int    `json:"inet_reason"`
	ReasonText     string `json:"inet_reason_str"`
	IPv6Health     Health `json:"ipv6_inet_status"`
	IPv6HealthText string `json:"ipv6_inet_status_str"`
	IPv6Reason     int    `json:"ipv6_inet_reason"`
	IPv6ReasonText string `json:"ipv6_inet_reason_str"`
	LinkSpeed      int    `json:"link_speed"`
	ISP            string `json:"local_isp"`
}

// CheckInternet runs the unit's internet connectivity check
// (Device.InetCheck) and waits for the result.
func CheckInternet(ctx context.Context, c *amplifi.Client) (*InternetCheck, error) {
	var st InternetCheck
	if err := rpc.Value(ctx, c, amplifi.IfaceDevice, "InetCheck", nil, &st); err != nil {
		return nil, fmt.Errorf("internet check: %w", err)
	}
	return &st, nil
}

// Reboot asks the unit to reboot. The unit typically drops the connection
// instead of replying, which counts as success. The session is unusable
// afterwards.
func Reboot(ctx context.Context, c *amplifi.Client) error {
	if err := c.CallDisconnecting(ctx, amplifi.IfaceDevice, "Reboot", nil); err != nil {
		return fmt.Errorf("reboot: %w", err)
	}
	return nil
}

// FactoryReset erases the unit's configuration and reboots it. It is
// irreversible. Like Reboot, a dropped connection counts as success, and the
// session is unusable afterwards.
func FactoryReset(ctx context.Context, c *amplifi.Client) error {
	if err := c.CallDisconnecting(ctx, amplifi.IfaceDevice, "FactoryReset", nil); err != nil {
		return fmt.Errorf("factory reset: %w", err)
	}
	return nil
}

// Upgrade upgrades only the unit this session is connected to, with the
// upgrade it has already found. The unit may drop the connection to flash
// and reboot, which counts as success; the session is unusable afterwards.
// Mesh-wide upgrades are in package firmware.
func Upgrade(ctx context.Context, c *amplifi.Client) error {
	if err := c.CallDisconnecting(ctx, amplifi.IfaceDevice, "UpgradeFirmware", nil); err != nil {
		return fmt.Errorf("upgrade firmware: %w", err)
	}
	return nil
}

// InterfaceMAC is one of the unit's MAC addresses and the interface it
// belongs to.
type InterfaceMAC struct {
	Role string      `json:"role"`
	MAC  amplifi.MAC `json:"mac"`
}

// macRoles maps MacAddressRole codes.
var macRoles = map[string]string{
	"1": "initial setup", "2": "internal 2.4GHz", "3": "internal 5GHz",
	"4": "user 2.4GHz", "5": "user 5GHz", "6": "guest 2.4GHz", "7": "guest 5GHz",
	"8": "primary", "9": "bluetooth", "10": "wan", "11": "lan",
	"31": "iot bridge", "32": "iot 2.4GHz", "33": "iot 5GHz",
}

// MACs lists the unit's MAC addresses by role, in role code order.
func MACs(ctx context.Context, c *amplifi.Client) ([]InterfaceMAC, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetMacAddresses", nil)
	if err != nil {
		return nil, fmt.Errorf("mac addresses: %w", err)
	}
	return parseMACs(m), nil
}

// parseMACs decodes a GetMacAddresses result: a map of MacAddressRole code to
// binary MAC.
func parseMACs(m map[string]any) []InterfaceMAC {
	codes := make([]string, 0, len(m))
	for code, v := range m {
		if _, ok := v.([]byte); ok {
			codes = append(codes, code)
		}
	}
	slices.SortFunc(codes, rpc.CompareNumeric)

	macs := make([]InterfaceMAC, 0, len(codes))
	for _, code := range codes {
		macs = append(macs, InterfaceMAC{Role: rpc.KeyName(macRoles, code), MAC: amplifi.MAC(rpc.Bytes(m, code))})
	}
	return macs
}

// Config option codes (DeviceConfigOption).
const (
	configInitialSetupDone         = "1"
	configFirmwareUpgradeAvailable = "2"
	configProtocolVersion          = "3"
	configFriendlyName             = "4"
	configPlatformName             = "5"
	configUniqueName               = "6"
	configModelName                = "7"
	configSystemName               = "8"
	configBoardName                = "9"
	configBoardRevision            = "10"
	configCountry                  = "11"
	configTimezone                 = "12"
	configZoneName                 = "13"
	configHostName                 = "14"
	configRole                     = "15"
	configMLConnectivityMode       = "17"
)

// connectivityModes maps MlConnectivityMode codes.
var connectivityModes = map[string]string{"1": "unknown", "2": "wireless", "3": "wired"}

// Config is the unit's identity and locale configuration. Fields holds
// every option code returned, so unmapped ones are still visible.
type Config struct {
	InitialSetupDone         bool           `json:"initial_setup_done"`
	FirmwareUpgradeAvailable bool           `json:"firmware_upgrade_available"`
	ProtocolVersion          int            `json:"protocol_version"`
	FriendlyName             string         `json:"friendly_name"`
	PlatformName             string         `json:"platform_name"`
	UniqueName               string         `json:"unique_name"`
	ModelName                string         `json:"model_name"`
	SystemName               string         `json:"system_name"`
	BoardName                string         `json:"board_name"`
	BoardRevision            int            `json:"board_revision"`
	Country                  string         `json:"country"`
	Timezone                 string         `json:"timezone"`
	ZoneName                 string         `json:"zone_name"`
	HostName                 string         `json:"host_name"`
	Role                     amplifi.Role   `json:"role,omitempty"`
	ConnectivityMode         string         `json:"connectivity_mode"`
	Fields                   map[string]any `json:"fields"`
}

// GetConfig fetches the unit's device configuration.
func GetConfig(ctx context.Context, c *amplifi.Client) (*Config, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetDeviceConfig", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("device config: %w", err)
	}
	return parseConfig(m), nil
}

// parseConfig decodes a GetDeviceConfig option map.
func parseConfig(m map[string]any) *Config {
	cfg := &Config{
		InitialSetupDone:         rpc.Bool(m, configInitialSetupDone),
		FirmwareUpgradeAvailable: rpc.Bool(m, configFirmwareUpgradeAvailable),
		ProtocolVersion:          int(rpc.Int(m, configProtocolVersion)),
		FriendlyName:             rpc.String(m, configFriendlyName),
		PlatformName:             rpc.String(m, configPlatformName),
		UniqueName:               rpc.String(m, configUniqueName),
		ModelName:                rpc.String(m, configModelName),
		SystemName:               rpc.String(m, configSystemName),
		BoardName:                rpc.String(m, configBoardName),
		BoardRevision:            int(rpc.Int(m, configBoardRevision)),
		Country:                  rpc.String(m, configCountry),
		Timezone:                 rpc.String(m, configTimezone),
		ZoneName:                 rpc.String(m, configZoneName),
		HostName:                 rpc.String(m, configHostName),
		ConnectivityMode:         rpc.Name(connectivityModes, m, configMLConnectivityMode),
		Fields:                   m,
	}
	if _, ok := m[configRole]; ok {
		cfg.Role = rpc.Role(rpc.Int(m, configRole))
	}
	return cfg
}

// Property names, from the Property enums of DeviceInterface and
// ClusterNodeInterface.
var (
	deviceProperties = []string{
		"UniqueName", "PlatformName", "SystemName", "ModelName", "BoardName",
		"BoardRevision", "FriendlyName", "InitialSetupDone", "ProtocolVersion",
		"FirmwareUpgradeAvailable", "InitialSetupNetworkEnabled",
		"InternalNetworkEnabled", "UserNetworkEnabled", "GuestNetworkEnabled",
		"SingleRadio", "ActiveBand", "BandSteeringEnabled", "RouterSteeringEnabled",
		"WifiSignalQualityScanEnabled", "Country", "WanBlocked", "UserApPaused",
		"EthClientCount", "Uuid", "PlatformModification", "RegionLock",
	}
	clusterNodeProperties = []string{
		"NodeRole", "InitialSyncDone", "MasterPeer", "ExtenderRssiLocal",
		"ExtenderRssiRouter", "ExtenderRssiMin", "EthernetBackbone",
		"WebRtcIsDeviceAdopted",
	}
)

// Property is one property value read by Properties. Error is set instead
// of Value when the unit refused to read it.
type Property struct {
	Interface string `json:"interface"`
	Name      string `json:"name"`
	Value     any    `json:"value,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Properties reads every known Device and ClusterNode property and
// Unsecure.Uptime. Properties the unit refuses (not every firmware has all of
// them) are returned with Error set; any other failure aborts the read. Use
// c.Property to read a single property.
func Properties(ctx context.Context, c *amplifi.Client) ([]Property, error) {
	sets := []struct {
		iface string
		names []string
	}{
		{amplifi.IfaceDevice, deviceProperties},
		{amplifi.IfaceClusterNode, clusterNodeProperties},
		{amplifi.IfaceUnsecure, []string{"Uptime"}},
	}

	props := make([]Property, 0, len(deviceProperties)+len(clusterNodeProperties)+1)
	for _, set := range sets {
		for _, n := range set.names {
			p, err := readProperty(ctx, c, set.iface, n)
			if err != nil {
				return nil, fmt.Errorf("properties: %w", err)
			}
			props = append(props, p)
		}
	}
	return props, nil
}

// readProperty reads one property, recording a refusal in the result.
func readProperty(ctx context.Context, c *amplifi.Client, iface, name string) (Property, error) {
	p := Property{Interface: iface, Name: name}

	v, err := c.Property(ctx, iface, name)
	if amplifi.IsRouterError(err) {
		p.Error = err.Error()
		return p, nil
	}
	if err != nil {
		return p, err
	}

	p.Value = v
	return p, nil
}

// UiConfig option codes (UiConfigOption).
const (
	uiSystemSoundVolume = "1"
	uiTapSoundVolume    = "2"
	uiLCDBrightness     = "3"
	uiLEDBrightness     = "4"
	uiClockType         = "5"
	uiClockTimeout      = "6"
	uiOffTimeout        = "7"
	uiNightStart        = "8"
	uiNightEnd          = "9"
	uiNightMode         = "10"
)

// Display is the unit's touchscreen, LED and sound settings (UiConfig).
// NightStart and NightEnd are times of day as durations since midnight. The
// units of the timeouts are unconfirmed; they are the unit's raw values.
type Display struct {
	SystemSoundVolume int           `json:"system_sound_volume"`
	TapSoundVolume    int           `json:"tap_sound_volume"`
	LCDBrightness     int           `json:"lcd_brightness"`
	LEDBrightness     int           `json:"led_brightness"`
	ClockType         int           `json:"clock_type"`
	ClockTimeout      int           `json:"clock_timeout"`
	OffTimeout        int           `json:"off_timeout"`
	NightStart        time.Duration `json:"night_start"`
	NightEnd          time.Duration `json:"night_end"`
	NightMode         bool          `json:"night_mode"`
}

// MarshalJSON renders NightStart and NightEnd in seconds.
func (d Display) MarshalJSON() ([]byte, error) {
	type display Display
	return json.Marshal(struct {
		display
		NightStart int64 `json:"night_start"`
		NightEnd   int64 `json:"night_end"`
	}{display(d), int64(d.NightStart / time.Second), int64(d.NightEnd / time.Second)})
}

// GetDisplay fetches the unit's display settings.
func GetDisplay(ctx context.Context, c *amplifi.Client) (*Display, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetUiConfig", nil)
	if err != nil {
		return nil, fmt.Errorf("display settings: %w", err)
	}
	return parseDisplay(m), nil
}

// parseDisplay decodes a GetUiConfig option map. The night times are minutes
// after midnight.
func parseDisplay(m map[string]any) *Display {
	return &Display{
		SystemSoundVolume: int(rpc.Int(m, uiSystemSoundVolume)),
		TapSoundVolume:    int(rpc.Int(m, uiTapSoundVolume)),
		LCDBrightness:     int(rpc.Int(m, uiLCDBrightness)),
		LEDBrightness:     int(rpc.Int(m, uiLEDBrightness)),
		ClockType:         int(rpc.Int(m, uiClockType)),
		ClockTimeout:      int(rpc.Int(m, uiClockTimeout)),
		OffTimeout:        int(rpc.Int(m, uiOffTimeout)),
		NightStart:        time.Duration(rpc.Int(m, uiNightStart)) * time.Minute,
		NightEnd:          time.Duration(rpc.Int(m, uiNightEnd)) * time.Minute,
		NightMode:         rpc.Bool(m, uiNightMode),
	}
}

// DisplayUpdate holds the display settings to change; nil fields are left as
// they are. NightStart and NightEnd are rounded down to whole minutes.
type DisplayUpdate struct {
	SystemSoundVolume *int
	TapSoundVolume    *int
	LCDBrightness     *int
	LEDBrightness     *int
	ClockType         *int
	ClockTimeout      *int
	OffTimeout        *int
	NightStart        *time.Duration
	NightEnd          *time.Duration
	NightMode         *bool
}

// UpdateDisplay changes the display settings set in u, keeping the others.
func UpdateDisplay(ctx context.Context, c *amplifi.Client, u DisplayUpdate) error {
	changes := u.options()
	if len(changes) == 0 {
		return nil
	}
	if err := rpc.Update(ctx, c, amplifi.IfaceDevice, "GetUiConfig", nil, "SetUiConfig", changes); err != nil {
		return fmt.Errorf("update display settings: %w", err)
	}
	return nil
}

// options returns the set fields as UiConfig option codes.
func (u DisplayUpdate) options() map[string]any {
	changes := map[string]any{}
	rpc.Put(changes, uiSystemSoundVolume, u.SystemSoundVolume)
	rpc.Put(changes, uiTapSoundVolume, u.TapSoundVolume)
	rpc.Put(changes, uiLCDBrightness, u.LCDBrightness)
	rpc.Put(changes, uiLEDBrightness, u.LEDBrightness)
	rpc.Put(changes, uiClockType, u.ClockType)
	rpc.Put(changes, uiClockTimeout, u.ClockTimeout)
	rpc.Put(changes, uiOffTimeout, u.OffTimeout)
	if u.NightStart != nil {
		changes[uiNightStart] = int64(*u.NightStart / time.Minute)
	}
	if u.NightEnd != nil {
		changes[uiNightEnd] = int64(*u.NightEnd / time.Minute)
	}
	rpc.Put(changes, uiNightMode, u.NightMode)
	return changes
}
