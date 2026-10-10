package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/device"
	"github.com/borud/amplifi/network"
)

type netCmd struct {
	WAN        netWANCmd       `kong:"cmd,name='wan',help='WAN (internet) connection'"`
	LAN        netLANCmd       `kong:"cmd,name='lan',help='LAN, guest and IoT gateway addresses'"`
	Interfaces netIfacesCmd    `kong:"cmd,help='logical network interfaces'"`
	UPnP       netUPnPCmd      `kong:"cmd,name='upnp',help='UPnP and NAT-PMP'"`
	QoS        netQoSCmd       `kong:"cmd,name='qos',help='QoS configuration (raw JSON)'"`
	InetCheck  netInetCheckCmd `kong:"cmd,name='inet-check',help='run the internet connectivity check'"`
}

type netWANCmd struct {
	Show netWANShowCmd `kong:"cmd,help='show the WAN configuration and its current state'"`
	Set  netWANSetCmd  `kong:"cmd,help='change the WAN configuration'"`
}

type netLANCmd struct {
	Show netLANShowCmd `kong:"cmd,help='show the LAN, guest and IoT gateway addresses'"`
	Set  netLANSetCmd  `kong:"cmd,help='change the LAN, guest or IoT gateway address'"`
}

type netIfacesCmd struct {
	List netIfacesListCmd `kong:"cmd,aliases='ls',help='list the network interfaces'"`
	Set  netIfacesSetCmd  `kong:"cmd,help='change a network interface'"`
}

type netUPnPCmd struct {
	Show netUPnPShowCmd `kong:"cmd,help='show the UPnP and NAT-PMP configuration'"`
	Set  netUPnPSetCmd  `kong:"cmd,help='change the UPnP and NAT-PMP configuration'"`
}

type netQoSCmd struct {
	Show netQoSShowCmd `kong:"cmd,help='show the QoS configuration as JSON'"`
	Set  netQoSSetCmd  `kong:"cmd,help='replace the QoS configuration with JSON'"`
}

type netWANShowCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='show the PPPoE password'"`
}

// Run prints the WAN configuration.
func (c *netWANShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		nc, err := network.GetConfig(ctx, cl)
		if err != nil {
			return err
		}
		if !c.ShowPasswords {
			*nc = nc.Redacted()
		}

		if opt.JSON() {
			return printJSON(nc)
		}
		return printPairs(os.Stdout, nonEmptyPairs([][2]string{
			{"type", netWANType(nc)},
			{"static address", prefixText(nc.WANStaticAddr)},
			{"gateway", addrText(nc.WANGateway)},
			{"dns", addrsText(nc.WANDNS)},
			{"username", nc.WANUsername},
			{"password", nc.WANPassword},
			{"vlan", strconv.Itoa(nc.WANVLAN)},
			{"mac override", nc.WANMAC.String()},
			{"hw nat", onOff(nc.HWNAT)},
			{"ipv6", nc.WAN6Protocol},
			{"ipv6 client id", nc.WAN6ClientID},
			{"up", onOff(nc.WANUp)},
			{"address", prefixText(nc.RuntimeWANAddress)},
			{"current gateway", addrText(nc.RuntimeWANGateway)},
			{"current dns", addrsText(nc.RuntimeWANDNS)},
			{"ipv6 up", onOff(nc.WAN6Up)},
			{"ipv6 address", addrsText(nc.RuntimeWAN6Address)},
			{"ipv6 gateway", addrsText(nc.RuntimeWAN6Gateway)},
			{"ipv6 dns", addrsText(nc.RuntimeWAN6DNS)},
		}))
	})
}

// netWANType is the WAN type as the set command names it.
func netWANType(nc *network.Config) string {
	if nc.WANBridge {
		return "bridge"
	}
	return nc.WANType
}

type netWANSetCmd struct {
	unitFlags     `kong:"embed"`
	yesFlag       `kong:"embed"`
	Type          *string    `kong:"help='connection type: dhcp, static, pppoe or bridge'"`
	Addr          *string    `kong:"help='static WAN address with prefix, e.g. 203.0.113.10/24'"`
	Gateway       *string    `kong:"help='static WAN gateway'"`
	DNS           []string   `kong:"name='dns',sep=',',help='static DNS servers, comma separated (empty to clear)'"`
	Username      *string    `kong:"name='pppoe-username',help='PPPoE username'"`
	Password      *string    `kong:"name='pppoe-password',help='PPPoE password'"`
	VLAN          *int       `kong:"name='vlan',help='WAN VLAN ID (0 for none)'"`
	MAC           *string    `kong:"name='mac',help='WAN MAC address override (empty to clear)'"`
	IPv6          *string    `kong:"name='ipv6',help='IPv6 protocol, e.g. off, dhcpv6, static, 6to4 or relay'"`
	IPv6ClientID  *string    `kong:"name='ipv6-client-id',help='DHCPv6 client ID'"`
	HWNAT         *onOffFlag `kong:"name='hw-nat',help='hardware NAT acceleration (on|off)',placeholder='on|off'"`
	ShowPasswords bool       `kong:"help='show passwords in the change summary'"`
}

// Run changes the WAN configuration.
func (c *netWANSetCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("apply these WAN changes to %s? This can cut its internet connection", c.Host)
	return apply(opt, &c.unitFlags, c.yesFlag, prompt, network.GetConfig, c.plan)
}

// plan maps the flags to a ConfigUpdate and records them against cur. The
// bridge type is DHCP with bridge mode on; every other type turns bridge
// mode off.
func (c *netWANSetCmd) plan(cs *changeSet, cur *network.Config) writeFunc {
	u := network.ConfigUpdate{
		WANStaticAddr: cs.prefix("static address", c.Addr, cur.WANStaticAddr),
		WANGateway:    cs.addr("gateway", c.Gateway, cur.WANGateway),
		WANUsername:   cs.text("username", c.Username, cur.WANUsername),
		WANVLAN:       cs.number("vlan", c.VLAN, cur.WANVLAN),
		WAN6Protocol:  cs.text("ipv6", c.IPv6, cur.WAN6Protocol),
		WAN6ClientID:  cs.text("ipv6 client id", c.IPv6ClientID, cur.WAN6ClientID),
		HWNAT:         cs.onOff("hw nat", c.HWNAT, cur.HWNAT),
	}

	if c.Type != nil {
		typ, bridge := *c.Type, false
		switch typ {
		case "dhcp", "static", "pppoe":
		case "bridge":
			typ, bridge = "dhcp", true
		default:
			cs.fail("type", fmt.Errorf("want dhcp, static, pppoe or bridge, not %q", typ))
		}
		u.WANType, u.WANBridge = &typ, &bridge
		cs.add("type", netWANType(cur), *c.Type)
	}
	if c.DNS != nil {
		dns, err := netParseAddrs(nonEmpty(c.DNS))
		if err != nil {
			cs.fail("dns", err)
		}
		u.WANDNS = dns
		cs.add("dns", orDash(addrsText(cur.WANDNS)), orDash(addrsText(dns)))
	}
	u.WANPassword = c.Password
	switch {
	case c.Password == nil:
	case c.ShowPasswords:
		cs.text("password", c.Password, cur.WANPassword)
	default:
		cs.secret("password", c.Password)
	}
	if c.MAC != nil {
		mac, err := netParseMAC(*c.MAC)
		if err != nil {
			cs.fail("mac", err)
		}
		u.WANMAC = &mac
		cs.add("mac override", orDash(cur.WANMAC.String()), orDash(mac.String()))
	}
	return func(ctx context.Context, cl *amplifi.Client) error { return network.UpdateConfig(ctx, cl, u) }
}

// netParseAddrs parses a list of IP addresses.
func netParseAddrs(list []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(list))
	for _, s := range list {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// netParseMAC parses a MAC address flag; "" is the empty MAC that clears an
// override.
func netParseMAC(s string) (amplifi.MAC, error) {
	if s == "" {
		return amplifi.MAC{}, nil
	}
	return amplifi.ParseMAC(s)
}

type netLANShowCmd struct {
	unitFlags `kong:"embed"`
}

// netLAN is the LAN part of the network configuration.
type netLAN struct {
	LANGateway   netip.Prefix `json:"lan_gateway"`
	GuestGateway netip.Prefix `json:"guest_gateway"`
	IoTGateway   netip.Prefix `json:"iot_gateway"`
}

// Run prints the LAN gateways.
func (c *netLANShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		nc, err := network.GetConfig(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(netLAN{LANGateway: nc.LANGateway, GuestGateway: nc.GuestGateway, IoTGateway: nc.IoTGateway})
		}
		return printPairs(os.Stdout, nonEmptyPairs([][2]string{
			{"lan", prefixText(nc.LANGateway)},
			{"guest", prefixText(nc.GuestGateway)},
			{"iot", prefixText(nc.IoTGateway)},
		}))
	})
}

type netLANSetCmd struct {
	unitFlags    `kong:"embed"`
	yesFlag      `kong:"embed"`
	Gateway      *string `kong:"help='LAN gateway address with prefix, e.g. 192.168.1.1/24'"`
	GuestGateway *string `kong:"help='guest network gateway address with prefix'"`
	IoTGateway   *string `kong:"name='iot-gateway',help='IoT network gateway address with prefix'"`
}

// Run changes the LAN gateways.
func (c *netLANSetCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("apply these LAN changes to %s? Clients will get new addresses and this connection may drop", c.Host)
	return apply(opt, &c.unitFlags, c.yesFlag, prompt, network.GetConfig,
		func(cs *changeSet, cur *network.Config) writeFunc {
			u := network.ConfigUpdate{
				LANGateway:   cs.prefix("lan", c.Gateway, cur.LANGateway),
				GuestGateway: cs.prefix("guest", c.GuestGateway, cur.GuestGateway),
				IoTGateway:   cs.prefix("iot", c.IoTGateway, cur.IoTGateway),
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return network.UpdateConfig(ctx, cl, u) }
		})
}

type netIfacesListCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='show interface passwords'"`
}

// Run lists the network interfaces.
func (c *netIfacesListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		ifaces, err := network.Interfaces(ctx, cl)
		if err != nil {
			return err
		}
		if !c.ShowPasswords {
			for i := range ifaces {
				ifaces[i] = ifaces[i].Redacted()
			}
		}

		if opt.JSON() {
			return printJSON(ifaces)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "NAME\tDEVICE\tENABLED\tPROTO\tADDRESS\tGATEWAY\tDNS\tUP\tRUNTIME ADDRESS\tRUNTIME GATEWAY")
		for _, f := range ifaces {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				f.Name, f.Device, onOff(f.Enabled), f.Protocol, prefixText(f.Address), addrText(f.Gateway), addrText(f.DNS),
				onOff(f.Up), prefixText(f.RuntimeAddress), addrText(f.RuntimeGateway))
		}
		return tw.Flush()
	})
}

type netIfacesSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Name      string     `kong:"arg,help='interface name, as listed by net interfaces list'"`
	Enabled   *onOffFlag `kong:"help='enable the interface (on|off)',placeholder='on|off'"`
	Proto     *string    `kong:"help='protocol, e.g. dhcp, static, pppoe or none'"`
	Addr      *string    `kong:"help='address with prefix, e.g. 192.0.2.1/24'"`
	Gateway   *string    `kong:"help='gateway address'"`
	DNS       *string    `kong:"name='dns',help='DNS server address'"`
	Username  *string    `kong:"name='pppoe-username',help='PPPoE username'"`
	Password  *string    `kong:"name='pppoe-password',help='PPPoE password'"`
}

// Run changes one network interface.
func (c *netIfacesSetCmd) Run(opt *Options) error {
	read := func(ctx context.Context, cl *amplifi.Client) (network.Interface, error) {
		ifaces, err := network.Interfaces(ctx, cl)
		if err != nil {
			return network.Interface{}, err
		}
		return findOne(ifaces, c.Name, "network interface", func(f network.Interface) string { return f.Name }, nil)
	}
	prompt := fmt.Sprintf("change interface %s on %s? This can cut network connectivity", c.Name, c.Host)
	return apply(opt, &c.unitFlags, c.yesFlag, prompt, read,
		func(cs *changeSet, cur network.Interface) writeFunc {
			u := network.InterfaceUpdate{
				Enabled:  cs.onOff("enabled", c.Enabled, cur.Enabled),
				Protocol: cs.text("protocol", c.Proto, cur.Protocol),
				Address:  cs.prefix("address", c.Addr, cur.Address),
				Gateway:  cs.addr("gateway", c.Gateway, cur.Gateway),
				DNS:      cs.addr("dns", c.DNS, cur.DNS),
				Username: cs.text("username", c.Username, cur.Username),
				Password: cs.secret("password", c.Password),
			}
			return func(ctx context.Context, cl *amplifi.Client) error {
				return network.UpdateInterface(ctx, cl, cur.Name, u)
			}
		})
}

type netUPnPShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the UPnP configuration.
func (c *netUPnPShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		u, err := network.GetUPnP(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(u)
		}
		return printPairs(os.Stdout, [][2]string{
			{"upnp", onOff(u.Enabled)},
			{"nat-pmp", onOff(u.NATPMP)},
			{"port", strconv.Itoa(u.Port)},
		})
	})
}

type netUPnPSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	UPnP      *onOffFlag `kong:"name='upnp',help='enable UPnP (on|off)',placeholder='on|off'"`
	NATPMP    *onOffFlag `kong:"name='nat-pmp',help='enable NAT-PMP (on|off)',placeholder='on|off'"`
	Port      *int       `kong:"help='service port'"`
}

// Run changes the UPnP configuration.
func (c *netUPnPSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("apply these UPnP changes to %s?", c.Host), network.GetUPnP,
		func(cs *changeSet, cur *network.UPnP) writeFunc {
			u := network.UPnPUpdate{
				Enabled: cs.onOff("upnp", c.UPnP, cur.Enabled),
				NATPMP:  cs.onOff("nat-pmp", c.NATPMP, cur.NATPMP),
				Port:    cs.number("port", c.Port, cur.Port),
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return network.UpdateUPnP(ctx, cl, u) }
		})
}

type netQoSShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the QoS configuration. It is raw JSON in either output format.
func (c *netQoSShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		v, err := network.GetQoS(ctx, cl)
		if err != nil {
			return err
		}
		return printJSON(v)
	})
}

type netQoSSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	JSON      string `kong:"name='json',required,help='the new QoS configuration as JSON, shaped like the output of net qos show'"`
}

// Run replaces the QoS configuration. The change is a whole JSON document,
// so it is shown as before and after documents rather than a change set.
func (c *netQoSSetCmd) Run(opt *Options) error {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(c.JSON), &cfg); err != nil {
		return fmt.Errorf("--json: %w", err)
	}

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cur, err := network.GetQoS(ctx, cl)
		if err != nil {
			return err
		}

		if !c.Yes {
			if err := netPrintQoS(cur, cfg); err != nil {
				return err
			}
		}
		if !c.approve(fmt.Sprintf("replace the QoS configuration on %s?", c.Host)) {
			return nil
		}

		// A fresh timeout for the write, as in apply.
		wctx, cancel := c.context()
		defer cancel()
		if err := network.SetQoS(wctx, cl, cfg); err != nil {
			return err
		}
		fmt.Println("done")
		return nil
	})
}

func netPrintQoS(cur, next any) error {
	fmt.Println("current:")
	if err := printJSON(cur); err != nil {
		return err
	}
	fmt.Println("new:")
	return printJSON(next)
}

type netInetCheckCmd struct {
	unitFlags `kong:"embed"`
}

// Run runs the internet connectivity check.
func (c *netInetCheckCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		st, err := device.CheckInternet(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(st)
		}
		pairs := [][2]string{
			{"ipv4", statusWord(st.HealthText, st.ReasonText)},
			{"ipv6", statusWord(st.IPv6HealthText, st.IPv6ReasonText)},
			{"isp", st.ISP},
		}
		if st.LinkSpeed > 0 {
			pairs = append(pairs, [2]string{"link speed", strconv.Itoa(st.LinkSpeed)})
		}
		return printPairs(os.Stdout, nonEmptyPairs(pairs))
	})
}
