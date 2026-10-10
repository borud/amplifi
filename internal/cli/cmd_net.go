package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

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
	Show netIfacesShowCmd `kong:"cmd,help='list the network interfaces'"`
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
		nc, err := network.Config(ctx, cl)
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
			{"static address", nc.WANStaticAddr},
			{"gateway", nc.WANGateway},
			{"dns", strings.Join(nc.WANDNS, ", ")},
			{"username", nc.WANUsername},
			{"password", nc.WANPassword},
			{"vlan", strconv.Itoa(nc.WANVLAN)},
			{"mac override", nc.WANMAC.String()},
			{"hw nat", onOff(nc.HWNAT)},
			{"ipv6", nc.WAN6Protocol},
			{"ipv6 client id", nc.WAN6ClientID},
			{"up", onOff(nc.WANUp)},
			{"address", nc.RuntimeWANAddress},
			{"current gateway", nc.RuntimeWANGateway},
			{"current dns", strings.Join(nc.RuntimeWANDNS, ", ")},
			{"ipv6 up", onOff(nc.WAN6Up)},
			{"ipv6 address", strings.Join(nc.RuntimeWAN6Address, ", ")},
			{"ipv6 gateway", strings.Join(nc.RuntimeWAN6Gateway, ", ")},
			{"ipv6 dns", strings.Join(nc.RuntimeWAN6DNS, ", ")},
		}))
	})
}

// netWANType is the WAN type as the set command names it.
func netWANType(nc *network.Configuration) string {
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
	return apply(opt, &c.unitFlags, c.yesFlag, prompt, network.Config, c.plan)
}

// plan maps the flags to a ConfigUpdate and records them against cur. The
// bridge type is DHCP with bridge mode on; every other type turns bridge
// mode off.
func (c *netWANSetCmd) plan(cs *changeSet, cur *network.Configuration) writeFunc {
	u := network.ConfigUpdate{
		WANStaticAddr: cs.text("static address", c.Addr, cur.WANStaticAddr),
		WANGateway:    cs.text("gateway", c.Gateway, cur.WANGateway),
		WANUsername:   cs.text("username", c.Username, cur.WANUsername),
		WANVLAN:       cs.number("vlan", c.VLAN, int64(cur.WANVLAN)),
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
		u.WANDNS = nonEmpty(c.DNS)
		cs.add("dns", orDash(strings.Join(cur.WANDNS, ", ")), orDash(strings.Join(u.WANDNS, ", ")))
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
	LANGateway   string `json:"lan_gateway"`
	GuestGateway string `json:"guest_gateway"`
	IoTGateway   string `json:"iot_gateway"`
}

// Run prints the LAN gateways.
func (c *netLANShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		nc, err := network.Config(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(netLAN{LANGateway: nc.LANGateway, GuestGateway: nc.GuestGateway, IoTGateway: nc.IoTGateway})
		}
		return printPairs(os.Stdout, nonEmptyPairs([][2]string{
			{"lan", nc.LANGateway},
			{"guest", nc.GuestGateway},
			{"iot", nc.IoTGateway},
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
	return apply(opt, &c.unitFlags, c.yesFlag, prompt, network.Config,
		func(cs *changeSet, cur *network.Configuration) writeFunc {
			u := network.ConfigUpdate{
				LANGateway:   cs.text("lan", c.Gateway, cur.LANGateway),
				GuestGateway: cs.text("guest", c.GuestGateway, cur.GuestGateway),
				IoTGateway:   cs.text("iot", c.IoTGateway, cur.IoTGateway),
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return network.UpdateConfig(ctx, cl, u) }
		})
}

type netIfacesShowCmd struct {
	unitFlags     `kong:"embed"`
	ShowPasswords bool `kong:"help='show interface passwords'"`
}

// Run lists the network interfaces.
func (c *netIfacesShowCmd) Run(opt *Options) error {
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
				f.Name, f.Device, onOff(f.Enabled), f.Protocol, f.Address, f.Gateway, f.DNS,
				onOff(f.Up), f.RuntimeAddress, f.RuntimeGateway)
		}
		return tw.Flush()
	})
}

type netIfacesSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Name      string     `kong:"arg,help='interface name, as listed by net interfaces show'"`
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
				Address:  cs.text("address", c.Addr, cur.Address),
				Gateway:  cs.text("gateway", c.Gateway, cur.Gateway),
				DNS:      cs.text("dns", c.DNS, cur.DNS),
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
		u, err := network.UPnP(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(u)
		}
		return printPairs(os.Stdout, [][2]string{
			{"upnp", onOff(u.UPnP)},
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
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("apply these UPnP changes to %s?", c.Host), network.UPnP,
		func(cs *changeSet, cur *network.UPnPConfig) writeFunc {
			u := network.UPnPUpdate{
				UPnP:   cs.onOff("upnp", c.UPnP, cur.UPnP),
				NATPMP: cs.onOff("nat-pmp", c.NATPMP, cur.NATPMP),
				Port:   cs.number("port", c.Port, int64(cur.Port)),
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
		v, err := network.QoS(ctx, cl)
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
	var cfg any
	if err := json.Unmarshal([]byte(c.JSON), &cfg); err != nil {
		return fmt.Errorf("--json: %w", err)
	}

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cur, err := network.QoS(ctx, cl)
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
		st, err := device.Internet(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(st)
		}
		pairs := [][2]string{
			{"ipv4", statusWord(st.StatusText, st.ReasonText)},
			{"ipv6", statusWord(st.IPv6StatusText, st.IPv6ReasonText)},
			{"isp", st.ISP},
		}
		if st.LinkSpeed > 0 {
			pairs = append(pairs, [2]string{"link", fmt.Sprintf("%d Mbit/s", st.LinkSpeed)})
		}
		return printPairs(os.Stdout, nonEmptyPairs(pairs))
	})
}
