package cli

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/clients"
)

type clientsCmd struct {
	List           clientsListCmd        `kong:"cmd,help='list clients (wired and Wi-Fi) known to the mesh'"`
	Show           clientsShowCmd        `kong:"cmd,help='show one client in detail'"`
	History        clientsHistoryCmd     `kong:"cmd,help='list clients the mesh has seen before'"`
	Signal         clientsSignalCmd      `kong:"cmd,help='show the signal quality of each Wi-Fi client'"`
	Count          clientsCountCmd       `kong:"cmd,help='count Wi-Fi clients by band and network'"`
	Rename         clientsRenameCmd      `kong:"cmd,help='change the custom name of a client'"`
	Pause          clientsPauseCmd       `kong:"cmd,help='block internet access for a client'"`
	Resume         clientsResumeCmd      `kong:"cmd,help='restore internet access for a paused client'"`
	Fingerprint    clientsFingerprintCmd `kong:"cmd,help='show how the router identifies client devices'"`
	GuestWhitelist clientsWhitelistCmd   `kong:"cmd,name='guest-whitelist',help='show the guest Wi-Fi whitelist'"`
}

type clientsListCmd struct {
	unitFlags `kong:"embed"`
	Online    bool   `kong:"help='only clients that are online'"`
	Wired     bool   `kong:"help='only wired clients',xor='medium'"`
	Wifi      bool   `kong:"help='only Wi-Fi clients',xor='medium'"`
	Node      string `kong:"help='only clients connected to this unit (name or MAC address)'"`
}

// Run lists the clients, filtered by the flags.
func (c *clientsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		all, err := clients.List(ctx, cl)
		if err != nil {
			return err
		}
		list := slices.DeleteFunc(all, func(n clients.Client) bool { return !c.keep(n) })

		if opt.JSON() {
			return printJSON(list)
		}
		return clientsPrintTable(os.Stdout, list)
	})
}

func (c *clientsListCmd) keep(n clients.Client) bool {
	if c.Online && !n.Online {
		return false
	}
	if c.Wired && n.Connection != "wired" {
		return false
	}
	if c.Wifi && (n.Connection == "wired" || n.Connection == "") {
		return false
	}
	if c.Node == "" {
		return true
	}
	return strings.EqualFold(c.Node, n.Node.String()) || strings.EqualFold(c.Node, n.NodeName)
}

type clientsShowCmd struct {
	unitFlags `kong:"embed"`
	Client    string `kong:"arg,help='client MAC address or name'"`
}

// Run prints everything known about one client.
func (c *clientsShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		all, err := clients.List(ctx, cl)
		if err != nil {
			return err
		}
		n, err := clientsFind(all, c.Client)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(n)
		}
		return clientsPrintDetail(os.Stdout, n)
	})
}

type clientsHistoryCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the clients the mesh remembers.
func (c *clientsHistoryCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		list, err := clients.History(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(list)
		}
		return clientsPrintHistory(os.Stdout, list, time.Now())
	})
}

type clientsSignalCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the signal quality of each Wi-Fi client.
func (c *clientsSignalCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		signals, err := clients.Signals(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(signals)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "MAC\tBAND\tNETWORK\tNODE\tQUALITY")
		for _, s := range signals {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\n", s.MAC, s.Band, s.Network, orDash(clientsNode(s.NodeName, s.Node)), s.Quality)
		}
		return tw.Flush()
	})
}

type clientsCountCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the Wi-Fi client count of the unit.
func (c *clientsCountCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		wc, err := clients.Count(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(wc)
		}
		fmt.Printf("%d Wi-Fi clients (%s; %s)\n", wc.Total, counts(wc.ByBand), counts(wc.ByNetwork))
		return nil
	})
}

type clientsRenameCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string `kong:"arg,help='client MAC address or name'"`
	Name      string `kong:"arg,help='new name'"`
}

// Run sets a client's custom name.
func (c *clientsRenameCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("rename client %s on %s?", c.Client, c.Host), clientsReader(c.Client),
		func(cs *changeSet, n clients.Client) writeFunc {
			cs.text("name", &c.Name, n.Description)
			return func(ctx context.Context, cl *amplifi.Client) error { return clients.Rename(ctx, cl, n.MAC, c.Name) }
		})
}

type clientsPauseCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string `kong:"arg,help='client MAC address or name'"`
}

// Run blocks a client's internet access.
func (c *clientsPauseCmd) Run(opt *Options) error {
	return clientsSetPaused(opt, &c.unitFlags, c.yesFlag, c.Client, true)
}

type clientsResumeCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Client    string `kong:"arg,help='client MAC address or name'"`
}

// Run restores a client's internet access.
func (c *clientsResumeCmd) Run(opt *Options) error {
	return clientsSetPaused(opt, &c.unitFlags, c.yesFlag, c.Client, false)
}

// clientsSetPaused pauses or resumes a client. The router does not report
// whether a client is paused, so the change shows the old state as "?".
func clientsSetPaused(opt *Options, u *unitFlags, y yesFlag, ref string, paused bool) error {
	return apply(opt, u, y, fmt.Sprintf("change client %s on %s?", ref, u.Host), clientsReader(ref),
		func(cs *changeSet, n clients.Client) writeFunc {
			cs.add(clientsLabel(n)+" paused", "?", onOff(paused))
			return func(ctx context.Context, cl *amplifi.Client) error { return clients.Pause(ctx, cl, n.MAC, paused) }
		})
}

type clientsFingerprintCmd struct {
	unitFlags `kong:"embed"`
	Client    string `kong:"arg,optional,help='only this client MAC address'"`
}

// Run prints the router's device identification of each client.
func (c *clientsFingerprintCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		fps, err := clients.Fingerprints(ctx, cl)
		if amplifi.IsRouterError(err) {
			return fmt.Errorf("%w (not supported by this firmware; 'clients show' lists the icon id of each client, which is its fingerprint device id)", err)
		}
		if err != nil {
			return err
		}
		if c.Client != "" {
			fps = clientsFilterKey(fps, c.Client)
			if len(fps) == 0 {
				return fmt.Errorf("no fingerprint for %q", c.Client)
			}
		}

		if opt.JSON() {
			return printJSON(fps)
		}
		for _, mac := range slices.Sorted(maps.Keys(fps)) {
			fmt.Printf("%s\t%s\n", mac, inlineValue(fps[mac]))
		}
		return nil
	})
}

type clientsWhitelistCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the guest Wi-Fi whitelist.
func (c *clientsWhitelistCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		list, err := clients.GuestWhitelist(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(list)
		}
		if len(list) == 0 {
			fmt.Println("guest whitelist is empty")
			return nil
		}
		for _, e := range list {
			fmt.Println(inlineValue(e))
		}
		return nil
	})
}

// clientsReader returns a reader that finds a client by MAC address or
// name. A MAC address that is not in the client list is still accepted, so
// offline clients can be addressed.
func clientsReader(ref string) func(context.Context, *amplifi.Client) (clients.Client, error) {
	return func(ctx context.Context, cl *amplifi.Client) (clients.Client, error) {
		all, err := clients.List(ctx, cl)
		if err != nil {
			return clients.Client{}, err
		}
		n, err := clientsFind(all, ref)
		if err == nil {
			return n, nil
		}
		if mac, perr := amplifi.ParseMAC(ref); perr == nil {
			return clients.Client{MAC: mac}, nil
		}
		return clients.Client{}, err
	}
}

// clientsFind picks the client whose MAC address matches ref, or whose
// name, custom name or host name does (ignoring case). A name must match
// exactly one client.
func clientsFind(all []clients.Client, ref string) (clients.Client, error) {
	if mac, err := amplifi.ParseMAC(ref); err == nil {
		ref = mac.String()
	}
	return findOne(all, ref, "client",
		func(n clients.Client) string { return n.MAC.String() },
		func(n clients.Client) []string { return []string{n.Name, n.Description, n.HostName} })
}

func clientsPrintTable(w io.Writer, list []clients.Client) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tMAC\tIP\tCONNECTION\tNODE\tSIGNAL\tSTATUS")
	for _, n := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			orDash(n.Name), n.MAC, orDash(clientsIP(n.IP)), orDash(clientsConnection(n)),
			orDash(clientsNode(n.NodeName, n.Node)), clientsSignal(n), clientsStatus(n))
	}
	return tw.Flush()
}

func clientsPrintHistory(w io.Writer, list []clients.Client, now time.Time) error {
	tw := newTable(w)
	fmt.Fprintln(tw, "NAME\tMAC\tIP\tCONNECTION\tLAST SEEN\tTIMES SEEN\tSTATUS")
	for _, n := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			n.Name, n.MAC, orDash(clientsIP(n.IP)), orDash(clientsConnection(n)),
			clientsAgo(n.LastSeen, now), n.TimesSeen, clientsStatus(n))
	}
	return tw.Flush()
}

// clientsAgo describes t relative to now: minutes or hours ago within a day,
// the date before that.
func clientsAgo(t, now time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Local().Format(time.DateOnly)
}

func clientsPrintDetail(w io.Writer, n clients.Client) error {
	tw := newTable(w)
	row := func(k, v string) {
		if v != "" {
			fmt.Fprintf(tw, "%s\t%s\n", k, v)
		}
	}

	row("name", n.Name)
	row("host name", n.HostName)
	row("description", n.Description)
	if n.IconID != 0 {
		row("icon id", fmt.Sprintf("%d (fingerprint device id)", n.IconID))
	}
	row("mac", n.MAC.String())
	row("ip", clientsIP(n.IP))
	row("connection", clientsConnection(n))
	row("network", n.Network)
	row("node", clientsNode(n.NodeName, n.Node))
	row("status", clientsStatus(n))
	if !n.LastSeen.IsZero() {
		row("last seen", fmt.Sprintf("%s (%s)", n.LastSeen.Local().Format(time.DateTime), clientsAgo(n.LastSeen, time.Now())))
		row("times seen", fmt.Sprint(n.TimesSeen))
	}
	if wf := n.Wifi; wf != nil {
		row("mode", strings.TrimSpace(wf.Mode+" "+clientsRadioMode(wf)))
		row("signal", fmt.Sprint(wf.Signal))
		row("rate", fmt.Sprintf("rx %d Mbit/s  tx %d Mbit/s", wf.RxBitrate, wf.TxBitrate))
		row("mcs", fmt.Sprintf("rx %d  tx %d", wf.RxMCS, wf.TxMCS))
		row("channel width", fmt.Sprintf("rx %d MHz  tx %d MHz", wf.RxMHz, wf.TxMHz))
		row("streams", fmt.Sprint(wf.SpatialStreams))
		row("traffic", fmt.Sprintf("rx %d bytes  tx %d bytes", wf.RxBytes, wf.TxBytes))
		row("inactive", fmt.Sprint(wf.Inactive))
	}
	for _, k := range slices.Sorted(maps.Keys(n.Fields)) {
		row("field "+k, inlineValue(n.Fields[k]))
	}
	for _, k := range slices.Sorted(maps.Keys(n.Custom)) {
		row("custom "+k, inlineValue(n.Custom[k]))
	}
	return tw.Flush()
}

func clientsRadioMode(wf *clients.WifiClient) string {
	if wf.RadioMode == "" || wf.RadioMode == wf.Mode {
		return ""
	}
	return "(radio " + wf.RadioMode + ")"
}

// clientsNode names the unit a client is connected to.
func clientsNode(name string, mac amplifi.MAC) string {
	return cmp.Or(name, mac.String())
}

func clientsSignal(n clients.Client) string {
	if n.Wifi == nil {
		return "-"
	}
	return fmt.Sprint(n.Wifi.Signal)
}

func clientsStatus(n clients.Client) string {
	if n.Online {
		return "online"
	}
	return "offline"
}

func clientsConnection(n clients.Client) string {
	if n.Port == nil {
		return n.Connection
	}
	return fmt.Sprintf("%s (port %d)", n.Connection, *n.Port)
}

func clientsLabel(n clients.Client) string {
	if n.Name == "" || n.Name == n.MAC.String() {
		return n.MAC.String()
	}
	return fmt.Sprintf("%s (%s)", n.Name, n.MAC)
}

func clientsIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

// clientsFilterKey keeps the entry of m whose MAC address key matches mac.
func clientsFilterKey(m map[string]map[string]any, mac string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for k, v := range m {
		if strings.EqualFold(k, mac) {
			out[k] = v
		}
	}
	return out
}
