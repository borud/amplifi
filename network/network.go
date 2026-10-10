// Package network reads and changes a unit's IP networking: the WAN and LAN
// configuration, the logical network interfaces, UPnP/NAT-PMP and QoS. The
// DHCP server and port forwarding are in packages dhcp and portforward.
package network

import (
	"context"
	"fmt"
	"maps"
	"net/netip"
	"slices"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// NetConfigOption codes for GetNetworkConfig / SetNetworkConfig.
const (
	wanType           = "1"
	wanStaticAddr     = "2"
	wanGateway        = "3"
	wanDNS            = "4"
	wanUsername       = "5"
	wanPassword       = "6"
	wanUp             = "7"
	wanRuntimeAddress = "8"
	wanRuntimeGateway = "9"
	wanRuntimeDNS     = "10"
	wanMAC            = "11"
	wanBridgeMode     = "12"
	lanGateway        = "13"
	guestGateway      = "14"
	wanVLAN           = "15"
	wan6Protocol      = "16"
	wan6Up            = "17"
	wan6Address       = "18"
	wan6Gateway       = "19"
	wan6DNS           = "20"
	wan6ClientID      = "21"
	hwNAT             = "22"
	homeKit           = "28"
	iotGateway        = "29"
)

// NetworkInterfaceConfigOption codes for GetNetworkInterfaces /
// SetNetworkInterfaces.
const (
	ifDevice         = "1"
	ifEnabled        = "2"
	ifProtocol       = "3"
	ifAddress        = "4"
	ifGateway        = "5"
	ifDNS            = "6"
	ifUsername       = "7"
	ifPassword       = "8"
	ifUp             = "9"
	ifRuntimeAddress = "10"
	ifRuntimeGateway = "11"
	ifRuntimeDNS     = "12"
)

// UpnpConfigOption codes.
const (
	upnpEnabled   = "1"
	natPMPEnabled = "2"
	upnpPort      = "3"
)

// protocolNames maps NetProtocol codes.
var protocolNames = map[string]string{
	"1": "none", "2": "static", "3": "dhcp", "4": "dhcpv6", "5": "pppoe",
	"6": "zeroconf", "7": "gretap", "8": "gretap6", "9": "relay", "10": "6to4",
}

// protocolCode returns the NetProtocol code for a protocol name. "off" is
// accepted for "none".
func protocolCode(s string) (int64, error) {
	if s == "off" {
		s = "none"
	}
	return rpc.Code("protocol", protocolNames, s)
}

// protocolName returns the protocol name stored under key, or "" if unset.
func protocolName(m map[string]any, key string) string {
	n := rpc.Int(m, key)
	if n <= 0 {
		return ""
	}
	return rpc.CodeName(protocolNames, n)
}

// Config is the WAN and LAN addressing (GetNetworkConfig). An address the
// router stores without a prefix length gets a full-length prefix. The
// Runtime* fields are what is in use right now and cannot be set.
type Config struct {
	WANType       string       `json:"wan_type"`
	WANBridge     bool         `json:"wan_bridge"`
	WANStaticAddr netip.Prefix `json:"wan_static_addr,omitzero"`
	WANGateway    netip.Addr   `json:"wan_gateway,omitzero"`
	WANDNS        []netip.Addr `json:"wan_dns"`
	WANUsername   string       `json:"wan_username,omitempty"`
	WANPassword   string       `json:"wan_password,omitempty"`
	WANVLAN       int          `json:"wan_vlan"`
	WANMAC        amplifi.MAC  `json:"wan_mac,omitempty"`
	WAN6Protocol  string       `json:"wan6_protocol,omitempty"`
	WAN6ClientID  string       `json:"wan6_client_id,omitempty"`
	HWNAT         bool         `json:"hw_nat"`
	HomeKit       bool         `json:"homekit"`
	LANGateway    netip.Prefix `json:"lan_gateway,omitzero"`
	GuestGateway  netip.Prefix `json:"guest_gateway,omitzero"`
	IoTGateway    netip.Prefix `json:"iot_gateway,omitzero"`

	WANUp              bool           `json:"wan_up"`
	RuntimeWANAddress  netip.Prefix   `json:"runtime_wan_address,omitzero"`
	RuntimeWANGateway  netip.Addr     `json:"runtime_wan_gateway,omitzero"`
	RuntimeWANDNS      []netip.Addr   `json:"runtime_wan_dns"`
	WAN6Up             bool           `json:"wan6_up"`
	RuntimeWAN6Address []netip.Prefix `json:"runtime_wan6_address"`
	RuntimeWAN6Gateway []netip.Addr   `json:"runtime_wan6_gateway"`
	RuntimeWAN6DNS     []netip.Addr   `json:"runtime_wan6_dns"`
}

// Redacted returns a copy of cfg with the PPPoE password masked.
func (cfg Config) Redacted() Config {
	cfg.WANPassword = rpc.Mask(cfg.WANPassword)
	return cfg
}

// GetConfig fetches the WAN and LAN configuration.
func GetConfig(ctx context.Context, c *amplifi.Client) (*Config, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetNetworkConfig", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("network config: %w", err)
	}
	return parseConfig(m), nil
}

func parseConfig(m map[string]any) *Config {
	return &Config{
		WANType:       protocolName(m, wanType),
		WANBridge:     rpc.Bool(m, wanBridgeMode),
		WANStaticAddr: rpc.Prefix(m[wanStaticAddr]),
		WANGateway:    rpc.Addr(m[wanGateway]),
		WANDNS:        rpc.Addrs(m[wanDNS]),
		WANUsername:   rpc.String(m, wanUsername),
		WANPassword:   rpc.String(m, wanPassword),
		WANVLAN:       int(rpc.Int(m, wanVLAN)),
		WANMAC:        rpc.MAC(m[wanMAC]),
		WAN6Protocol:  protocolName(m, wan6Protocol),
		WAN6ClientID:  rpc.String(m, wan6ClientID),
		HWNAT:         rpc.Bool(m, hwNAT),
		HomeKit:       rpc.Bool(m, homeKit),
		LANGateway:    rpc.Prefix(m[lanGateway]),
		GuestGateway:  rpc.Prefix(m[guestGateway]),
		IoTGateway:    rpc.Prefix(m[iotGateway]),

		WANUp:              rpc.Bool(m, wanUp),
		RuntimeWANAddress:  rpc.Prefix(m[wanRuntimeAddress]),
		RuntimeWANGateway:  rpc.Addr(m[wanRuntimeGateway]),
		RuntimeWANDNS:      rpc.Addrs(m[wanRuntimeDNS]),
		WAN6Up:             rpc.Bool(m, wan6Up),
		RuntimeWAN6Address: rpc.Prefixes(m[wan6Address]),
		RuntimeWAN6Gateway: rpc.Addrs(m[wan6Gateway]),
		RuntimeWAN6DNS:     rpc.Addrs(m[wan6DNS]),
	}
}

// ConfigUpdate lists the network settings to change; nil fields are left as
// they are. Addresses must be IPv4; a zero address or prefix clears the
// setting.
type ConfigUpdate struct {
	// WANType is a protocol name, usually "dhcp", "static" or "pppoe".
	WANType       *string
	WANBridge     *bool
	WANStaticAddr *netip.Prefix
	WANGateway    *netip.Addr
	// WANDNS replaces the static DNS servers when non-nil; an empty slice
	// clears them.
	WANDNS      []netip.Addr
	WANUsername *string
	WANPassword *string
	WANVLAN     *int
	// WANMAC overrides the WAN MAC address; an empty MAC restores the
	// default.
	WANMAC       *amplifi.MAC
	WAN6Protocol *string
	WAN6ClientID *string
	HWNAT        *bool
	LANGateway   *netip.Prefix
	GuestGateway *netip.Prefix
	IoTGateway   *netip.Prefix
}

// UpdateConfig applies u with a read-modify-write of the network
// configuration. Changing the WAN settings can cut the connection to the
// internet, and changing the LAN gateway the connection to the unit.
func UpdateConfig(ctx context.Context, c *amplifi.Client, u ConfigUpdate) error {
	changes, err := u.options()
	if err != nil {
		return fmt.Errorf("network config: %w", err)
	}
	if len(changes) == 0 {
		return nil
	}

	err = rpc.Update(ctx, c, amplifi.IfaceDevice, "GetNetworkConfig", rpc.Null, "SetNetworkConfig", changes)
	if err != nil {
		return fmt.Errorf("network config: %w", err)
	}
	return nil
}

// options turns u into NetConfigOption values.
func (u ConfigUpdate) options() (map[string]any, error) {
	o := optionSet{m: map[string]any{}}
	o.protocol(wanType, u.WANType)
	rpc.Put(o.m, wanBridgeMode, u.WANBridge)
	o.prefix(wanStaticAddr, u.WANStaticAddr)
	o.addr(wanGateway, u.WANGateway)
	o.addrs(wanDNS, u.WANDNS)
	rpc.Put(o.m, wanUsername, u.WANUsername)
	rpc.Put(o.m, wanPassword, u.WANPassword)
	rpc.PutInt(o.m, wanVLAN, u.WANVLAN)
	o.mac(wanMAC, u.WANMAC)
	o.protocol(wan6Protocol, u.WAN6Protocol)
	rpc.Put(o.m, wan6ClientID, u.WAN6ClientID)
	rpc.Put(o.m, hwNAT, u.HWNAT)
	o.prefix(lanGateway, u.LANGateway)
	o.prefix(guestGateway, u.GuestGateway)
	o.prefix(iotGateway, u.IoTGateway)
	return o.m, o.err
}

// Interface is one logical network interface (GetNetworkInterfaces).
type Interface struct {
	Name           string       `json:"name"`
	Device         string       `json:"device"`
	Enabled        bool         `json:"enabled"`
	Protocol       string       `json:"protocol"`
	Address        netip.Prefix `json:"address,omitzero"`
	Gateway        netip.Addr   `json:"gateway,omitzero"`
	DNS            netip.Addr   `json:"dns,omitzero"`
	Username       string       `json:"username,omitempty"`
	Password       string       `json:"password,omitempty"`
	Up             bool         `json:"up"`
	RuntimeAddress netip.Prefix `json:"runtime_address,omitzero"`
	RuntimeGateway netip.Addr   `json:"runtime_gateway,omitzero"`
	RuntimeDNS     []netip.Addr `json:"runtime_dns"`
}

// Redacted returns a copy of i with the PPPoE password masked.
func (i Interface) Redacted() Interface {
	i.Password = rpc.Mask(i.Password)
	return i
}

// Interfaces lists the unit's logical network interfaces, sorted by name.
func Interfaces(ctx context.Context, c *amplifi.Client) ([]Interface, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetNetworkInterfaces", nil)
	if err != nil {
		return nil, fmt.Errorf("network interfaces: %w", err)
	}
	return parseInterfaces(m), nil
}

// parseInterfaces decodes a RawNetworkConfig: a map of interface name to
// NetworkInterfaceConfigOption map.
func parseInterfaces(m map[string]any) []Interface {
	out := make([]Interface, 0, len(m))
	for _, ifname := range slices.Sorted(maps.Keys(m)) {
		f, ok := m[ifname].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Interface{
			Name:           ifname,
			Device:         rpc.String(f, ifDevice),
			Enabled:        rpc.Bool(f, ifEnabled),
			Protocol:       protocolName(f, ifProtocol),
			Address:        rpc.Prefix(f[ifAddress]),
			Gateway:        rpc.Addr(f[ifGateway]),
			DNS:            rpc.Addr(f[ifDNS]),
			Username:       rpc.String(f, ifUsername),
			Password:       rpc.String(f, ifPassword),
			Up:             rpc.Bool(f, ifUp),
			RuntimeAddress: rpc.Prefix(f[ifRuntimeAddress]),
			RuntimeGateway: rpc.Addr(f[ifRuntimeGateway]),
			RuntimeDNS:     rpc.Addrs(f[ifRuntimeDNS]),
		})
	}
	return out
}

// InterfaceUpdate lists the interface settings to change; nil fields are
// left as they are. Addresses must be IPv4.
type InterfaceUpdate struct {
	Enabled  *bool
	Protocol *string
	Address  *netip.Prefix
	Gateway  *netip.Addr
	DNS      *netip.Addr
	Username *string
	Password *string
}

func (u InterfaceUpdate) options() (map[string]any, error) {
	o := optionSet{m: map[string]any{}}
	rpc.Put(o.m, ifEnabled, u.Enabled)
	o.protocol(ifProtocol, u.Protocol)
	o.prefix(ifAddress, u.Address)
	o.addr(ifGateway, u.Gateway)
	o.addr(ifDNS, u.DNS)
	rpc.Put(o.m, ifUsername, u.Username)
	rpc.Put(o.m, ifPassword, u.Password)
	return o.m, o.err
}

// UpdateInterface changes the interface named ifname. It reads all
// interfaces, applies u to that one and writes them all back.
func UpdateInterface(ctx context.Context, c *amplifi.Client, ifname string, u InterfaceUpdate) error {
	return updateInterface(ctx, c, ifname, u)
}

func updateInterface(ctx context.Context, c rpc.Caller, ifname string, u InterfaceUpdate) error {
	changes, err := u.options()
	if err != nil {
		return fmt.Errorf("network interface %q: %w", ifname, err)
	}
	if err := rpc.UpdateEntry(ctx, c, amplifi.IfaceDevice, "GetNetworkInterfaces", nil, "SetNetworkInterfaces", ifname, changes); err != nil {
		return fmt.Errorf("network interface %q: %w", ifname, err)
	}
	return nil
}

// UPnP is the UPnP and NAT-PMP port mapping service configuration.
type UPnP struct {
	Enabled bool `json:"enabled"`
	NATPMP  bool `json:"nat_pmp_enabled"`
	Port    int  `json:"port"`
}

// GetUPnP fetches the UPnP/NAT-PMP configuration.
func GetUPnP(ctx context.Context, c *amplifi.Client) (*UPnP, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetUpnpConfig", nil)
	if err != nil {
		return nil, fmt.Errorf("UPnP config: %w", err)
	}
	return &UPnP{
		Enabled: rpc.Bool(m, upnpEnabled),
		NATPMP:  rpc.Bool(m, natPMPEnabled),
		Port:    int(rpc.Int(m, upnpPort)),
	}, nil
}

// UPnPUpdate lists the UPnP settings to change; nil fields are left as they
// are.
type UPnPUpdate struct {
	Enabled *bool
	NATPMP  *bool
	Port    *int
}

// UpdateUPnP applies u with a read-modify-write.
func UpdateUPnP(ctx context.Context, c *amplifi.Client, u UPnPUpdate) error {
	changes := map[string]any{}
	rpc.Put(changes, upnpEnabled, u.Enabled)
	rpc.Put(changes, natPMPEnabled, u.NATPMP)
	rpc.PutInt(changes, upnpPort, u.Port)
	if len(changes) == 0 {
		return nil
	}

	if err := rpc.Update(ctx, c, amplifi.IfaceDevice, "GetUpnpConfig", nil, "SetUpnpConfig", changes); err != nil {
		return fmt.Errorf("UPnP config: %w", err)
	}
	return nil
}

// GetQoS fetches the QoS configuration as decoded JSON; its schema is
// unknown.
func GetQoS(ctx context.Context, c *amplifi.Client) (map[string]any, error) {
	var m map[string]any
	if err := rpc.Value(ctx, c, amplifi.IfaceDevice, "GetQosConfig", nil, &m); err != nil {
		return nil, fmt.Errorf("QoS config: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// SetQoS replaces the QoS configuration. cfg is sent as JSON and should have
// the shape GetQoS returns.
func SetQoS(ctx context.Context, c *amplifi.Client, cfg map[string]any) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "SetQosConfig", cfg, nil); err != nil {
		return fmt.Errorf("QoS config: %w", err)
	}
	return nil
}

// optionSet collects option values that need packing, keeping the first
// error. Plain values go straight into m with rpc.Put.
type optionSet struct {
	m   map[string]any
	err error
}

func (o *optionSet) set(key string, v any, err error) {
	if err != nil {
		if o.err == nil {
			o.err = err
		}
		return
	}
	o.m[key] = v
}

func (o *optionSet) protocol(key string, s *string) {
	if s == nil {
		return
	}
	code, err := protocolCode(*s)
	o.set(key, code, err)
}

func (o *optionSet) addr(key string, a *netip.Addr) {
	if a == nil {
		return
	}
	b, err := rpc.PackAddr(*a)
	o.set(key, b, err)
}

func (o *optionSet) addrs(key string, list []netip.Addr) {
	if list == nil {
		return
	}
	out := make([]any, 0, len(list))
	for _, a := range list {
		b, err := rpc.PackAddr(a)
		if err != nil {
			o.set(key, nil, err)
			return
		}
		out = append(out, b)
	}
	o.set(key, out, nil)
}

func (o *optionSet) prefix(key string, p *netip.Prefix) {
	if p == nil {
		return
	}
	b, err := rpc.PackPrefix(*p)
	o.set(key, b, err)
}

func (o *optionSet) mac(key string, m *amplifi.MAC) {
	if m == nil {
		return
	}
	b, err := rpc.PackMAC(*m)
	o.set(key, b, err)
}
