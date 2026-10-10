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
	"github.com/borud/amplifi/teleport"
)

type teleportCmd struct {
	Status  teleportStatusCmd  `kong:"cmd,help='show Teleport connectivity'"`
	Invite  teleportInviteCmd  `kong:"cmd,help='Teleport invite code'"`
	Clients teleportClientsCmd `kong:"cmd,help='Teleport clients'"`
}

type teleportInviteCmd struct {
	Create teleportInviteCreateCmd `kong:"cmd,help='generate a new invite code'"`
	Show   teleportInviteShowCmd   `kong:"cmd,help='show the current invite code'"`
	Update teleportInviteUpdateCmd `kong:"cmd,help='change the validity of the current invite code'"`
	Revoke teleportInviteRevokeCmd `kong:"cmd,help='invalidate the current invite code'"`
}

type teleportClientsCmd struct {
	List teleportClientsListCmd `kong:"cmd,aliases='ls',help='list Teleport clients'"`
	Set  teleportClientsSetCmd  `kong:"cmd,help='change the name or access of a client'"`
	Rm   teleportClientsRmCmd   `kong:"cmd,help='delete a client'"`
}

type teleportStatusCmd struct {
	unitFlags `kong:"embed"`
}

// teleportStatus is what the status command gathers. A part the unit
// refuses is nil and its error is recorded in Errors.
type teleportStatus struct {
	Router     *teleport.Connectivity `json:"router,omitempty"`
	Connection *teleport.Connection   `json:"connection,omitempty"`
	Errors     map[string]string      `json:"errors,omitempty"`
}

// Run prints the Teleport connectivity.
func (c *teleportStatusCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		st := teleportStatus{Errors: map[string]string{}}

		router, routerErr := teleport.GetConnectivity(ctx, cl)
		if err := partError(st.Errors, "router", routerErr); err != nil {
			return err
		}
		st.Router = router

		link, linkErr := teleport.GetConnection(ctx, cl)
		if err := partError(st.Errors, "connection", linkErr); err != nil {
			return err
		}
		st.Connection = link

		if opt.JSON() {
			return printJSON(st)
		}

		w := newTable(os.Stdout)
		if r := st.Router; r != nil {
			fmt.Fprintf(w, "local\t%s\n", r.Local)
			fmt.Fprintf(w, "upnp\t%s\n", r.UPnP)
			teleportPrintMap(w, "remote", r.Remote)
		}
		if m := st.Connection; m != nil {
			fmt.Fprintf(w, "state\t%s\n", m.State)
			fmt.Fprintf(w, "same network\t%s\n", m.SameNetwork)
			fmt.Fprintf(w, "network conflict\t%s\n", m.NetworkConflict)
			teleportPrintMap(w, "status", m.Status)
		}
		for _, k := range slices.Sorted(maps.Keys(st.Errors)) {
			fmt.Fprintf(w, "%s\tunavailable: %s\n", k, st.Errors[k])
		}
		return w.Flush()
	})
}

func teleportPrintMap(w io.Writer, prefix string, m map[string]string) {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(w, "%s.%s\t%s\n", prefix, k, m[k])
	}
}

// teleportInviteFlags are the settings of a new or updated invite.
type teleportInviteFlags struct {
	Duration *time.Duration `kong:"help='how long the code stays valid, e.g. 24h'"`
	Sessions *int           `kong:"help='how many devices may join with the code'"`
}

func (f teleportInviteFlags) update() teleport.InviteUpdate {
	return teleport.InviteUpdate{Duration: f.Duration, Sessions: f.Sessions}
}

type teleportInviteCreateCmd struct {
	unitFlags           `kong:"embed"`
	yesFlag             `kong:"embed"`
	teleportInviteFlags `kong:"embed"`
}

// Run generates a new invite code and prints it.
func (c *teleportInviteCreateCmd) Run(opt *Options) error {
	fmt.Printf("new invite code: duration %s, sessions %s\n", durationOrDash(c.Duration), intOrDash(c.Sessions))
	if !c.approve("generate a new invite code on " + c.Host + "? Any current code stops working") {
		return nil
	}

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		inv, err := teleport.CreateInvite(ctx, cl, c.update())
		if err != nil {
			return err
		}
		return teleportPrintInvite(opt, inv)
	})
}

type teleportInviteShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the current invite code.
func (c *teleportInviteShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		inv, err := teleport.GetInvite(ctx, cl)
		if err != nil {
			return err
		}
		return teleportPrintInvite(opt, inv)
	})
}

type teleportInviteUpdateCmd struct {
	unitFlags           `kong:"embed"`
	yesFlag             `kong:"embed"`
	teleportInviteFlags `kong:"embed"`
}

// Run changes the validity of the current invite code and prints the
// result, so it does not use apply.
func (c *teleportInviteUpdateCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cur, err := teleport.GetInvite(ctx, cl)
		if err != nil {
			return err
		}

		var cs changeSet
		if c.Duration != nil {
			cs.add("duration", cur.Duration.String(), c.Duration.String())
		}
		cs.number("sessions", c.Sessions, cur.Sessions)
		ok, err := cs.approve(c.yesFlag, fmt.Sprintf("change invite code %s on %s?", cur.Code, c.Host))
		if !ok || err != nil {
			return err
		}

		// A fresh timeout for the write, as in apply.
		wctx, cancel := c.context()
		defer cancel()
		inv, err := teleport.UpdateInvite(wctx, cl, c.update())
		if err != nil {
			return err
		}
		return teleportPrintInvite(opt, inv)
	})
}

type teleportInviteRevokeCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
}

// Run invalidates the current invite code.
func (c *teleportInviteRevokeCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "revoke the invite code on "+c.Host+"?", teleport.GetInvite,
		func(cs *changeSet, cur *teleport.Invite) writeFunc {
			cs.remove("invite code", cur.Code)
			return teleport.RevokeInvite
		})
}

// durationOrDash formats an optional duration, or "-" when it is unset.
func durationOrDash(d *time.Duration) string {
	if d == nil {
		return "-"
	}
	return d.String()
}

func teleportPrintInvite(opt *Options, inv *teleport.Invite) error {
	if opt.JSON() {
		return printJSON(inv)
	}
	rows := [][2]string{
		{"code", inv.Code},
		{"duration", inv.Duration.String()},
		{"sessions", strconv.Itoa(inv.Sessions)},
		{"remaining", strconv.Itoa(inv.Remaining)},
	}
	if !inv.Expires.IsZero() {
		rows = append(rows, [2]string{"expires", timeText(inv.Expires)})
	}
	return printPairs(os.Stdout, rows)
}

type teleportClientsListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the Teleport clients.
func (c *teleportClientsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		clients, err := teleport.Clients(ctx, cl)
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
				t.ID, t.Name, t.AccessMode, t.Online, t.State, t.ConnectionType,
				addrText(t.IP), addrText(t.PublicIP), orDash(t.MAC.String()), t.Platform, timeText(t.Joined))
		}
		return w.Flush()
	})
}

type teleportClientsSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string  `kong:"arg,help='client ID or name'"`
	Name      *string `kong:"help='new name'"`
	Access    string  `kong:"help='access mode: full (LAN and internet) or wan (internet only)'"`
}

// Run changes the name or access mode of a Teleport client.
func (c *teleportClientsSetCmd) Run(opt *Options) error {
	mode := teleport.AccessMode(strings.ToUpper(c.Access))
	if mode != "" && mode != teleport.AccessFull && mode != teleport.AccessWAN {
		return fmt.Errorf("bad access mode %q: want full or wan", c.Access)
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change client %s on %s?", c.Client, c.Host), teleportClientReader(c.Client),
		func(cs *changeSet, t teleport.Client) writeFunc {
			cs.text("name", c.Name, t.Name)
			if mode != "" {
				cs.add("access", string(t.AccessMode), string(mode))
			}
			return func(ctx context.Context, cl *amplifi.Client) error {
				// RtModifyClient accepts one field per call.
				if c.Name != nil {
					if err := teleport.RenameClient(ctx, cl, t.ID, *c.Name); err != nil {
						return err
					}
				}
				if mode == "" {
					return nil
				}
				return teleport.SetClientAccess(ctx, cl, t.ID, mode)
			}
		})
}

type teleportClientsRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string `kong:"arg,help='client ID or name'"`
}

// Run deletes a Teleport client.
func (c *teleportClientsRmCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "delete this client on "+c.Host+"?", teleportClientReader(c.Client),
		func(cs *changeSet, t teleport.Client) writeFunc {
			cs.remove("client "+t.ID, fmt.Sprintf("%q (%s, %s)", t.Name, orDash(t.Platform), orDash(t.MAC.String())))
			return func(ctx context.Context, cl *amplifi.Client) error { return teleport.DeleteClient(ctx, cl, t.ID) }
		})
}

// teleportClientReader returns a reader that fetches the Teleport clients
// and finds ref, an ID or name, among them.
func teleportClientReader(ref string) func(context.Context, *amplifi.Client) (teleport.Client, error) {
	return func(ctx context.Context, cl *amplifi.Client) (teleport.Client, error) {
		clients, err := teleport.Clients(ctx, cl)
		if err != nil {
			return teleport.Client{}, err
		}
		return findOne(clients, ref, "client",
			func(t teleport.Client) string { return t.ID },
			func(t teleport.Client) []string { return []string{t.Name} })
	}
}
