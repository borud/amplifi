package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/magiclink"
)

type magiclinkCmd struct {
	Status  magiclinkStatusCmd  `kong:"cmd,help='show Teleport connectivity'"`
	Invite  magiclinkInviteCmd  `kong:"cmd,help='Teleport invite code'"`
	Clients magiclinkClientsCmd `kong:"cmd,help='Teleport clients'"`
	Pairing magiclinkPairingCmd `kong:"cmd,help='factory pairing of mesh points'"`
}

type magiclinkInviteCmd struct {
	Create magiclinkInviteCreateCmd `kong:"cmd,help='generate a new invite code'"`
	Show   magiclinkInviteShowCmd   `kong:"cmd,help='show the current invite code'"`
	Update magiclinkInviteUpdateCmd `kong:"cmd,help='change the validity of the current invite code'"`
	Revoke magiclinkInviteRevokeCmd `kong:"cmd,help='invalidate the current invite code'"`
}

type magiclinkClientsCmd struct {
	List magiclinkClientsListCmd `kong:"cmd,help='list Teleport clients'"`
	Set  magiclinkClientsSetCmd  `kong:"cmd,help='change the name or access of a client'"`
	Rm   magiclinkClientsRmCmd   `kong:"cmd,help='delete a client'"`
}

type magiclinkPairingCmd struct {
	Show   magiclinkPairingShowCmd   `kong:"cmd,help='show the factory pairing state'"`
	Accept magiclinkPairingAcceptCmd `kong:"cmd,help='accept a pairing request'"`
	Reject magiclinkPairingRejectCmd `kong:"cmd,help='reject a pairing request'"`
}

type magiclinkStatusCmd struct {
	unitFlags `kong:"embed"`
}

// magiclinkStatus is what the status command gathers. A part the unit
// refuses is nil and its error is recorded in Errors.
type magiclinkStatus struct {
	Router    *magiclink.RouterConnectivity `json:"router,omitempty"`
	MagicLink *magiclink.Connection         `json:"magiclink,omitempty"`
	Errors    map[string]string             `json:"errors,omitempty"`
}

// Run prints the Teleport connectivity.
func (c *magiclinkStatusCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		st := magiclinkStatus{Errors: map[string]string{}}

		router, routerErr := magiclink.RouterStatus(ctx, cl)
		if err := partError(st.Errors, "router", routerErr); err != nil {
			return err
		}
		st.Router = router

		link, linkErr := magiclink.Status(ctx, cl)
		if err := partError(st.Errors, "magiclink", linkErr); err != nil {
			return err
		}
		st.MagicLink = link

		if opt.JSON() {
			return printJSON(st)
		}

		w := newTable(os.Stdout)
		if r := st.Router; r != nil {
			fmt.Fprintf(w, "local\t%s\n", r.Local)
			fmt.Fprintf(w, "upnp\t%s\n", r.UPnP)
			magiclinkPrintMap(w, "remote", r.Remote)
		}
		if m := st.MagicLink; m != nil {
			fmt.Fprintf(w, "state\t%s\n", m.State)
			fmt.Fprintf(w, "same network\t%s\n", m.SameNetwork)
			fmt.Fprintf(w, "network conflict\t%s\n", m.NetworkConflict)
			magiclinkPrintMap(w, "status", m.Status)
		}
		for _, k := range slices.Sorted(maps.Keys(st.Errors)) {
			fmt.Fprintf(w, "%s\tunavailable: %s\n", k, st.Errors[k])
		}
		return w.Flush()
	})
}

func magiclinkPrintMap(w io.Writer, prefix string, m map[string]string) {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(w, "%s.%s\t%s\n", prefix, k, m[k])
	}
}

// magiclinkInviteFlags are the settings of a new or updated invite.
type magiclinkInviteFlags struct {
	Duration time.Duration `kong:"help='how long the code stays valid, e.g. 24h'"`
	Sessions *int          `kong:"help='how many devices may join with the code'"`
}

func (f magiclinkInviteFlags) update() magiclink.InviteUpdate {
	var u magiclink.InviteUpdate
	if f.Duration > 0 {
		secs := int(f.Duration / time.Second)
		u.Duration = &secs
	}
	u.Sessions = f.Sessions
	return u
}

type magiclinkInviteCreateCmd struct {
	unitFlags            `kong:"embed"`
	yesFlag              `kong:"embed"`
	magiclinkInviteFlags `kong:"embed"`
}

// Run generates a new invite code and prints it.
func (c *magiclinkInviteCreateCmd) Run(opt *Options) error {
	u := c.update()
	fmt.Printf("new invite code: duration %s, sessions %s\n", magiclinkSeconds(u.Duration), intOrDash(u.Sessions))
	if !c.approve("generate a new invite code on " + c.Host + "? Any current code stops working") {
		return nil
	}

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		inv, err := magiclink.CreateInvite(ctx, cl, u)
		if err != nil {
			return err
		}
		return magiclinkPrintInvite(opt, inv)
	})
}

type magiclinkInviteShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the current invite code.
func (c *magiclinkInviteShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		inv, err := magiclink.Invite(ctx, cl)
		if err != nil {
			return err
		}
		return magiclinkPrintInvite(opt, inv)
	})
}

type magiclinkInviteUpdateCmd struct {
	unitFlags            `kong:"embed"`
	yesFlag              `kong:"embed"`
	magiclinkInviteFlags `kong:"embed"`
}

// Run changes the validity of the current invite code and prints the
// result, so it does not use apply.
func (c *magiclinkInviteUpdateCmd) Run(opt *Options) error {
	u := c.update()

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cur, err := magiclink.Invite(ctx, cl)
		if err != nil {
			return err
		}

		var cs changeSet
		if u.Duration != nil {
			cs.add("duration", magiclinkSeconds(cur.Duration), magiclinkSeconds(u.Duration))
		}
		if u.Sessions != nil {
			cs.add("sessions", intOrDash(cur.Sessions), intOrDash(u.Sessions))
		}
		ok, err := cs.approve(c.yesFlag, fmt.Sprintf("change invite code %s on %s?", cur.Code, c.Host))
		if !ok || err != nil {
			return err
		}

		// A fresh timeout for the write, as in apply.
		wctx, cancel := c.context()
		defer cancel()
		inv, err := magiclink.UpdateInvite(wctx, cl, u)
		if err != nil {
			return err
		}
		return magiclinkPrintInvite(opt, inv)
	})
}

type magiclinkInviteRevokeCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run invalidates the current invite code.
func (c *magiclinkInviteRevokeCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "revoke the invite code on "+c.Host+"?", magiclink.Invite,
		func(cs *changeSet, cur *magiclink.InviteCode) writeFunc {
			cs.remove("invite code", cur.Code)
			return magiclink.RevokeInvite
		})
}

func magiclinkSeconds(p *int) string {
	if p == nil {
		return "-"
	}
	return (time.Duration(*p) * time.Second).String()
}

func magiclinkPrintInvite(opt *Options, inv *magiclink.InviteCode) error {
	if opt.JSON() {
		return printJSON(inv)
	}

	rows := [][2]string{
		{"code", inv.Code},
		{"duration", magiclinkSeconds(inv.Duration)},
		{"sessions", intOrDash(inv.Sessions)},
		{"remaining", intOrDash(inv.Remaining)},
	}
	if inv.Expiration != nil && *inv.Expiration > 0 {
		rows = append(rows, [2]string{"expires", unixTime(*inv.Expiration)})
	}
	if inv.Success != nil {
		rows = append(rows, [2]string{"success", strconv.FormatBool(*inv.Success)})
	}
	return printPairs(os.Stdout, rows)
}

type magiclinkClientsListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the Teleport clients.
func (c *magiclinkClientsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		clients, err := magiclink.Clients(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(clients)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "ID\tNAME\tACCESS\tONLINE\tSTATE\tCONN\tIP\tPUBLIC IP\tMAC\tPLATFORM\tJOINED")
		for _, t := range clients {
			fmt.Fprintf(w, "%s\t%s\t%s\t%t\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				t.ID, t.FriendlyName, t.AccessMode, t.Online, t.State, t.ConnectionType,
				t.IP, t.PublicIP, orDash(t.MAC.String()), t.Platform, unixTime(t.Joined))
		}
		return w.Flush()
	})
}

type magiclinkClientsSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string  `kong:"arg,help='client ID or name'"`
	Name      *string `kong:"help='new friendly name'"`
	Access    string  `kong:"help='access mode: full (LAN and internet) or wan (internet only)'"`
}

// Run changes the name or access mode of a Teleport client.
func (c *magiclinkClientsSetCmd) Run(opt *Options) error {
	mode := magiclink.AccessMode(strings.ToUpper(c.Access))
	if mode != "" && mode != magiclink.AccessFull && mode != magiclink.AccessWAN {
		return fmt.Errorf("bad access mode %q: want full or wan", c.Access)
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change client %s on %s?", c.Client, c.Host), magiclinkClientReader(c.Client),
		func(cs *changeSet, t magiclink.Client) writeFunc {
			cs.text("name", c.Name, t.FriendlyName)
			if mode != "" {
				cs.add("access", string(t.AccessMode), string(mode))
			}
			return func(ctx context.Context, cl *amplifi.Client) error {
				// RtModifyClient accepts one field per call.
				if c.Name != nil {
					if err := magiclink.RenameClient(ctx, cl, t.ID, *c.Name); err != nil {
						return err
					}
				}
				if mode == "" {
					return nil
				}
				return magiclink.SetClientAccess(ctx, cl, t.ID, mode)
			}
		})
}

type magiclinkClientsRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string `kong:"arg,help='client ID or name'"`
}

// Run deletes a Teleport client.
func (c *magiclinkClientsRmCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "delete this client on "+c.Host+"?", magiclinkClientReader(c.Client),
		func(cs *changeSet, t magiclink.Client) writeFunc {
			cs.remove("client "+t.ID, fmt.Sprintf("%q (%s, %s)", t.FriendlyName, orDash(t.Platform), orDash(t.MAC.String())))
			return func(ctx context.Context, cl *amplifi.Client) error { return magiclink.DeleteClient(ctx, cl, t.ID) }
		})
}

// magiclinkClientReader returns a reader that fetches the Teleport clients
// and finds ref, an ID or friendly name, among them.
func magiclinkClientReader(ref string) func(context.Context, *amplifi.Client) (magiclink.Client, error) {
	return func(ctx context.Context, cl *amplifi.Client) (magiclink.Client, error) {
		clients, err := magiclink.Clients(ctx, cl)
		if err != nil {
			return magiclink.Client{}, err
		}
		return findOne(clients, ref, "client",
			func(t magiclink.Client) string { return t.ID },
			func(t magiclink.Client) []string { return []string{t.FriendlyName} })
	}
}

type magiclinkPairingShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the factory pairing state.
func (c *magiclinkPairingShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		pairings, err := magiclink.Pairings(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(pairings)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "PEER\tNAME\tSTATE")
		for _, p := range pairings {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.PeerID, p.FriendlyName, p.State)
		}
		return w.Flush()
	})
}

type magiclinkPairingAcceptCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Peer      string `kong:"arg,help='peer ID, as listed by pairing show'"`
}

// Run accepts a pairing request.
func (c *magiclinkPairingAcceptCmd) Run(opt *Options) error {
	return magiclinkPair(opt, &c.unitFlags, c.yesFlag, c.Peer, true)
}

type magiclinkPairingRejectCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Peer      string `kong:"arg,help='peer ID, as listed by pairing show'"`
}

// Run rejects a pairing request.
func (c *magiclinkPairingRejectCmd) Run(opt *Options) error {
	return magiclinkPair(opt, &c.unitFlags, c.yesFlag, c.Peer, false)
}

func magiclinkPair(opt *Options, u *unitFlags, y yesFlag, peer string, accept bool) error {
	verb, pair := "reject", magiclink.RejectPairing
	if accept {
		verb, pair = "accept", magiclink.AcceptPairing
	}

	prompt := fmt.Sprintf("%s pairing with %q on %s?", verb, peer, u.Host)
	return u.run(opt, y, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return pair(ctx, cl, peer)
	})
}
