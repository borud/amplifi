package cli

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/network"
)

type portfwdCmd struct {
	List    portfwdListCmd    `kong:"cmd,help='list port forwarding rules'"`
	Add     portfwdAddCmd     `kong:"cmd,help='add a port forwarding rule'"`
	Set     portfwdSetCmd     `kong:"cmd,help='change a port forwarding rule'"`
	Enable  portfwdEnableCmd  `kong:"cmd,help='enable a port forwarding rule'"`
	Disable portfwdDisableCmd `kong:"cmd,help='disable a port forwarding rule'"`
	Rm      portfwdRmCmd      `kong:"cmd,help='delete port forwarding rules'"`
}

type portfwdListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the rules.
func (c *portfwdListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		rules, err := network.PortForwards(ctx, cl)
		if err != nil {
			return err
		}

		if opt.JSON() {
			return printJSON(rules)
		}
		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "ID\tENABLED\tNAME\tTO\tPORTS")
		for _, r := range rules {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", r.ID, onOff(r.Enabled), r.Name, r.LANIP, portfwdPorts(r.Mappings))
		}
		return tw.Flush()
	})
}

// portfwdPorts formats mappings as "tcp 8080->80, udp 5000-5009".
func portfwdPorts(mappings []network.PortMapping) string {
	parts := make([]string, 0, len(mappings))
	for _, m := range mappings {
		wan := portfwdRange(m.WANPort, m.WANCount)
		lan := portfwdRange(cmp.Or(m.LANPort, m.WANPort), cmp.Or(m.LANCount, m.WANCount))
		if lan == wan {
			parts = append(parts, m.Protocol+" "+wan)
			continue
		}
		parts = append(parts, m.Protocol+" "+wan+"->"+lan)
	}
	return strings.Join(parts, ", ")
}

func portfwdRange(port, count int) string {
	if count <= 1 {
		return strconv.Itoa(port)
	}
	return fmt.Sprintf("%d-%d", port, port+count-1)
}

// portfwdParseRange parses PORT or PORT-PORT into a first port and count.
func portfwdParseRange(s string) (port, count int, err error) {
	first, last, isRange := strings.Cut(s, "-")
	port, err = strconv.Atoi(first)
	if err != nil || port < 1 || port > 65535 {
		return 0, 0, fmt.Errorf("invalid port %q", s)
	}
	if !isRange {
		return port, 1, nil
	}

	end, err := strconv.Atoi(last)
	if err != nil || end < port || end > 65535 {
		return 0, 0, fmt.Errorf("invalid port range %q", s)
	}
	return port, end - port + 1, nil
}

// portfwdMappings builds the mappings for proto (tcp, udp or both) from the
// --ext and --int flag values. An empty internal range means the same as the
// external one.
func portfwdMappings(proto, ext, internal string) ([]network.PortMapping, error) {
	wan, wanCount, err := portfwdParseRange(ext)
	if err != nil {
		return nil, err
	}
	lan, lanCount := wan, wanCount
	if internal != "" {
		lan, lanCount, err = portfwdParseRange(internal)
		if err != nil {
			return nil, err
		}
	}
	if lanCount == 1 {
		lanCount = wanCount
	}
	if lanCount != wanCount {
		return nil, fmt.Errorf("internal range %q is not the same size as external range %q", internal, ext)
	}

	protos := []string{proto}
	switch proto {
	case "tcp", "udp":
	case "both":
		protos = []string{"tcp", "udp"}
	default:
		return nil, fmt.Errorf("--proto must be tcp, udp or both, not %q", proto)
	}

	out := make([]network.PortMapping, 0, len(protos))
	for i, p := range protos {
		out = append(out, network.PortMapping{ID: i + 1, Protocol: p, WANPort: wan, LANPort: lan, WANCount: wanCount, LANCount: lanCount})
	}
	return out, nil
}

// portfwdProto describes the protocols of existing mappings the way --proto
// names them.
func portfwdProto(mappings []network.PortMapping) string {
	tcp := slices.ContainsFunc(mappings, func(m network.PortMapping) bool { return m.Protocol == "tcp" })
	udp := slices.ContainsFunc(mappings, func(m network.PortMapping) bool { return m.Protocol == "udp" })
	switch {
	case tcp && udp:
		return "both"
	case udp:
		return "udp"
	default:
		return "tcp"
	}
}

type portfwdAddCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Name      string `kong:"required,help='rule name'"`
	To        string `kong:"required,help='LAN IP address to forward to'"`
	Proto     string `kong:"default='tcp',enum='tcp,udp,both',help='protocol: tcp, udp or both'"`
	Ext       string `kong:"required,help='external port or range, e.g. 8080 or 5000-5010'"`
	Int       string `kong:"help='internal port or range (default: same as --ext)'"`
	Disabled  bool   `kong:"help='add the rule disabled'"`
}

// Run adds a rule.
func (c *portfwdAddCmd) Run(opt *Options) error {
	mappings, err := portfwdMappings(c.Proto, c.Ext, c.Int)
	if err != nil {
		return err
	}
	r := network.PortForward{Name: c.Name, Enabled: !c.Disabled, LANIP: c.To, Mappings: mappings}

	var cs changeSet
	cs.create("name", r.Name)
	cs.create("to", r.LANIP)
	cs.create("ports", portfwdPorts(r.Mappings))
	cs.create("enabled", onOff(r.Enabled))
	ok, err := cs.approve(c.yesFlag, fmt.Sprintf("add this port forwarding rule on %s?", c.Host))
	if err != nil || !ok {
		return err
	}

	var id int
	err = c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) (err error) {
		id, err = network.AddPortForward(ctx, cl, r)
		return err
	})
	if err != nil {
		return err
	}
	fmt.Printf("done: rule %d\n", id)
	return nil
}

type portfwdSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	ID        int     `kong:"arg,name='id',help='rule ID, as listed by portfwd list'"`
	Name      *string `kong:"help='rule name'"`
	To        *string `kong:"help='LAN IP address to forward to'"`
	Proto     *string `kong:"help='protocol: tcp, udp or both'"`
	Ext       *string `kong:"help='external port or range'"`
	Int       *string `kong:"help='internal port or range'"`
}

// Run changes a rule.
func (c *portfwdSetCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, portfwdPrompt(c.ID, c.Host), portfwdReader(c.ID),
		func(cs *changeSet, cur network.PortForward) writeFunc {
			return portfwdWrite(cur.ID, c.update(cs, cur))
		})
}

// update maps the flags to a PortForwardUpdate and records them against
// cur.
func (c *portfwdSetCmd) update(cs *changeSet, cur network.PortForward) network.PortForwardUpdate {
	u := network.PortForwardUpdate{
		Name:  cs.text("name", c.Name, cur.Name),
		LANIP: cs.text("to", c.To, cur.LANIP),
	}
	if c.Proto == nil && c.Ext == nil && c.Int == nil {
		return u
	}

	mappings, err := c.mappings(cur)
	if err != nil {
		cs.fail("ports", err)
		return u
	}
	u.Mappings = mappings
	cs.add("ports", portfwdPorts(cur.Mappings), portfwdPorts(mappings))
	return u
}

// mappings rebuilds the rule's mappings, taking the flags not given from
// the first current mapping.
func (c *portfwdSetCmd) mappings(cur network.PortForward) ([]network.PortMapping, error) {
	proto, ext, internal := portfwdProto(cur.Mappings), "", ""
	if len(cur.Mappings) > 0 {
		m := cur.Mappings[0]
		ext = portfwdRange(m.WANPort, m.WANCount)
		internal = portfwdRange(cmp.Or(m.LANPort, m.WANPort), cmp.Or(m.LANCount, m.WANCount))
	}
	if c.Proto != nil {
		proto = *c.Proto
	}
	if c.Ext != nil {
		ext, internal = *c.Ext, ""
	}
	if c.Int != nil {
		internal = *c.Int
	}
	return portfwdMappings(proto, ext, internal)
}

type portfwdEnableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	ID        int `kong:"arg,name='id',help='rule ID, as listed by portfwd list'"`
}

// Run enables a rule.
func (c *portfwdEnableCmd) Run(opt *Options) error {
	return portfwdSetEnabled(opt, &c.unitFlags, c.yesFlag, c.ID, true)
}

type portfwdDisableCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	ID        int `kong:"arg,name='id',help='rule ID, as listed by portfwd list'"`
}

// Run disables a rule.
func (c *portfwdDisableCmd) Run(opt *Options) error {
	return portfwdSetEnabled(opt, &c.unitFlags, c.yesFlag, c.ID, false)
}

func portfwdSetEnabled(opt *Options, u *unitFlags, y yesFlag, id int, enabled bool) error {
	flag := onOffFlag(enabled)
	return apply(opt, u, y, portfwdPrompt(id, u.Host), portfwdReader(id),
		func(cs *changeSet, cur network.PortForward) writeFunc {
			return portfwdWrite(cur.ID, network.PortForwardUpdate{Enabled: cs.onOff("enabled", &flag, cur.Enabled)})
		})
}

func portfwdPrompt(id int, host string) string {
	return fmt.Sprintf("change port forwarding rule %d on %s?", id, host)
}

// portfwdReader returns a reader that fetches rule id.
func portfwdReader(id int) func(context.Context, *amplifi.Client) (network.PortForward, error) {
	return func(ctx context.Context, cl *amplifi.Client) (network.PortForward, error) {
		rules, err := network.PortForwards(ctx, cl)
		if err != nil {
			return network.PortForward{}, err
		}
		return findOne(rules, strconv.Itoa(id), "port forwarding rule", func(r network.PortForward) string { return strconv.Itoa(r.ID) }, nil)
	}
}

func portfwdWrite(id int, u network.PortForwardUpdate) writeFunc {
	return func(ctx context.Context, cl *amplifi.Client) error { return network.UpdatePortForward(ctx, cl, id, u) }
}

type portfwdRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	IDs       []int `kong:"arg,name='id',help='rule IDs'"`
}

// Run deletes rules.
func (c *portfwdRmCmd) Run(opt *Options) error {
	ids := make([]string, 0, len(c.IDs))
	for _, id := range c.IDs {
		ids = append(ids, strconv.Itoa(id))
	}
	prompt := fmt.Sprintf("delete port forwarding rule %s on %s?", strings.Join(ids, ", "), c.Host)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return network.DeletePortForwards(ctx, cl, c.IDs...)
	})
}
