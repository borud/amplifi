// Package network reads and changes a unit's IP networking: the WAN and LAN
// configuration, the logical network interfaces, UPnP/NAT-PMP, QoS, the
// DHCP server's pools and static leases, and port forwarding rules.
package network

import (
	"context"
	"encoding/hex"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strconv"

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

// Configuration is the WAN and LAN addressing (GetNetworkConfig).
// Addresses with a mask are in CIDR form. The Runtime* fields are what is in
// use right now and cannot be set.
type Configuration struct {
	WANType       string      `json:"wan_type"`
	WANBridge     bool        `json:"wan_bridge"`
	WANStaticAddr string      `json:"wan_static_addr,omitempty"`
	WANGateway    string      `json:"wan_gateway,omitempty"`
	WANDNS        []string    `json:"wan_dns"`
	WANUsername   string      `json:"wan_username,omitempty"`
	WANPassword   string      `json:"wan_password,omitempty"`
	WANVLAN       int         `json:"wan_vlan"`
	WANMAC        amplifi.MAC `json:"wan_mac,omitempty"`
	WAN6Protocol  string      `json:"wan6_protocol,omitempty"`
	WAN6ClientID  string      `json:"wan6_client_id,omitempty"`
	HWNAT         bool        `json:"hw_nat"`
	HomeKit       bool        `json:"homekit"`
	LANGateway    string      `json:"lan_gateway,omitempty"`
	GuestGateway  string      `json:"guest_gateway,omitempty"`
	IoTGateway    string      `json:"iot_gateway,omitempty"`

	WANUp              bool     `json:"wan_up"`
	RuntimeWANAddress  string   `json:"runtime_wan_address,omitempty"`
	RuntimeWANGateway  string   `json:"runtime_wan_gateway,omitempty"`
	RuntimeWANDNS      []string `json:"runtime_wan_dns"`
	WAN6Up             bool     `json:"wan6_up"`
	RuntimeWAN6Address []string `json:"runtime_wan6_address"`
	RuntimeWAN6Gateway []string `json:"runtime_wan6_gateway"`
	RuntimeWAN6DNS     []string `json:"runtime_wan6_dns"`
}

// Redacted returns a copy of cfg with the PPPoE password masked.
func (cfg Configuration) Redacted() Configuration {
	cfg.WANPassword = rpc.Mask(cfg.WANPassword)
	return cfg
}

// Config fetches the WAN and LAN configuration.
func Config(ctx context.Context, c *amplifi.Client) (*Configuration, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetNetworkConfig", rpc.Null)
	if err != nil {
		return nil, fmt.Errorf("network config: %w", err)
	}
	return parseConfig(m), nil
}

func parseConfig(m map[string]any) *Configuration {
	return &Configuration{
		WANType:       protocolName(m, wanType),
		WANBridge:     rpc.Bool(m, wanBridgeMode),
		WANStaticAddr: formatAddr(m[wanStaticAddr]),
		WANGateway:    formatAddr(m[wanGateway]),
		WANDNS:        formatAddrs(m[wanDNS]),
		WANUsername:   rpc.String(m, wanUsername),
		WANPassword:   rpc.String(m, wanPassword),
		WANVLAN:       int(rpc.Int(m, wanVLAN)),
		WANMAC:        rpc.MAC(m[wanMAC]),
		WAN6Protocol:  protocolName(m, wan6Protocol),
		WAN6ClientID:  rpc.String(m, wan6ClientID),
		HWNAT:         rpc.Bool(m, hwNAT),
		HomeKit:       rpc.Bool(m, homeKit),
		LANGateway:    formatAddr(m[lanGateway]),
		GuestGateway:  formatAddr(m[guestGateway]),
		IoTGateway:    formatAddr(m[iotGateway]),

		WANUp:              rpc.Bool(m, wanUp),
		RuntimeWANAddress:  formatAddr(m[wanRuntimeAddress]),
		RuntimeWANGateway:  formatAddr(m[wanRuntimeGateway]),
		RuntimeWANDNS:      formatAddrs(m[wanRuntimeDNS]),
		WAN6Up:             rpc.Bool(m, wan6Up),
		RuntimeWAN6Address: formatAddrs(m[wan6Address]),
		RuntimeWAN6Gateway: formatAddrs(m[wan6Gateway]),
		RuntimeWAN6DNS:     formatAddrs(m[wan6DNS]),
	}
}

// ConfigUpdate lists the network settings to change; nil fields are left as
// they are. Addresses with a mask (WANStaticAddr and the gateways of the
// LAN, guest and IoT networks) are IPv4 CIDR such as "192.168.1.1/24".
type ConfigUpdate struct {
	// WANType is a protocol name, usually "dhcp", "static" or "pppoe".
	WANType       *string
	WANBridge     *bool
	WANStaticAddr *string
	WANGateway    *string
	// WANDNS replaces the static DNS servers when non-nil; an empty slice
	// clears them.
	WANDNS      []string
	WANUsername *string
	WANPassword *string
	WANVLAN     *int
	// WANMAC overrides the WAN MAC address; an empty MAC restores the
	// default.
	WANMAC       *amplifi.MAC
	WAN6Protocol *string
	WAN6ClientID *string
	HWNAT        *bool
	LANGateway   *string
	GuestGateway *string
	IoTGateway   *string
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
	o.cidr(wanStaticAddr, u.WANStaticAddr)
	o.ip(wanGateway, u.WANGateway)
	o.ips(wanDNS, u.WANDNS)
	rpc.Put(o.m, wanUsername, u.WANUsername)
	rpc.Put(o.m, wanPassword, u.WANPassword)
	rpc.PutInt(o.m, wanVLAN, u.WANVLAN)
	o.mac(wanMAC, u.WANMAC)
	o.protocol(wan6Protocol, u.WAN6Protocol)
	rpc.Put(o.m, wan6ClientID, u.WAN6ClientID)
	rpc.Put(o.m, hwNAT, u.HWNAT)
	o.cidr(lanGateway, u.LANGateway)
	o.cidr(guestGateway, u.GuestGateway)
	o.cidr(iotGateway, u.IoTGateway)
	return o.m, o.err
}

// Interface is one logical network interface (GetNetworkInterfaces).
type Interface struct {
	Name           string   `json:"name"`
	Device         string   `json:"device"`
	Enabled        bool     `json:"enabled"`
	Protocol       string   `json:"protocol"`
	Address        string   `json:"address,omitempty"`
	Gateway        string   `json:"gateway,omitempty"`
	DNS            string   `json:"dns,omitempty"`
	Username       string   `json:"username,omitempty"`
	Password       string   `json:"password,omitempty"`
	Up             bool     `json:"up"`
	RuntimeAddress string   `json:"runtime_address,omitempty"`
	RuntimeGateway string   `json:"runtime_gateway,omitempty"`
	RuntimeDNS     []string `json:"runtime_dns"`
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
			Address:        formatAddr(f[ifAddress]),
			Gateway:        formatAddr(f[ifGateway]),
			DNS:            formatAddr(f[ifDNS]),
			Username:       rpc.String(f, ifUsername),
			Password:       rpc.String(f, ifPassword),
			Up:             rpc.Bool(f, ifUp),
			RuntimeAddress: formatAddr(f[ifRuntimeAddress]),
			RuntimeGateway: formatAddr(f[ifRuntimeGateway]),
			RuntimeDNS:     formatAddrs(f[ifRuntimeDNS]),
		})
	}
	return out
}

// InterfaceUpdate lists the interface settings to change; nil fields are
// left as they are. Address is IPv4 CIDR.
type InterfaceUpdate struct {
	Enabled  *bool
	Protocol *string
	Address  *string
	Gateway  *string
	DNS      *string
	Username *string
	Password *string
}

func (u InterfaceUpdate) options() (map[string]any, error) {
	o := optionSet{m: map[string]any{}}
	rpc.Put(o.m, ifEnabled, u.Enabled)
	o.protocol(ifProtocol, u.Protocol)
	o.cidr(ifAddress, u.Address)
	o.ip(ifGateway, u.Gateway)
	o.ip(ifDNS, u.DNS)
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

// UPnPConfig is the UPnP and NAT-PMP port mapping service configuration.
type UPnPConfig struct {
	UPnP   bool `json:"upnp_enabled"`
	NATPMP bool `json:"nat_pmp_enabled"`
	Port   int  `json:"port"`
}

// UPnP fetches the UPnP/NAT-PMP configuration.
func UPnP(ctx context.Context, c *amplifi.Client) (*UPnPConfig, error) {
	m, err := rpc.Map(ctx, c, amplifi.IfaceDevice, "GetUpnpConfig", nil)
	if err != nil {
		return nil, fmt.Errorf("UPnP config: %w", err)
	}
	return &UPnPConfig{
		UPnP:   rpc.Bool(m, upnpEnabled),
		NATPMP: rpc.Bool(m, natPMPEnabled),
		Port:   int(rpc.Int(m, upnpPort)),
	}, nil
}

// UPnPUpdate lists the UPnP settings to change; nil fields are left as they
// are.
type UPnPUpdate struct {
	UPnP   *bool
	NATPMP *bool
	Port   *int
}

// UpdateUPnP applies u with a read-modify-write.
func UpdateUPnP(ctx context.Context, c *amplifi.Client, u UPnPUpdate) error {
	changes := map[string]any{}
	rpc.Put(changes, upnpEnabled, u.UPnP)
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

// QoS fetches the QoS configuration as decoded JSON; its schema is unknown.
func QoS(ctx context.Context, c *amplifi.Client) (any, error) {
	var v any
	if err := rpc.Value(ctx, c, amplifi.IfaceDevice, "GetQosConfig", nil, &v); err != nil {
		return nil, fmt.Errorf("QoS config: %w", err)
	}
	if v == nil {
		v = map[string]any{}
	}
	return v, nil
}

// SetQoS replaces the QoS configuration. cfg is sent as JSON and should have
// the shape QoS returns.
func SetQoS(ctx context.Context, c *amplifi.Client, cfg any) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "SetQosConfig", cfg, nil); err != nil {
		return fmt.Errorf("QoS config: %w", err)
	}
	return nil
}

// optionSet collects option values that need parsing, keeping the first
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

func (o *optionSet) ip(key string, s *string) {
	if s == nil {
		return
	}
	b, err := packIP(*s)
	o.set(key, b, err)
}

func (o *optionSet) ips(key string, list []string) {
	if list == nil {
		return
	}
	out := make([]any, 0, len(list))
	for _, s := range list {
		b, err := packIP(s)
		if err != nil {
			o.set(key, nil, err)
			return
		}
		out = append(out, b)
	}
	o.set(key, out, nil)
}

func (o *optionSet) cidr(key string, s *string) {
	if s == nil {
		return
	}
	b, err := packCIDR(*s)
	o.set(key, b, err)
}

func (o *optionSet) mac(key string, m *amplifi.MAC) {
	if m == nil {
		return
	}
	b, err := packMAC(*m)
	o.set(key, b, err)
}

// packIP packs an IPv4 address as 4 byte binary
//. "" packs as empty binary.
func packIP(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return nil, fmt.Errorf("invalid IPv4 address %q", s)
	}
	b := a.As4()
	return b[:], nil
}

// packCIDR packs an IPv4 address with prefix length ("192.0.2.1/24") as 5
// byte binary: the address followed by the prefix length
//. "" packs as empty
// binary.
func packCIDR(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil || !p.Addr().Is4() {
		return nil, fmt.Errorf("invalid IPv4 address/prefix %q", s)
	}
	b := p.Addr().As4()
	return append(b[:], byte(p.Bits())), nil
}

// packMAC packs a MAC address as 6 byte binary. An empty MAC packs as empty
// binary, which clears a MAC override.
func packMAC(m amplifi.MAC) ([]byte, error) {
	if len(m) == 0 {
		return []byte{}, nil
	}
	if len(m) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q", m.String())
	}
	return []byte(m), nil
}

// formatAddr formats a packed address: 4 or 16 bytes is a plain IPv4 or IPv6
// address, 5 or 17 bytes the same followed by a prefix length. Anything else
// non-empty is shown as hex.
func formatAddr(v any) string {
	b, ok := v.([]byte)
	if !ok || len(b) == 0 {
		return ""
	}

	switch len(b) {
	case 4, 16:
		a, _ := netip.AddrFromSlice(b)
		return a.Unmap().String()
	case 5, 17:
		a, _ := netip.AddrFromSlice(b[:len(b)-1])
		return a.Unmap().String() + "/" + strconv.Itoa(int(b[len(b)-1]))
	default:
		return hex.EncodeToString(b)
	}
}

// formatAddrs formats a list of packed addresses. A single binary value is
// accepted too.
func formatAddrs(v any) []string {
	out := []string{}
	switch t := v.(type) {
	case []byte:
		if s := formatAddr(t); s != "" {
			out = append(out, s)
		}
	case []any:
		for _, item := range t {
			if s := formatAddr(item); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
