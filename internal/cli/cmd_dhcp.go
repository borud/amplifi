package cli

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/dhcp"
)

type dhcpCmd struct {
	Pools  dhcpPoolsCmd  `kong:"cmd,help='DHCP address pools'"`
	Leases dhcpLeasesCmd `kong:"cmd,help='static DHCP leases (address reservations)'"`
}

type dhcpPoolsCmd struct {
	List dhcpPoolsListCmd `kong:"cmd,aliases='ls',help='list the DHCP pools'"`
	Add  dhcpPoolsAddCmd  `kong:"cmd,help='add a DHCP pool'"`
	Set  dhcpPoolsSetCmd  `kong:"cmd,help='change a DHCP pool'"`
	Rm   dhcpPoolsRmCmd   `kong:"cmd,help='delete DHCP pools'"`
}

type dhcpLeasesCmd struct {
	List dhcpLeasesListCmd `kong:"cmd,aliases='ls',help='list the static leases'"`
	Add  dhcpLeasesAddCmd  `kong:"cmd,help='reserve an address for a MAC address'"`
	Set  dhcpLeasesSetCmd  `kong:"cmd,help='change a static lease'"`
	Rm   dhcpLeasesRmCmd   `kong:"cmd,help='delete static leases'"`
}

type dhcpPoolsListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the DHCP pools.
func (c *dhcpPoolsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		pools, err := dhcp.Pools(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(pools)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "NAME\tINTERFACE\tIGNORE\tOFFSET\tLENGTH\tLEASE\tDHCP4\tDHCP6\tRA\tNDP")
		for _, p := range pools {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
				p.Name, p.Interface, onOff(p.Ignore), p.Offset, p.Length, dhcpLease(p.LeaseTime),
				p.DHCP4Mode, p.DHCP6Mode, p.RAMode, p.NDPMode)
		}
		return tw.Flush()
	})
}

// dhcpLease formats a lease time, taken to be in seconds.
func dhcpLease(seconds int) string {
	if seconds <= 0 {
		return strconv.Itoa(seconds)
	}
	return (time.Duration(seconds) * time.Second).String()
}

// dhcpPoolFlags are the pool settings shared by add and set.
type dhcpPoolFlags struct {
	Iface     *string        `kong:"name='iface',help='network interface the pool serves (default: the pool name)'"`
	Ignore    *onOffFlag     `kong:"help='do not serve DHCP on this interface (on|off)',placeholder='on|off'"`
	Offset    *int           `kong:"help='first host number handed out within the subnet'"`
	Length    *int           `kong:"help='number of addresses in the pool'"`
	LeaseTime *time.Duration `kong:"help='lease time, e.g. 12h'"`
	DHCP4     *string        `kong:"name='dhcp4',help='DHCPv4 mode: disabled, server, relay or hybrid'"`
	DHCP6     *string        `kong:"name='dhcp6',help='DHCPv6 mode: disabled, server, relay or hybrid'"`
	RA        *string        `kong:"name='ra',help='router advertisement mode: disabled, server, relay or hybrid'"`
	NDP       *string        `kong:"name='ndp',help='NDP proxy mode: disabled, server, relay or hybrid'"`
}

// plan maps the flags to a pool update and records them against cur.
func (f *dhcpPoolFlags) plan(cs *changeSet, cur dhcp.Pool) dhcp.PoolUpdate {
	u := dhcp.PoolUpdate{
		Interface: cs.text("interface", f.Iface, cur.Interface),
		Ignore:    cs.onOff("ignore", f.Ignore, cur.Ignore),
		Offset:    cs.number("offset", f.Offset, cur.Offset),
		Length:    cs.number("length", f.Length, cur.Length),
		DHCP4Mode: cs.text("dhcp4", f.DHCP4, cur.DHCP4Mode),
		DHCP6Mode: cs.text("dhcp6", f.DHCP6, cur.DHCP6Mode),
		RAMode:    cs.text("ra", f.RA, cur.RAMode),
		NDPMode:   cs.text("ndp", f.NDP, cur.NDPMode),
	}
	if f.LeaseTime != nil {
		secs := int(f.LeaseTime.Seconds())
		u.LeaseTime = &secs
		cs.add("lease time", dhcpLease(cur.LeaseTime), f.LeaseTime.String())
	}
	return u
}

type dhcpPoolsAddCmd struct {
	unitFlags     `kong:"embed"`
	yesFlag       `kong:"embed"`
	Name          string `kong:"arg,help='pool name, normally the interface name, e.g. lan'"`
	dhcpPoolFlags `kong:"embed"`
}

// Run adds a pool.
func (c *dhcpPoolsAddCmd) Run(opt *Options) error {
	var cs changeSet
	cs.create("pool", c.Name)
	u := c.plan(&cs, dhcp.Pool{})
	ok, err := cs.approve(c.yesFlag, fmt.Sprintf("add DHCP pool %s on %s?", c.Name, c.Host))
	if err != nil || !ok {
		return err
	}

	p := dhcp.Pool{
		Name:      c.Name,
		Interface: deref(u.Interface),
		Ignore:    deref(u.Ignore),
		Offset:    deref(u.Offset),
		Length:    deref(u.Length),
		LeaseTime: deref(u.LeaseTime),
		DHCP4Mode: deref(u.DHCP4Mode),
		DHCP6Mode: deref(u.DHCP6Mode),
		RAMode:    deref(u.RAMode),
		NDPMode:   deref(u.NDPMode),
	}
	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error { return dhcp.AddPool(ctx, cl, p) })
}

type dhcpPoolsSetCmd struct {
	unitFlags     `kong:"embed"`
	yesFlag       `kong:"embed"`
	Name          string `kong:"arg,help='pool name, as listed by dhcp pools list'"`
	dhcpPoolFlags `kong:"embed"`
}

// Run changes a pool.
func (c *dhcpPoolsSetCmd) Run(opt *Options) error {
	read := func(ctx context.Context, cl *amplifi.Client) (dhcp.Pool, error) {
		pools, err := dhcp.Pools(ctx, cl)
		if err != nil {
			return dhcp.Pool{}, err
		}
		return findOne(pools, c.Name, "DHCP pool", func(p dhcp.Pool) string { return p.Name }, nil)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change DHCP pool %s on %s?", c.Name, c.Host), read,
		func(cs *changeSet, cur dhcp.Pool) writeFunc {
			u := c.plan(cs, cur)
			return func(ctx context.Context, cl *amplifi.Client) error {
				return dhcp.UpdatePool(ctx, cl, cur.Name, u)
			}
		})
}

type dhcpPoolsRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Names     []string `kong:"arg,help='pool names'"`
}

// Run deletes pools.
func (c *dhcpPoolsRmCmd) Run(opt *Options) error {
	prompt := fmt.Sprintf("delete DHCP pool %s on %s? Clients on that network will get no addresses", strings.Join(c.Names, ", "), c.Host)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return dhcp.DeletePools(ctx, cl, c.Names...)
	})
}

type dhcpLeasesListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the static leases.
func (c *dhcpLeasesListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		leases, err := dhcp.StaticLeases(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(leases)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "MAC\tIP\tNAME\tDESCRIPTION")
		for _, l := range leases {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", l.MAC, l.IP, l.Name, l.Description)
		}
		return tw.Flush()
	})
}

type dhcpLeasesAddCmd struct {
	unitFlags   `kong:"embed"`
	yesFlag     `kong:"embed"`
	MAC         string `kong:"arg,name='mac',help='client MAC address'"`
	IP          string `kong:"arg,name='ip',help='address to reserve'"`
	Name        string `kong:"help='lease name'"`
	Description string `kong:"help='free-text description'"`
}

// Run adds a static lease.
func (c *dhcpLeasesAddCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(c.IP)
	if err != nil {
		return err
	}
	l := dhcp.StaticLease{MAC: mac, IP: ip, Name: c.Name, Description: c.Description}

	var cs changeSet
	cs.create("mac", l.MAC.String())
	cs.create("ip", l.IP.String())
	cs.create("name", l.Name)
	cs.create("description", l.Description)
	ok, err := cs.approve(c.yesFlag, fmt.Sprintf("add this static lease on %s?", c.Host))
	if err != nil || !ok {
		return err
	}
	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error {
		return dhcp.AddStaticLease(ctx, cl, l)
	})
}

type dhcpLeasesSetCmd struct {
	unitFlags   `kong:"embed"`
	yesFlag     `kong:"embed"`
	Lease       string  `kong:"arg,name='lease',help='client MAC address or name of the lease'"`
	IP          *string `kong:"name='ip',help='new reserved address'"`
	Name        *string `kong:"help='new name'"`
	Description *string `kong:"help='new description'"`
}

// Run changes a static lease.
func (c *dhcpLeasesSetCmd) Run(opt *Options) error {
	read := func(ctx context.Context, cl *amplifi.Client) (dhcp.StaticLease, error) {
		leases, err := dhcp.StaticLeases(ctx, cl)
		if err != nil {
			return dhcp.StaticLease{}, err
		}
		return dhcpFindLease(leases, c.Lease)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change the static lease %s on %s?", c.Lease, c.Host), read,
		func(cs *changeSet, cur dhcp.StaticLease) writeFunc {
			u := dhcp.StaticLeaseUpdate{
				IP:          cs.addr("ip", c.IP, cur.IP),
				Name:        cs.text("name", c.Name, cur.Name),
				Description: cs.text("description", c.Description, cur.Description),
			}
			return func(ctx context.Context, cl *amplifi.Client) error {
				return dhcp.UpdateStaticLease(ctx, cl, cur.MAC, u)
			}
		})
}

// dhcpFindLease finds a lease by MAC address, in any notation, or by name.
func dhcpFindLease(leases []dhcp.StaticLease, ref string) (dhcp.StaticLease, error) {
	if mac, err := amplifi.ParseMAC(ref); err == nil {
		ref = mac.String()
	}
	return findOne(leases, ref, "static lease",
		func(l dhcp.StaticLease) string { return l.MAC.String() },
		func(l dhcp.StaticLease) []string { return []string{l.Name} })
}

type dhcpLeasesRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MACs      []string `kong:"arg,name='mac',help='client MAC addresses'"`
}

// Run deletes static leases.
func (c *dhcpLeasesRmCmd) Run(opt *Options) error {
	macs, err := parseMACs(c.MACs)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("delete the static leases for %s on %s?", joinMACs(macs), c.Host)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return dhcp.DeleteStaticLeases(ctx, cl, macs...)
	})
}
