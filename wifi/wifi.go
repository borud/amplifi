// Package wifi reads and changes a unit's Wi-Fi: the main, guest and IoT
// networks, radios and interfaces, channels and countries, DFS radar events,
// the signal quality scan and WPS push-button pairing.
package wifi

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// WifiConfigOption codes.
const (
	cfgInternalEnabled    = "2"
	cfgUserEnabled        = "3"
	cfgGuestEnabled       = "4"
	cfgSSID               = "5"
	cfgInternalSSID       = "6"
	cfgGuestSSID          = "7"
	cfgPassword           = "8"
	cfgInternalPassword   = "9"
	cfgGuestPassword      = "10"
	cfgEncryption         = "11"
	cfgInternalEncryption = "12"
	cfgGuestEncryption    = "13"
	cfgBandSteering       = "14"
	cfgRouterSteering     = "15"
	cfgActiveBand         = "16"
	cfgSingleRadio        = "17"
	cfgCountry            = "18"
	cfgChannel24          = "19"
	cfgChannel5           = "20"
	cfgInternalHidden     = "21"
	cfgUserHidden         = "22"
	cfgGuestHidden        = "23"
	cfgRuntimeCountry     = "24"
	cfgRuntimeChannel24   = "25"
	cfgRuntimeChannel5    = "26"
	cfgMultipleHops       = "30"
	cfgRelayPassword      = "34"
	cfgLegacy11b          = "36"
	cfgEthernetBackbone   = "37"
	cfg80211r             = "38"
	cfg80211w             = "39"
	cfg80211k             = "40"
	cfg80211v             = "41"
	cfgIoTEnabled         = "61"
	cfgIoTHidden          = "62"
	cfgIoTSSID            = "63"
	cfgIoTEncryption      = "64"
)

// ChannelAuto is the channel value for automatic selection. Units also
// report 0 for automatic.
const ChannelAuto = -1

// SiteConfigOption codes for the guest network limits.
const (
	siteGuestClientLimit = "6"
	siteGuestDisableTime = "8"
	siteGuestDuration    = "9"
)

// Network is one SSID of the Wi-Fi configuration.
type Network struct {
	Enabled    bool       `json:"enabled"`
	Hidden     bool       `json:"hidden"`
	SSID       string     `json:"ssid"`
	Password   string     `json:"password,omitempty"`
	Encryption Encryption `json:"encryption"`
}

// Redacted returns a copy of n with the password masked.
func (n Network) Redacted() Network {
	n.Password = rpc.Mask(n.Password)
	return n
}

// NetworkUpdate changes one network. Nil fields are left as they are.
type NetworkUpdate struct {
	Enabled    *bool
	Hidden     *bool
	SSID       *string
	Password   *string
	Encryption *Encryption
}

// Config is the unit's Wi-Fi configuration. Channel ChannelAuto (or 0)
// means automatic. Fields holds every option code returned, so unmapped ones
// are still visible.
type Config struct {
	Main             Network        `json:"main"`
	Guest            Network        `json:"guest"`
	IoT              Network        `json:"iot"`
	Internal         Network        `json:"internal"`
	BandSteering     bool           `json:"band_steering"`
	RouterSteering   bool           `json:"router_steering"`
	SingleRadio      bool           `json:"single_radio"`
	ActiveBand       string         `json:"active_band,omitempty"`
	Country          string         `json:"country"`
	RuntimeCountry   string         `json:"runtime_country"`
	Channel24        int            `json:"channel_24"`
	Channel5         int            `json:"channel_5"`
	RuntimeChannel24 int            `json:"runtime_channel_24"`
	RuntimeChannel5  int            `json:"runtime_channel_5"`
	FastRoaming      bool           `json:"80211r"`
	RadioMeasurement bool           `json:"80211k"`
	BSSTransition    bool           `json:"80211v"`
	PMF              PMF            `json:"80211w"`
	Legacy11b        bool           `json:"legacy_11b"`
	MultipleHops     bool           `json:"multiple_hops"`
	EthernetBackbone bool           `json:"ethernet_backbone"`
	Fields           map[string]any `json:"fields"`
}

// Redacted returns a copy of w with every password masked, including those
// in Fields.
func (w Config) Redacted() Config {
	w.Main = w.Main.Redacted()
	w.Guest = w.Guest.Redacted()
	w.IoT = w.IoT.Redacted()
	w.Internal = w.Internal.Redacted()
	w.Fields = rpc.MaskFields(w.Fields, cfgPassword, cfgInternalPassword, cfgGuestPassword, cfgRelayPassword)
	return w
}

// ConfigUpdate changes the Wi-Fi configuration. Nil fields are left as they
// are. A channel of ChannelAuto selects the channel automatically.
type ConfigUpdate struct {
	Main             NetworkUpdate
	Guest            NetworkUpdate
	IoT              NetworkUpdate
	BandSteering     *bool
	RouterSteering   *bool
	SingleRadio      *bool
	ActiveBand       *string
	Country          *string
	Channel24        *int
	Channel5         *int
	FastRoaming      *bool
	RadioMeasurement *bool
	BSSTransition    *bool
	PMF              *PMF
	Legacy11b        *bool
	MultipleHops     *bool
}

// networkCodes are the option codes of one network in the configuration. An
// empty code means the network has no such option.
type networkCodes struct {
	enabled, hidden, ssid, password, encryption string
}

var (
	mainCodes     = networkCodes{cfgUserEnabled, cfgUserHidden, cfgSSID, cfgPassword, cfgEncryption}
	guestCodes    = networkCodes{cfgGuestEnabled, cfgGuestHidden, cfgGuestSSID, cfgGuestPassword, cfgGuestEncryption}
	iotCodes      = networkCodes{cfgIoTEnabled, cfgIoTHidden, cfgIoTSSID, "", cfgIoTEncryption}
	internalCodes = networkCodes{cfgInternalEnabled, cfgInternalHidden, cfgInternalSSID, cfgInternalPassword, cfgInternalEncryption}
)

func (nc networkCodes) parse(m map[string]any) Network {
	return Network{
		Enabled:    rpc.Bool(m, nc.enabled),
		Hidden:     rpc.Bool(m, nc.hidden),
		SSID:       rpc.String(m, nc.ssid),
		Password:   rpc.String(m, nc.password),
		Encryption: Encryption(rpc.Int(m, nc.encryption)),
	}
}

func (nc networkCodes) changes(u NetworkUpdate, out map[string]any) error {
	if u.Password != nil && nc.password == "" {
		return errors.New("this network has no password of its own")
	}
	rpc.Put(out, nc.enabled, u.Enabled)
	rpc.Put(out, nc.hidden, u.Hidden)
	rpc.Put(out, nc.ssid, u.SSID)
	rpc.Put(out, nc.password, u.Password)
	rpc.PutInt(out, nc.encryption, u.Encryption)
	return nil
}

// changes returns the option codes u sets and their new values.
func (u ConfigUpdate) changes() (map[string]any, error) {
	m := map[string]any{}
	if err := mainCodes.changes(u.Main, m); err != nil {
		return nil, fmt.Errorf("main network: %w", err)
	}
	if err := guestCodes.changes(u.Guest, m); err != nil {
		return nil, fmt.Errorf("guest network: %w", err)
	}
	if err := iotCodes.changes(u.IoT, m); err != nil {
		return nil, fmt.Errorf("iot network: %w", err)
	}

	if u.ActiveBand != nil {
		band, ok := bandCodes[strings.ToLower(*u.ActiveBand)]
		if !ok {
			return nil, fmt.Errorf("unknown band %q (want 2.4 or 5)", *u.ActiveBand)
		}
		m[cfgActiveBand] = band
	}

	rpc.Put(m, cfgBandSteering, u.BandSteering)
	rpc.Put(m, cfgRouterSteering, u.RouterSteering)
	rpc.Put(m, cfgSingleRadio, u.SingleRadio)
	rpc.Put(m, cfgCountry, u.Country)
	rpc.PutInt(m, cfgChannel24, u.Channel24)
	rpc.PutInt(m, cfgChannel5, u.Channel5)
	rpc.Put(m, cfg80211r, u.FastRoaming)
	rpc.Put(m, cfg80211k, u.RadioMeasurement)
	rpc.Put(m, cfg80211v, u.BSSTransition)
	rpc.PutInt(m, cfg80211w, u.PMF)
	rpc.Put(m, cfgLegacy11b, u.Legacy11b)
	rpc.Put(m, cfgMultipleHops, u.MultipleHops)
	return m, nil
}

// GetConfig fetches the unit's Wi-Fi configuration (Device.GetWifiConfig).
// It includes the passwords; see Config.Redacted.
func GetConfig(ctx context.Context, c *amplifi.Client) (*Config, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiConfig", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("wifi config: %w", err)
	}
	return parseConfig(m), nil
}

func parseConfig(m map[string]any) *Config {
	return &Config{
		Main:             mainCodes.parse(m),
		Guest:            guestCodes.parse(m),
		IoT:              iotCodes.parse(m),
		Internal:         internalCodes.parse(m),
		BandSteering:     rpc.Bool(m, cfgBandSteering),
		RouterSteering:   rpc.Bool(m, cfgRouterSteering),
		SingleRadio:      rpc.Bool(m, cfgSingleRadio),
		ActiveBand:       rpc.Name(rpc.BandNames, m, cfgActiveBand),
		Country:          rpc.String(m, cfgCountry),
		RuntimeCountry:   rpc.String(m, cfgRuntimeCountry),
		Channel24:        int(rpc.Int(m, cfgChannel24)),
		Channel5:         int(rpc.Int(m, cfgChannel5)),
		RuntimeChannel24: int(rpc.Int(m, cfgRuntimeChannel24)),
		RuntimeChannel5:  int(rpc.Int(m, cfgRuntimeChannel5)),
		FastRoaming:      rpc.Bool(m, cfg80211r),
		RadioMeasurement: rpc.Bool(m, cfg80211k),
		BSSTransition:    rpc.Bool(m, cfg80211v),
		PMF:              PMF(rpc.Int(m, cfg80211w)),
		Legacy11b:        rpc.Bool(m, cfgLegacy11b),
		MultipleHops:     rpc.Bool(m, cfgMultipleHops),
		EthernetBackbone: rpc.Bool(m, cfgEthernetBackbone),
		Fields:           m,
	}
}

// UpdateConfig changes the Wi-Fi configuration with a read-modify-write of
// Device.GetWifiConfig / SetWifiConfig. Only the fields set in u change.
func UpdateConfig(ctx context.Context, c *amplifi.Client, u ConfigUpdate) error {
	changes, err := u.changes()
	if err != nil {
		return fmt.Errorf("update wifi config: %w", err)
	}
	if len(changes) == 0 {
		return nil
	}
	if err := rpc.Update(ctx, c, amplifi.IfaceDevice, "GetWifiConfig", rpc.Null, "SetWifiConfig", changes); err != nil {
		return fmt.Errorf("update wifi config: %w", err)
	}
	return nil
}

// Guest is the guest network and its limits. The limits live in the
// mesh-wide site configuration. DurationLimit and DisableTime are the
// router's raw values; their units are unconfirmed (DurationLimit is
// probably minutes, DisableTime a timestamp).
type Guest struct {
	Network
	ClientLimit   int    `json:"client_limit"`
	DurationLimit int    `json:"duration_limit"`
	DisableTime   string `json:"disable_time,omitempty"`
}

// Redacted returns a copy of g with the password masked.
func (g Guest) Redacted() Guest {
	g.Network = g.Network.Redacted()
	return g
}

// GuestUpdate changes the guest network. Nil fields are left as they are.
type GuestUpdate struct {
	NetworkUpdate
	ClientLimit   *int
	DurationLimit *int
	DisableTime   *string
}

// GetGuest fetches the guest network (Device.GetWifiConfig) and its limits
// (ClusterNode.GetSiteConfig).
func GetGuest(ctx context.Context, c *amplifi.Client) (*Guest, error) {
	w, err := GetConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	site, err := rpc.ClusterMap(ctx, c, "GetSiteConfig")
	if err != nil {
		return nil, fmt.Errorf("guest limits: %w", err)
	}
	return &Guest{
		Network:       w.Guest,
		ClientLimit:   int(rpc.Int(site, siteGuestClientLimit)),
		DurationLimit: int(rpc.Int(site, siteGuestDuration)),
		DisableTime:   rpc.String(site, siteGuestDisableTime),
	}, nil
}

// UpdateGuest changes the guest network and its limits. Network fields go to
// Device.SetWifiConfig, limits to ClusterNode.SetSiteConfig, each as a
// read-modify-write and only when something in it changes. Both are read
// before either is written, so a failed read changes nothing.
func UpdateGuest(ctx context.Context, c *amplifi.Client, u GuestUpdate) error {
	wifiChanges, err := ConfigUpdate{Guest: u.NetworkUpdate}.changes()
	if err != nil {
		return fmt.Errorf("update guest network: %w", err)
	}
	siteChanges := map[string]any{}
	rpc.PutInt(siteChanges, siteGuestClientLimit, u.ClientLimit)
	rpc.PutInt(siteChanges, siteGuestDuration, u.DurationLimit)
	rpc.Put(siteChanges, siteGuestDisableTime, u.DisableTime)

	var cfg, site map[string]any
	if len(wifiChanges) > 0 {
		cfg, err = rpc.Map(ctx, c, amplifi.IfaceDevice, "GetWifiConfig", rpc.Null)
		if err != nil {
			return fmt.Errorf("update guest network: %w", err)
		}
		maps.Copy(cfg, wifiChanges)
	}
	if len(siteChanges) > 0 {
		site, err = rpc.ClusterMap(ctx, c, "GetSiteConfig")
		if err != nil {
			return fmt.Errorf("update guest limits: %w", err)
		}
		maps.Copy(site, siteChanges)
	}

	if cfg != nil {
		if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "SetWifiConfig", rpc.Flat(cfg)); err != nil {
			return fmt.Errorf("update guest network: %w", err)
		}
	}
	if site == nil {
		return nil
	}
	if err := rpc.SetCluster(ctx, c, "SetSiteConfig", rpc.Flat(site)); err != nil {
		if cfg != nil {
			return fmt.Errorf("update guest limits (network settings were applied): %w", err)
		}
		return fmt.Errorf("update guest limits: %w", err)
	}
	return nil
}

// GetIoT fetches the IoT network. It has no password of its own.
func GetIoT(ctx context.Context, c *amplifi.Client) (*Network, error) {
	w, err := GetConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	return &w.IoT, nil
}

// UpdateIoT changes the IoT network. Setting a password is an error.
func UpdateIoT(ctx context.Context, c *amplifi.Client, u NetworkUpdate) error {
	return UpdateConfig(ctx, c, ConfigUpdate{IoT: u})
}
