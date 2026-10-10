package cli

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/access"
)

type accessCmd struct {
	Profiles accessProfilesCmd `kong:"cmd,help='access control profiles (groups of clients)'"`
	Clients  accessClientsCmd  `kong:"cmd,help='access control client table'"`
	Rules    accessRulesCmd    `kong:"cmd,help='blocking schedules of profiles'"`
}

type accessProfilesCmd struct {
	List   accessProfilesListCmd   `kong:"cmd,aliases='ls',help='list profiles'"`
	Add    accessProfilesAddCmd    `kong:"cmd,help='add a profile'"`
	Set    accessProfilesSetCmd    `kong:"cmd,help='change the name or clients of a profile'"`
	Rm     accessProfilesRmCmd     `kong:"cmd,help='delete profiles'"`
	Pause  accessProfilesPauseCmd  `kong:"cmd,help='block internet access for the clients of a profile'"`
	Resume accessProfilesResumeCmd `kong:"cmd,help='restore internet access for the clients of a profile'"`
}

type accessClientsCmd struct {
	List accessClientsListCmd `kong:"cmd,aliases='ls',help='list client entries'"`
	Add  accessClientsAddCmd  `kong:"cmd,help='add a client entry'"`
	Set  accessClientsSetCmd  `kong:"cmd,help='change a client entry'"`
	Rm   accessClientsRmCmd   `kong:"cmd,help='delete client entries'"`
}

type accessRulesCmd struct {
	List accessRulesListCmd `kong:"cmd,aliases='ls',help='list rules'"`
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
		fmt.Fprintln(w, "ID\tNAME\tENABLED\tPAUSED\tCLIENTS")
		for _, p := range profiles {
			fmt.Fprintf(w, "%s\t%s\t%t\t%t\t%s\n", p.ID, p.Name, p.Enabled, p.Paused, joinMACs(p.Clients))
		}
		return w.Flush()
	})
}

type accessProfilesAddCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Name      string   `kong:"arg,help='profile name'"`
	Client    []string `kong:"help='MAC address of a client to put in the profile (repeatable)'"`
	Paused    bool     `kong:"help='add the profile paused'"`
}

// Run adds a profile.
func (c *accessProfilesAddCmd) Run(opt *Options) error {
	clients, err := parseMACs(c.Client)
	if err != nil {
		return err
	}
	p := access.Profile{Name: c.Name, Enabled: true, Paused: c.Paused, Clients: clients}

	var cs changeSet
	cs.create("name", p.Name)
	cs.create("paused", onOff(p.Paused))
	cs.create("clients", joinMACs(p.Clients))
	ok, err := cs.approve(c.yesFlag, fmt.Sprintf("add profile %q on %s?", c.Name, c.Host))
	if err != nil || !ok {
		return err
	}
	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error {
		id, err := access.AddProfile(ctx, cl, p)
		if err != nil {
			return err
		}
		fmt.Printf("added profile %s\n", id)
		return nil
	})
}

type accessProfilesSetCmd struct {
	unitFlags    `kong:"embed"`
	yesFlag      `kong:"embed"`
	Profile      string   `kong:"arg,help='profile ID or name'"`
	Name         *string  `kong:"help='new name'"`
	AddClient    []string `kong:"help='MAC address of a client to add (repeatable)'"`
	RemoveClient []string `kong:"help='MAC address of a client to remove (repeatable)'"`
}

// Run changes a profile.
func (c *accessProfilesSetCmd) Run(opt *Options) error {
	add, err := parseMACs(c.AddClient)
	if err != nil {
		return err
	}
	remove, err := parseMACs(c.RemoveClient)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change profile %s on %s?", c.Profile, c.Host), accessProfileReader(c.Profile),
		func(cs *changeSet, p access.Profile) writeFunc {
			u := access.ProfileUpdate{Name: cs.text("name", c.Name, p.Name)}
			if len(add) > 0 || len(remove) > 0 {
				u.Clients = accessEditMACs(p.Clients, add, remove)
				cs.add("clients", joinMACs(p.Clients), joinMACs(u.Clients))
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
				cs.remove("profile "+p.ID, fmt.Sprintf("%q (clients %s)", p.Name, joinMACs(p.Clients)))
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
			return func(ctx context.Context, cl *amplifi.Client) error {
				return access.SetProfilePaused(ctx, cl, p.ID, paused)
			}
		})
}

type accessClientsListCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists the client entries.
func (c *accessClientsListCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		clients, err := access.Clients(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(clients)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "MAC\tNAME\tTAG\tDSCP")
		for _, d := range clients {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.MAC, d.Name, intOrDash(d.Tag), accessDSCPName(d.DSCP))
		}
		return w.Flush()
	})
}

type accessClientsAddCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MAC       string `kong:"arg,name='mac',help='client MAC address'"`
	Name      string `kong:"help='client name'"`
	DSCP      string `kong:"name='dscp',help='traffic priority: none, streaming, gaming or a DSCP number'"`
	Tag       *int   `kong:"help='tag'"`
}

// Run adds a client entry.
func (c *accessClientsAddCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	dscp, err := accessParseDSCP(c.DSCP)
	if err != nil {
		return err
	}
	d := access.Client{MAC: mac, Name: c.Name, Tag: c.Tag, DSCP: dscp}

	var cs changeSet
	cs.create("mac", d.MAC.String())
	cs.create("name", d.Name)
	cs.create("tag", intOrDash(d.Tag))
	cs.create("dscp", accessDSCPName(d.DSCP))
	ok, err := cs.approve(c.yesFlag, "add this client entry on "+c.Host+"?")
	if err != nil || !ok {
		return err
	}
	return c.commit(opt, func(ctx context.Context, cl *amplifi.Client) error { return access.AddClient(ctx, cl, d) })
}

type accessClientsSetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MAC       string  `kong:"arg,name='mac',help='client MAC address'"`
	Name      *string `kong:"help='client name'"`
	DSCP      string  `kong:"name='dscp',help='traffic priority: none, streaming, gaming or a DSCP number'"`
	Tag       *int    `kong:"help='tag'"`
}

// Run changes a client entry.
func (c *accessClientsSetCmd) Run(opt *Options) error {
	mac, err := amplifi.ParseMAC(c.MAC)
	if err != nil {
		return err
	}
	dscp, err := accessParseDSCP(c.DSCP)
	if err != nil {
		return err
	}

	read := func(ctx context.Context, cl *amplifi.Client) (access.Client, error) {
		clients, err := access.Clients(ctx, cl)
		if err != nil {
			return access.Client{}, err
		}
		return findOne(clients, mac.String(), "client entry", func(d access.Client) string { return d.MAC.String() }, nil)
	}
	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("change client entry %s on %s?", mac, c.Host), read,
		func(cs *changeSet, d access.Client) writeFunc {
			cs.text("name", c.Name, d.Name)
			if c.Tag != nil {
				cs.add("tag", intOrDash(d.Tag), intOrDash(c.Tag))
			}
			if dscp != nil {
				cs.add("dscp", accessDSCPName(d.DSCP), accessDSCPName(dscp))
			}
			u := access.ClientUpdate{Name: c.Name, Tag: c.Tag, DSCP: dscp}
			return func(ctx context.Context, cl *amplifi.Client) error { return access.UpdateClient(ctx, cl, d.MAC, u) }
		})
}

type accessClientsRmCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	MACs      []string `kong:"arg,name='mac',help='client MAC addresses'"`
}

// Run deletes client entries.
func (c *accessClientsRmCmd) Run(opt *Options) error {
	macs, err := parseMACs(c.MACs)
	if err != nil {
		return err
	}

	prompt := fmt.Sprintf("delete the client entries for %s on %s?", joinMACs(macs), c.Host)
	return c.run(opt, c.yesFlag, prompt, func(ctx context.Context, cl *amplifi.Client) error {
		return access.DeleteClients(ctx, cl, macs...)
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
			rules = slices.DeleteFunc(rules, func(r access.Rule) bool { return r.ProfileID != p.ID })
		}

		if opt.JSON() {
			return printJSON(rules)
		}

		w := newTable(os.Stdout)
		fmt.Fprintln(w, "ID\tPROFILE\tENABLED\tTIME\tDAYS\tDESCRIPTION")
		for _, r := range rules {
			fmt.Fprintf(w, "%s\t%s\t%t\t%s\t%s\t%s\n", r.ID, names[r.ProfileID], r.Enabled, accessWindow(r.Start, r.Duration), accessDaysString(r.WeekDays), r.Description)
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
	duration, err := accessRuleLength(start, c.End, c.Duration)
	if err != nil {
		return err
	}
	if duration == nil {
		return errors.New("give --end or --duration")
	}
	days, err := accessParseDays(c.Days)
	if err != nil {
		return err
	}

	return apply(opt, &c.unitFlags, c.yesFlag, fmt.Sprintf("add this rule to profile %s on %s?", c.Profile, c.Host), accessProfileReader(c.Profile),
		func(cs *changeSet, p access.Profile) writeFunc {
			r := access.Rule{
				ProfileID:   p.ID,
				Description: c.Description,
				Enabled:     !c.Disabled,
				Start:       start,
				Duration:    *duration,
				WeekDays:    days,
			}
			cs.create("profile", fmt.Sprintf("%s %q", p.ID, p.Name))
			cs.create("time", accessWindow(r.Start, r.Duration))
			cs.create("days", accessDaysString(r.WeekDays))
			cs.create("enabled", onOff(r.Enabled))
			cs.create("description", r.Description)
			return func(ctx context.Context, cl *amplifi.Client) error {
				id, err := access.AddRule(ctx, cl, r)
				if err != nil {
					return err
				}
				fmt.Printf("added rule %s\n", id)
				return nil
			}
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

	start := r.Start
	if c.Start != "" {
		d, err := parseClock(c.Start)
		if err != nil {
			cs.fail("start", err)
			return u
		}
		start = d
		u.Start = &start
	}
	duration, err := accessRuleLength(start, c.End, c.Duration)
	if err != nil {
		cs.fail("duration", err)
		return u
	}
	u.Duration = duration
	if u.Start != nil || u.Duration != nil {
		next := r.Duration
		if duration != nil {
			next = *duration
		}
		cs.add("time", accessWindow(r.Start, r.Duration), accessWindow(start, next))
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
				cs.remove("rule "+r.ID, "block "+accessWindow(r.Start, r.Duration)+" on "+accessDaysString(r.WeekDays))
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

// accessParseDuration parses a rule duration, HH:MM from 00:01 to 24:00.
func accessParseDuration(s string) (time.Duration, error) {
	if s == "24:00" {
		return 24 * time.Hour, nil
	}
	d, err := parseClock(s)
	if err != nil {
		return 0, err
	}
	if d == 0 {
		return 0, fmt.Errorf("bad duration %q: want 00:01 to 24:00", s)
	}
	return d, nil
}

// accessRuleLength returns the rule duration from --end or --duration, or
// nil if neither is set.
func accessRuleLength(start time.Duration, end, duration string) (*time.Duration, error) {
	if duration != "" {
		d, err := accessParseDuration(duration)
		if err != nil {
			return nil, err
		}
		return &d, nil
	}
	if end == "" {
		return nil, nil
	}

	e, err := parseClock(end)
	if err != nil {
		return nil, err
	}
	d := (e - start + 24*time.Hour) % (24 * time.Hour)
	if d == 0 {
		d = 24 * time.Hour
	}
	return &d, nil
}

// accessWindow formats a start time and duration as "HH:MM-HH:MM".
func accessWindow(start, duration time.Duration) string {
	return clockText(start) + "-" + clockText((start+duration)%(24*time.Hour))
}

// accessWeek lists the week days Monday first.
var accessWeek = []time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
}

// accessParseDays parses a day list such as "mon-fri", "sat,sun", "1,3,5"
// (1 is Monday), "weekdays", "weekend" or "all", Monday first.
func accessParseDays(s string) ([]time.Weekday, error) {
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

	days := make([]time.Weekday, 0, len(set))
	for _, i := range slices.Sorted(maps.Keys(set)) {
		days = append(days, accessWeek[i])
	}
	return days, nil
}

// accessDay parses a day name or number (1 is Monday) into an index of
// accessWeek.
func accessDay(s string) (int, error) {
	if i := slices.IndexFunc(accessWeek, func(d time.Weekday) bool { return accessDayName(d) == s }); i >= 0 {
		return i, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 7 {
		return 0, fmt.Errorf("bad day %q: want mon..sun or 1..7", s)
	}
	return n - 1, nil
}

// accessDayName is the three-letter lowercase name of d.
func accessDayName(d time.Weekday) string {
	return strings.ToLower(d.String()[:3])
}

func accessDaysString(days []time.Weekday) string {
	names := make([]string, 0, len(days))
	for _, d := range days {
		names = append(names, accessDayName(d))
	}
	return orDash(strings.Join(names, ","))
}
