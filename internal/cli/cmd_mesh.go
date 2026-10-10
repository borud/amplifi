package cli

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/mesh"
)

type meshCmd struct {
	List         meshListCmd         `kong:"cmd,aliases='ls',help='list the mesh points connected to the router'"`
	Units        meshUnitsCmd        `kong:"cmd,help='list every unit registered in the mesh'"`
	Signal       meshSignalCmd       `kong:"cmd,help='show the signal quality of wireless mesh links'"`
	Ports        meshPortsCmd        `kong:"cmd,help='show the Ethernet ports of every unit'"`
	Pairings     meshPairingsCmd     `kong:"cmd,help='factory pairing of new mesh points'"`
	Ignore       meshIgnoreCmd       `kong:"cmd,help='stop offering units for adoption into the mesh'"`
	FactoryReset meshFactoryResetCmd `kong:"cmd,help='factory reset a mesh point (irreversible)'"`
}

type meshPairingsCmd struct {
	List   meshPairingsListCmd   `kong:"cmd,aliases='ls',help='list units waiting to pair, and their state'"`
	Accept meshPairingsAcceptCmd `kong:"cmd,help='accept a pairing request'"`
	Reject meshPairingsRejectCmd `kong:"cmd,help='reject a pairing request'"`
}

type meshListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the connected mesh points.
func (c *meshListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		peers, err := mesh.Peers(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(peers)
		}

		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "NAME\tROLE\tIP\tMAC\tUPTIME\tBACKHAUL\tMASTER")
		for _, p := range peers {
			backhaul, master := "", ""
			if p.Role != amplifi.RoleRouter {
				backhaul = fmt.Sprintf("%s %d dBm", p.Band, p.RSSI)
				master = p.Master.String()
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.Name, p.Role, p.IP, p.MAC, p.Uptime.Truncate(time.Minute), backhaul, master)
		}
		return tw.Flush()
	})
}

type meshUnitsCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the registered mesh units.
func (c *meshUnitsCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		units, err := mesh.Units(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(units)
		}

		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "NAME\tROLE\tMAC\tPLATFORM")
		for _, u := range units {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", u.Name, u.Role, u.MAC, u.Platform)
		}
		return tw.Flush()
	})
}

type meshSignalCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the peers' wireless signal quality.
func (c *meshSignalCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		sig, err := mesh.Signals(ctx, cl)
		if amplifi.IsRouterError(err) {
			return fmt.Errorf("%w (not supported by this firmware; 'mesh list' shows each mesh point's backhaul RSSI)", err)
		}
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(sig)
		}

		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "MAC\tBAND\tNETWORK\tQUALITY")
		for _, s := range sig {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", s.MAC, s.Band, s.Network, s.Quality)
		}
		return tw.Flush()
	})
}

type meshPortsCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the Ethernet port state of every unit.
func (c *meshPortsCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		ports, err := mesh.Ports(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(ports)
		}

		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "UNIT\tPORT\tLINK\tSPEED\tRX\tTX")
		for _, p := range ports {
			link, speed := "down", ""
			if p.Link {
				link, speed = "up", fmt.Sprintf("%d Mbit/s", p.LinkSpeedMbps)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", p.Unit, p.Port, link, speed, mbit(p.RxKbps), mbit(p.TxKbps))
		}
		return tw.Flush()
	})
}

type meshIgnoreCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MACs      []string `kong:"arg,name='mac',help='MAC addresses of the units to ignore'"`
}

// Run asks the router to ignore the given units.
func (c *meshIgnoreCmd) Run(opt *Options) error {
	macs, err := parseMACs(c.MACs)
	if err != nil {
		return err
	}

	return c.run(opt, c.yesFlag, fmt.Sprintf("ignore %s on %s?", joinMACs(macs), c.Host), func(ctx context.Context, cl *amplifi.Client) error {
		return mesh.Ignore(ctx, cl, macs...)
	})
}

type meshFactoryResetCmd struct {
	unitFlags `kong:"embed"`
	resetFlag `kong:"embed"`
	MAC       string `kong:"arg,name='mac',help='MAC address of the mesh point to reset'"`
}

// Run factory resets one mesh point after the user types its MAC address. A
// unit missing from the router's non-empty factory reset list is refused.
func (c *meshFactoryResetCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	if !c.confirmName(mac.String()) {
		return nil
	}

	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error {
		// The list's format is not documented, so an unreadable or empty
		// list does not block the reset.
		resettable, err := mesh.ResettableUnits(ctx, cl)
		if err != nil {
			opt.Logger().Warn("could not read the list of resettable units", "err", err)
		}
		if len(resettable) > 0 && !slices.ContainsFunc(resettable, mac.Equal) {
			return fmt.Errorf("%s is not in the router's factory reset list (%s)", mac, joinMACs(resettable))
		}
		return mesh.FactoryReset(ctx, cl, mac)
	})
}

type meshPairingsListCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the factory pairing state.
func (c *meshPairingsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		pairings, err := mesh.Pairings(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(pairings)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "PEER\tNAME\tSTATE")
		for _, p := range pairings {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.PeerID, p.Name, p.State)
		}
		return w.Flush()
	})
}

type meshPairingsAcceptCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Peer      string `kong:"arg,help='peer ID, as listed by mesh pairings list'"`
}

// Run accepts a pairing request.
func (c *meshPairingsAcceptCmd) Run(opt *Options) error {
	return meshPair(opt, &c.unitFlags, c.yesFlag, c.Peer, true)
}

type meshPairingsRejectCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Peer      string `kong:"arg,help='peer ID, as listed by mesh pairings list'"`
}

// Run rejects a pairing request.
func (c *meshPairingsRejectCmd) Run(opt *Options) error {
	return meshPair(opt, &c.unitFlags, c.yesFlag, c.Peer, false)
}

func meshPair(opt *Options, u *unitFlags, y yesFlag, peer string, accept bool) error {
	verb, pair := "reject", mesh.RejectPairing
	if accept {
		verb, pair = "accept", mesh.AcceptPairing
	}

	prompt := fmt.Sprintf("%s pairing with %q on %s?", verb, peer, u.Host)
	return u.run(opt, y, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return pair(ctx, cl, peer)
	})
}
