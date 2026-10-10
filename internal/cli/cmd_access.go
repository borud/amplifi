package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/access"
)

type accessCmd struct {
	Profiles accessProfilesCmd `kong:"cmd,help='access control profiles (groups of clients)'"`
	Devices  accessDevicesCmd  `kong:"cmd,help='access control device table'"`
	Rules    accessRulesCmd    `kong:"cmd,help='blocking schedules of profiles'"`
}

type accessProfilesCmd struct {
	List   accessProfilesListCmd   `kong:"cmd,help='list profiles'"`
	Add    accessProfilesAddCmd    `kong:"cmd,help='add a profile'"`
	Set    accessProfilesSetCmd    `kong:"cmd,help='change the name or clients of a profile'"`
	Rm     accessProfilesRmCmd     `kong:"cmd,help='delete profiles'"`
	Pause  accessProfilesPauseCmd  `kong:"cmd,help='block internet access for the clients of a profile'"`
	Resume accessProfilesResumeCmd `kong:"cmd,help='restore internet access for the clients of a profile'"`
}

type accessDevicesCmd struct {
	List accessDevicesListCmd `kong:"cmd,help='list device entries'"`
	Add  accessDevicesAddCmd  `kong:"cmd,help='add a device entry'"`
	Set  accessDevicesSetCmd  `kong:"cmd,help='change a device entry'"`
	Rm   accessDevicesRmCmd   `kong:"cmd,help='delete device entries'"`
}

type accessRulesCmd struct {
	List accessRulesListCmd `kong:"cmd,help='list rules'"`
	Add  accessRulesAddCmd  `kong:"cmd,help='add a blocking schedule to a profile'"`
	Set  accessRulesSetCmd  `kong:"cmd,help='change a rule'"`
	Rm   accessRulesRmCmd   `kong:"cmd,help='delete rules'"`
}

type accessProfilesListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the profiles.
func (c *accessProfilesListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		profiles, err := access.Profiles(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(profiles)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "ID\tNAME\tENABLED\tPAUSED\tDEVICES")
		for _, p := range profiles {
			fmt.Fprintf(w, "%s\t%s\t%t\t%t\t%s\n", p.ID, p.Name, p.Enabled, p.Paused, joinMACs(p.Devices))
		}
		return w.Flush()
	})
}

type accessProfilesAddCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Name      string   `kong:"arg,help='profile name'"`
	Device    []string `kong:"help='MAC address of a client to put in the profile (repeatable)'"`
	Paused    bool     `kong:"help='add the profile paused'"`
}

// Run adds a profile.
func (c *accessProfilesAddCmd) Run(opt *Options) error {
	devices, err := parseMACs(c.Device)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("add profile %q on %s?", c.Name, c.Host), access.Profiles,
		func(cs *changeSet, profiles []access.Profile) writeFunc {
			p := access.Profile{
				ID:      accessNextProfileID(profiles),
				Name:    c.Name,
				Enabled: true,
				Paused:  c.Paused,
				Devices: devices,
			}
			cs.create("id", p.ID)
			cs.create("name", p.Name)
			cs.create("paused", onOff(p.Paused))
			cs.create("devices", joinMACs(p.Devices))
			return func(ctx context.Context, cl *amplifi.Client) error { return access.AddProfile(ctx, cl, p) }
		})
}

type accessProfilesSetCmd struct {
	unitFlags    `kong:"embed"`
	yesFlag      `kong:"embed"`
	Profile      string   `kong:"arg,help='profile ID or name'"`
	Name         *string  `kong:"help='new name'"`
	AddDevice    []string `kong:"help='MAC address of a client to add (repeatable)'"`
	RemoveDevice []string `kong:"help='MAC address of a client to remove (repeatable)'"`
}

// Run changes a profile.
func (c *accessProfilesSetCmd) Run(opt *Options) error {
	add, err := parseMACs(c.AddDevice)
	if err != nil {
		return err
	}
	remove, err := parseMACs(c.RemoveDevice)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change profile %s on %s?", c.Profile, c.Host), accessProfileReader(c.Profile),
		func(cs *changeSet, p access.Profile) writeFunc {
			u := access.ProfileUpdate{Name: cs.text("name", c.Name, p.Name)}
			if len(add) > 0 || len(remove) > 0 {
				u.Devices = accessEditMACs(p.Devices, add, remove)
				cs.add("devices", joinMACs(p.Devices), joinMACs(u.Devices))
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return access.UpdateProfile(ctx, cl, p.ID, u) }
		})
}

type accessProfilesRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Profiles  []string `kong:"arg,help='profile IDs or names'"`
}

// Run deletes profiles.
func (c *accessProfilesRmCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "delete these profiles on "+c.Host+"?", access.Profiles,
		func(cs *changeSet, profiles []access.Profile) writeFunc {
			ids := make([]string, 0, len(c.Profiles))
			for _, ref := range c.Profiles {
				p, err := accessFindProfile(profiles, ref)
				if err != nil {
					cs.fail("profile", err)
					continue
				}
				cs.remove("profile "+p.ID, fmt.Sprintf("%q (devices %s)", p.Name, joinMACs(p.Devices)))
				ids = append(ids, p.ID)
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return access.DeleteProfiles(ctx, cl, ids...) }
		})
}

type accessProfilesPauseCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Profile   string `kong:"arg,help='profile ID or name'"`
}

// Run blocks internet access for the clients of a profile.
func (c *accessProfilesPauseCmd) Run(opt *Options) error {
	return accessSetPaused(opt, &c.unitFlags, c.yesFlag, c.Profile, true)
}

type accessProfilesResumeCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Profile   string `kong:"arg,help='profile ID or name'"`
}

// Run restores internet access for the clients of a profile.
func (c *accessProfilesResumeCmd) Run(opt *Options) error {
	return accessSetPaused(opt, &c.unitFlags, c.yesFlag, c.Profile, false)
}

func accessSetPaused(opt *Options, u *unitFlags, y yesFlag, ref string, paused bool) error {
	return apply(opt, u, y, fmt.Sprintf("change profile %s on %s?", ref, u.Host), accessProfileReader(ref),
		func(cs *changeSet, p access.Profile) writeFunc {
			cs.add("paused", onOff(p.Paused), onOff(paused))
			return func(ctx context.Context, cl *amplifi.Client) error { return access.PauseProfile(ctx, cl, p.ID, paused) }
		})
}

type accessDevicesListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the device entries.
func (c *accessDevicesListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		devices, err := access.Devices(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(devices)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "MAC\tNAME\tTAG\tDSCP")
		for _, d := range devices {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.MAC, d.Name, intOrDash(d.Tag), accessDSCPName(d.DSCP))
		}
		return w.Flush()
	})
}

type accessDevicesAddCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MAC       string `kong:"arg,name='mac',help='client MAC address'"`
	Name      string `kong:"help='device name'"`
	DSCP      string `kong:"name='dscp',help='traffic priority: none, streaming, gaming or a DSCP number'"`
	Tag       *int   `kong:"help='tag'"`
}

// Run adds a device entry.
func (c *accessDevicesAddCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	dscp, err := accessParseDSCP(c.DSCP)
	if err != nil {
		return err
	}
	d := access.Device{MAC: mac, Name: c.Name, Tag: c.Tag, DSCP: dscp}

	var cs changeSet
	cs.create("mac", d.MAC.String())
	cs.create("name", d.Name)
	cs.create("tag", intOrDash(d.Tag))
	cs.create("dscp", accessDSCPName(d.DSCP))
	ok, err := cs.approve(c.yesFlag, "add this device entry on "+c.Host+"?")
	if err != nil || !ok {
		return err
	}
	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error { return access.AddDevice(ctx, cl, d) })
}

type accessDevicesSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MAC       string  `kong:"arg,name='mac',help='client MAC address'"`
	Name      *string `kong:"help='device name'"`
	DSCP      string  `kong:"name='dscp',help='traffic priority: none, streaming, gaming or a DSCP number'"`
	Tag       *int    `kong:"help='tag'"`
}

// Run changes a device entry.
func (c *accessDevicesSetCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	dscp, err := accessParseDSCP(c.DSCP)
	if err != nil {
		return err
	}

	read := func(ctx context.Context, cl *amplifi.Client) (access.Device, error) {
		devices, err := access.Devices(ctx, cl)
		if err != nil {
			return access.Device{}, err
		}
		return findOne(devices, mac.String(), "device entry", func(d access.Device) string { return d.MAC.String() }, nil)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change device entry %s on %s?", mac, c.Host), read,
		func(cs *changeSet, d access.Device) writeFunc {
			cs.text("name", c.Name, d.Name)
			if c.Tag != nil {
				cs.add("tag", intOrDash(d.Tag), intOrDash(c.Tag))
			}
			if dscp != nil {
				cs.add("dscp", accessDSCPName(d.DSCP), accessDSCPName(dscp))
			}
			u := access.DeviceUpdate{Name: c.Name, Tag: c.Tag, DSCP: dscp}
			return func(ctx context.Context, cl *amplifi.Client) error { return access.UpdateDevice(ctx, cl, d.MAC, u) }
		})
}

type accessDevicesRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MACs      []string `kong:"arg,name='mac',help='client MAC addresses'"`
}

// Run deletes device entries.
func (c *accessDevicesRmCmd) Run(opt *Options) error {
	macs, err := parseMACs(c.MACs)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("delete the device entries for %s on %s?", joinMACs(macs), c.Host)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return access.DeleteDevices(ctx, cl, macs...)
	})
}

type accessRulesListCmd struct {
	unitFlags `kong:"embed"`
	Profile   string `kong:"help='only show rules of this profile (ID or name)'"`
}

// Run lists the rules.
func (c *accessRulesListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		rules, err := access.Rules(ctx, cl)
		if err != nil {
			return err
		}

		// Profile names make the table readable; without them it still works.
		profiles, err := access.Profiles(ctx, cl)
		if err != nil {
			opt.Logger().Warn("cannot list profiles", "err", err)
		}
		names := make(map[string]string, len(profiles))
		for _, p := range profiles {
			names[p.ID] = p.Name
		}

		if c.Profile != "" {
			p, err := accessFindProfile(profiles, c.Profile)
			if err != nil {
				return err
			}
			rules = slices.DeleteFunc(rules, func(r access.Rule) bool { return r.ProfileID() != p.ID })
		}

		if opt.JSON() {
			return printJSON(rules)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "ID\tPROFILE\tENABLED\tTIME\tDAYS\tDESCRIPTION")
		for _, r := range rules {
			fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\n", r.ID, names[r.ProfileID()], r.Enabled, accessWindow(r.StartTime, r.Duration), accessDaysString(r.WeekDays), r.Description)
		}
		return w.Flush()
	})
}

type accessRulesAddCmd struct {
	unitFlags   `kong:"embed"`
	yesFlag     `kong:"embed"`
	Profile     string `kong:"arg,help='profile ID or name'"`
	Start       string `kong:"required,help='start of the blocked period, HH:MM'"`
	End         string `kong:"xor='length',help='end of the blocked period, HH:MM'"`
	Duration    string `kong:"xor='length',help='length of the blocked period, HH:MM'"`
	Days        string `kong:"help='days the rule applies, e.g. mon-fri, sat,sun, weekdays, weekend or all',default='all'"`
	Description string `kong:"help='description'"`
	Disabled    bool   `kong:"help='add the rule disabled'"`
}

// Run adds a rule to a profile.
func (c *accessRulesAddCmd) Run(opt *Options) error {
	start, err := parseClock(c.Start)
	if err != nil {
		return err
	}
	duration, err := accessRuleLength(clockText(start), c.End, c.Duration)
	if err != nil {
		return err
	}
	if duration == "" {
		return errors.New("give --end or --duration")
	}
	days, err := accessParseDays(c.Days)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("add this rule to profile %s on %s?", c.Profile, c.Host), accessProfileReader(c.Profile),
		func(cs *changeSet, p access.Profile) writeFunc {
			r := access.Rule{
				ID:          access.NewRuleID(p.ID),
				Description: c.Description,
				Enabled:     !c.Disabled,
				StartTime:   clockText(start),
				Duration:    duration,
				WeekDays:    days,
			}
			cs.create("id", r.ID)
			cs.create("profile", fmt.Sprintf("%s %q", p.ID, p.Name))
			cs.create("time", accessWindow(r.StartTime, r.Duration))
			cs.create("days", accessDaysString(r.WeekDays))
			cs.create("enabled", onOff(r.Enabled))
			cs.create("description", r.Description)
			return func(ctx context.Context, cl *amplifi.Client) error { return access.AddRule(ctx, cl, r) }
		})
}

type accessRulesSetCmd struct {
	unitFlags   `kong:"embed"`
	yesFlag     `kong:"embed"`
	ID          string  `kong:"arg,name='id',help='rule ID'"`
	Start       string  `kong:"help='start of the blocked period, HH:MM'"`
	End         string  `kong:"xor='length',help='end of the blocked period, HH:MM'"`
	Duration    string  `kong:"xor='length',help='length of the blocked period, HH:MM'"`
	Days        string  `kong:"help='days the rule applies, e.g. mon-fri, sat,sun, weekdays, weekend or all'"`
	Description *string `kong:"help='description'"`
	Enable      bool    `kong:"xor='state',help='enable the rule'"`
	Disable     bool    `kong:"xor='state',help='disable the rule'"`
}

// Run changes a rule.
func (c *accessRulesSetCmd) Run(opt *Options) error {
	read := func(ctx context.Context, cl *amplifi.Client) (access.Rule, error) {
		rules, err := access.Rules(ctx, cl)
		if err != nil {
			return access.Rule{}, err
		}
		return accessFindRule(rules, c.ID)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change rule %s on %s?", c.ID, c.Host), read,
		func(cs *changeSet, r access.Rule) writeFunc {
			u := c.update(cs, r)
			return func(ctx context.Context, cl *amplifi.Client) error { return access.UpdateRule(ctx, cl, r.ID, u) }
		})
}

// update builds the update for rule r from the flags, recording the changes
// and any invalid flag values in cs.
func (c *accessRulesSetCmd) update(cs *changeSet, r access.Rule) access.RuleUpdate {
	var u access.RuleUpdate

	start := r.StartTime
	if c.Start != "" {
		m, err := parseClock(c.Start)
		if err != nil {
			cs.fail("start", err)
			return u
		}
		start = clockText(m)
		u.StartTime = &start
	}
	duration, err := accessRuleLength(start, c.End, c.Duration)
	if err != nil {
		cs.fail("duration", err)
		return u
	}
	if duration != "" {
		u.Duration = &duration
	}
	if u.StartTime != nil || u.Duration != nil {
		cs.add("time", accessWindow(r.StartTime, r.Duration), accessWindow(start, cmp.Or(duration, r.Duration)))
	}

	if c.Days != "" {
		days, err := accessParseDays(c.Days)
		if err != nil {
			cs.fail("days", err)
			return u
		}
		u.WeekDays = days
		cs.add("days", accessDaysString(r.WeekDays), accessDaysString(days))
	}
	u.Description = cs.text("description", c.Description, r.Description)
	if c.Enable || c.Disable {
		enabled := onOffFlag(c.Enable)
		u.Enabled = cs.onOff("enabled", &enabled, r.Enabled)
	}
	return u
}

type accessRulesRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	IDs       []string `kong:"arg,name='id',help='rule IDs'"`
}

// Run deletes rules.
func (c *accessRulesRmCmd) Run(opt *Options) error {
	return apply(opt, &c.unitFlags, c.yesFlag, "delete these rules on "+c.Host+"?", access.Rules,
		func(cs *changeSet, rules []access.Rule) writeFunc {
			for _, id := range c.IDs {
				r, err := accessFindRule(rules, id)
				if err != nil {
					cs.fail("rule", err)
					continue
				}
				cs.remove("rule "+r.ID, "block "+accessWindow(r.StartTime, r.Duration)+" on "+accessDaysString(r.WeekDays))
			}
			return func(ctx context.Context, cl *amplifi.Client) error { return access.DeleteRules(ctx, cl, c.IDs...) }
		})
}

// accessProfileReader returns a reader that fetches the profiles and finds
// ref among them.
func accessProfileReader(ref string) func(context.Context, *amplifi.Client) (access.Profile, error) {
	return func(ctx context.Context, cl *amplifi.Client) (access.Profile, error) {
		profiles, err := access.Profiles(ctx, cl)
		if err != nil {
			return access.Profile{}, err
		}
		return accessFindProfile(profiles, ref)
	}
}

// accessFindProfile finds a profile by ID, or else by name.
func accessFindProfile(profiles []access.Profile, ref string) (access.Profile, error) {
	return findOne(profiles, ref, "profile",
		func(p access.Profile) string { return p.ID },
		func(p access.Profile) []string { return []string{p.Name} })
}

// accessFindRule finds a rule by ID.
func accessFindRule(rules []access.Rule, id string) (access.Rule, error) {
	return findOne(rules, id, "rule", func(r access.Rule) string { return r.ID }, nil)
}

// accessNextProfileID picks the ID for a new profile: one more than the
// largest numeric ID in use.
func accessNextProfileID(profiles []access.Profile) string {
	next := 1
	for _, p := range profiles {
		if n, err := strconv.Atoi(p.ID); err == nil && n >= next {
			next = n + 1
		}
	}
	return strconv.Itoa(next)
}

// accessEditMACs returns list with add appended and remove taken out,
// without duplicates.
func accessEditMACs(list, add, remove []amplifi.MAC) []amplifi.MAC {
	out := make([]amplifi.MAC, 0, len(list)+len(add))
	has := func(s []amplifi.MAC, m amplifi.MAC) bool { return slices.ContainsFunc(s, m.Equal) }
	for _, m := range slices.Concat(list, add) {
		if has(remove, m) || has(out, m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

var accessDSCPNames = map[string]access.DSCP{
	"none":      access.DSCPNone,
	"streaming": access.DSCPStreaming,
	"gaming":    access.DSCPGaming,
}

// accessParseDSCP parses --dscp; empty means unset.
func accessParseDSCP(s string) (*access.DSCP, error) {
	if s == "" {
		return nil, nil
	}
	if v, ok := accessDSCPNames[strings.ToLower(s)]; ok {
		return &v, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 63 {
		return nil, fmt.Errorf("bad DSCP %q: want none, streaming, gaming or 0-63", s)
	}
	v := access.DSCP(n)
	return &v, nil
}

func accessDSCPName(p *access.DSCP) string {
	if p == nil {
		return "-"
	}
	for name, v := range accessDSCPNames {
		if v == *p {
			return name
		}
	}
	return strconv.Itoa(int(*p))
}

// accessDurationMinutes parses a rule duration, "HH:MM" up to 24:00.
func accessDurationMinutes(s string) (int, error) {
	if s == "24:00" {
		return 24 * 60, nil
	}
	return parseClock(s)
}

// accessRuleLength returns the rule duration ("HH:MM") from --end or
// --duration, or "" if neither is set.
func accessRuleLength(start, end, duration string) (string, error) {
	if duration != "" {
		m, err := accessDurationMinutes(duration)
		if err != nil {
			return "", err
		}
		if m == 0 {
			return "", fmt.Errorf("bad duration %q: want 00:01 to 24:00", duration)
		}
		return clockText(m), nil
	}
	if end == "" {
		return "", nil
	}

	s, err := parseClock(start)
	if err != nil {
		return "", err
	}
	e, err := parseClock(end)
	if err != nil {
		return "", err
	}
	m := (e - s + 24*60) % (24 * 60)
	if m == 0 {
		m = 24 * 60
	}
	return clockText(m), nil
}

// accessWindow formats a start time and duration as "HH:MM-HH:MM".
func accessWindow(start, duration string) string {
	s, serr := parseClock(start)
	d, derr := accessDurationMinutes(duration)
	if serr != nil || derr != nil {
		return start + "+" + duration
	}
	return clockText(s) + "-" + clockText((s+d)%(24*60))
}

var accessDayNames = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// accessParseDays parses a day list such as "mon-fri", "sat,sun", "1,3,5",
// "weekdays", "weekend" or "all" into weekday numbers 1 (Monday) to 7.
func accessParseDays(s string) ([]int, error) {
	set := map[int]bool{}
	for part := range strings.SplitSeq(strings.ToLower(s), ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "all", "daily":
			part = "mon-sun"
		case "weekdays":
			part = "mon-fri"
		case "weekend":
			part = "sat-sun"
		}

		from, to, isRange := strings.Cut(part, "-")
		a, err := accessDay(from)
		if err != nil {
			return nil, err
		}
		b := a
		if isRange {
			if b, err = accessDay(to); err != nil {
				return nil, err
			}
		}
		if b < a {
			return nil, fmt.Errorf("bad day range %q", part)
		}
		for d := a; d <= b; d++ {
			set[d] = true
		}
	}

	return slices.Sorted(maps.Keys(set)), nil
}

func accessDay(s string) (int, error) {
	if i := slices.Index(accessDayNames, s); i >= 0 {
		return i + 1, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 7 {
		return 0, fmt.Errorf("bad day %q: want mon..sun or 1..7", s)
	}
	return n, nil
}

func accessDaysString(days []int) string {
	names := make([]string, 0, len(days))
	for _, d := range days {
		if d < 1 || d > 7 {
			names = append(names, strconv.Itoa(d))
			continue
		}
		names = append(names, accessDayNames[d-1])
	}
	return orDash(strings.Join(names, ","))
}
