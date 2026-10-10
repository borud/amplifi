package cli

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/device"
	"github.com/borud/amplifi/firmware"
)

type fwCmd struct {
	Check     fwCheckCmd     `kong:"cmd,help='check for new firmware for the mesh'"`
	Available fwAvailableCmd `kong:"cmd,help='list the firmware each unit runs and any upgrade found'"`
	Status    fwStatusCmd    `kong:"cmd,help='show the progress of a firmware check or upgrade'"`
	Upgrade   fwUpgradeCmd   `kong:"cmd,help='upgrade the firmware of the whole mesh or only this unit'"`
	Config    fwConfigCmd    `kong:"cmd,help='automatic update settings'"`
}

// fwPollInterval is how often --wait polls the update status.
const fwPollInterval = 2 * time.Second

type fwCheckCmd struct {
	unitFlags   `kong:"embed"`
	Wait        bool          `kong:"help='wait for the check to finish and list the result'"`
	WaitTimeout time.Duration `kong:"help='maximum time to wait',default='3m'"`
}

// Run starts an update check.
func (c *fwCheckCmd) Run(opt *Options) error {
	return c.session(opt, c.WaitTimeout, func(ctx context.Context, cl *amplifi.Client) error {
		if err := firmware.StartCheck(ctx, cl); err != nil {
			return err
		}
		if !c.Wait {
			fmt.Fprintln(os.Stderr, "update check started; see 'amplifi firmware status' and 'amplifi firmware available'")
			return nil
		}

		if _, err := fwWait(ctx, cl, !opt.JSON()); err != nil {
			return err
		}
		v, err := firmware.GetVersions(ctx, cl)
		if err != nil {
			return err
		}
		return fwPrintVersions(opt, v)
	})
}

type fwAvailableCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the available upgrades.
func (c *fwAvailableCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		v, err := firmware.GetVersions(ctx, cl)
		if err != nil {
			return err
		}
		return fwPrintVersions(opt, v)
	})
}

func fwPrintVersions(opt *Options, v *firmware.Versions) error {
	if opt.JSON() {
		return printJSON(v)
	}

	fmt.Printf("status: %s\n\n", fwWord(string(v.State)))
	tw := newTable(os.Stdout)
	fmt.Fprintln(tw, "MAC\tROLE\tCURRENT\tUPGRADE\tLAST CHECK")
	for _, u := range v.Units {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.MAC, u.Role, u.Current.Version, fwUpgradeText(u), timeText(u.LastCheck))
	}
	return tw.Flush()
}

// fwUpgradeText describes the upgrade available to a unit, if any.
func fwUpgradeText(n firmware.UnitVersion) string {
	if !n.HasUpgrade() {
		return "-"
	}
	if n.Upgrade.Status == "" {
		return n.Upgrade.Version
	}
	return n.Upgrade.Version + " (" + fwWord(n.Upgrade.Status) + ")"
}

// fwWord turns the router's snake_case status strings into words.
func fwWord(s string) string {
	if s == "" {
		return "-"
	}
	return strings.ReplaceAll(strings.ToLower(s), "_", " ")
}

type fwStatusCmd struct {
	unitFlags   `kong:"embed"`
	Wait        bool          `kong:"help='poll until no check or upgrade is in progress'"`
	WaitTimeout time.Duration `kong:"help='maximum time to wait',default='30m'"`
}

// Run shows the update status, optionally waiting for it to settle.
func (c *fwStatusCmd) Run(opt *Options) error {
	return c.session(opt, c.WaitTimeout, func(ctx context.Context, cl *amplifi.Client) error {
		get := firmware.GetProgress
		if c.Wait {
			get = func(ctx context.Context, cl *amplifi.Client) (*firmware.Progress, error) {
				return fwWait(ctx, cl, !opt.JSON())
			}
		}
		p, err := get(ctx, cl)
		if err != nil {
			return err
		}
		return fwPrintProgress(opt, p)
	})
}

// fwWait polls the update status until nothing is in progress, printing
// each change of status to stderr when progress is set.
func fwWait(ctx context.Context, cl *amplifi.Client, progress bool) (*firmware.Progress, error) {
	var last firmware.State
	for {
		p, err := firmware.GetProgress(ctx, cl)
		if err != nil {
			return nil, err
		}
		if progress && p.State != last {
			fmt.Fprintf(os.Stderr, "%s  %s\n", time.Now().Format(time.TimeOnly), fwWord(string(p.State)))
			last = p.State
		}
		if !p.State.Busy() {
			return p, nil
		}

		if err := sleepCtx(ctx, fwPollInterval); err != nil {
			return nil, fmt.Errorf("gave up waiting: %w", err)
		}
	}
}

func fwPrintProgress(opt *Options, p *firmware.Progress) error {
	if opt.JSON() {
		return printJSON(p)
	}

	fmt.Printf("status: %s\n", fwWord(string(p.State)))
	if len(p.Units) == 0 {
		return nil
	}

	fmt.Println()
	tw := newTable(os.Stdout)
	fmt.Fprintln(tw, "MAC\tROLE\tCURRENT\tUPGRADE\tCHECK")
	for _, u := range p.Units {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", u.MAC, u.Role, u.Current.Version, fwUpgradeText(u.UnitVersion), fwCheckText(u.Check))
	}
	return tw.Flush()
}

func fwCheckText(r firmware.CheckResult) string {
	if r.Status != "" {
		return fwWord(r.Status)
	}
	return fmt.Sprintf("code %d", r.Code)
}

type fwUpgradeCmd struct {
	unitFlags   `kong:"embed"`
	yesFlag     `kong:"embed"`
	UnitOnly    bool          `kong:"help='upgrade only the unit connected to, not the whole mesh'"`
	Wait        bool          `kong:"help='follow the upgrade until the units go offline to reboot'"`
	WaitTimeout time.Duration `kong:"help='maximum time to wait',default='30m'"`
}

// Run upgrades the mesh, or one unit, after showing what will change. It
// can follow the upgrade afterwards, so it does not use apply.
func (c *fwUpgradeCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		planned, err := fwPlan(ctx, cl, c.UnitOnly)
		if err != nil {
			return err
		}
		if len(planned) == 0 {
			fmt.Println("no firmware upgrade available; run 'amplifi firmware check' first")
			return nil
		}
		fmt.Println("the following units will be upgraded and will reboot:")
		for _, line := range planned {
			fmt.Println("  " + line)
		}
		if !c.approve("start the firmware upgrade now?") {
			return nil
		}

		// A fresh timeout for the write, as in apply.
		wctx, cancel := c.context()
		defer cancel()

		if c.UnitOnly {
			if err := device.Upgrade(wctx, cl); err != nil {
				return err
			}
			fmt.Printf("upgrade requested on %s; the unit will flash and reboot\n", c.Host)
			return nil
		}

		if err := firmware.StartUpgrade(wctx, cl); err != nil {
			return err
		}
		fmt.Println("upgrade started")
		if !c.Wait {
			return nil
		}

		fctx, cancelFollow := context.WithTimeout(context.Background(), c.WaitTimeout)
		defer cancelFollow()
		return fwFollow(fctx, cl)
	})
}

// fwPlan lists the upgrades that will be applied, one line per unit.
func fwPlan(ctx context.Context, cl *amplifi.Client, unitOnly bool) ([]string, error) {
	if unitOnly {
		fw, err := device.GetFirmware(ctx, cl)
		if err != nil {
			return nil, err
		}
		if fw.UpgradeVersion == "" || fw.UpgradeVersion == fw.Version {
			return []string{}, nil
		}
		return []string{fmt.Sprintf("this unit: %s -> %s", fw.Version, fw.UpgradeVersion)}, nil
	}

	v, err := firmware.GetVersions(ctx, cl)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(v.Units))
	for _, u := range v.Units {
		if !u.HasUpgrade() {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s): %s -> %s", u.MAC, u.Role, u.Current.Version, u.Upgrade.Version))
	}
	return lines, nil
}

// fwFollow polls the update status after an upgrade has started, until the
// upgrade settles or the router goes away to reboot.
func fwFollow(ctx context.Context, cl *amplifi.Client) error {
	p, err := fwWait(ctx, cl, true)
	switch {
	case err == nil:
		fmt.Printf("upgrade %s\n", fwWord(string(p.State)))
		return nil
	case amplifi.IsRouterError(err), ctx.Err() != nil:
		return err
	default:
		fmt.Println("connection lost; the units are flashing and rebooting")
		return nil
	}
}

type fwConfigCmd struct {
	Show fwConfigShowCmd `kong:"cmd,help='show the automatic update settings'"`
	Set  fwConfigSetCmd  `kong:"cmd,help='change automatic update settings given as KEY=VALUE, with the keys of show'"`
}

type fwConfigShowCmd struct {
	unitFlags `kong:"embed"`
}

// Run prints the update settings. Their shape is not known, so both output
// formats print them as JSON.
func (c *fwConfigShowCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		cfg, err := firmware.GetConfig(ctx, cl)
		if err != nil {
			return err
		}
		return printJSON(cfg)
	})
}

type fwConfigSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Values    []string `kong:"arg,name='key=value',help='settings to change; values are JSON (true, 3, \"x\") or plain strings'"`
}

// Run does a read-modify-write of the update settings.
func (c *fwConfigSetCmd) Run(opt *Options) error {
	changes, err := fwParseAssignments(c.Values)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, "change the automatic update settings on "+c.Host+"?", firmware.GetConfig,
		func(cs *changeSet, cfg map[string]any) writeFunc {
			for _, k := range slices.Sorted(maps.Keys(changes)) {
				from := "(unset)"
				if old, ok := cfg[k]; ok {
					from = inlineValue(old)
				}
				cs.add(k, from, inlineValue(changes[k]))
			}
			maps.Copy(cfg, changes)
			return func(ctx context.Context, cl *amplifi.Client) error { return firmware.SetConfig(ctx, cl, cfg) }
		})
}

// fwParseAssignments parses KEY=VALUE arguments, taking each value as JSON
// when it parses as JSON and as a string otherwise.
func fwParseAssignments(args []string) (map[string]any, error) {
	out := make(map[string]any, len(args))
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("expected KEY=VALUE, got %q", a)
		}
		out[k] = parseValue(v)
	}
	return out, nil
}
