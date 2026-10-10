// Package access manages scheduled access control (parental
// controls).
//
// The router keeps three tables, each a JSON object keyed by ID: profiles (a
// named group of client MACs that can be paused), clients (per-client
// settings keyed by MAC, the router's "devices") and rules (time windows when
// a profile's clients are blocked, keyed by "<profile ID>.<n>"). Writes take an object of ID ->
// fields; the router answers {"changed": bool}, which is ignored. The router
// announces changes with the Device signal
// ScheduledAccessControlConfigChanged.
package access

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// Profile is a scheduled access control profile: a set of clients that rules
// and pausing apply to.
type Profile struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Enabled bool          `json:"enabled"`
	Paused  bool          `json:"paused"`
	Tag     *int          `json:"tag,omitempty"`
	Clients []amplifi.MAC `json:"clients"`
}

// profileEntry is a profile as the router sends it.
type profileEntry struct {
	Name    string        `json:"name"`
	Enabled bool          `json:"enabled"`
	Paused  bool          `json:"paused"`
	Tag     *int          `json:"tag"`
	Devices []amplifi.MAC `json:"devices"`
}

// ProfileUpdate is a partial update of a profile. Nil fields are left
// unchanged; a non-nil Clients replaces the whole member list.
type ProfileUpdate struct {
	Name    *string
	Paused  *bool
	Clients []amplifi.MAC
}

// DSCP is the traffic priority assigned to a client.
type DSCP int

// Known DSCP values.
const (
	DSCPNone      DSCP = 0
	DSCPStreaming DSCP = 40
	DSCPGaming    DSCP = 56
)

// Client is a client's entry in the access control client table.
type Client struct {
	MAC             amplifi.MAC `json:"mac"`
	Name            string      `json:"name,omitempty"`
	Tag             *int        `json:"tag,omitempty"`
	DSCP            *DSCP       `json:"dscp,omitempty"`
	R2RAccessID     string      `json:"r2r_access_id,omitempty"`
	R2RAccessIDType string      `json:"r2r_access_id_type,omitempty"`
	R2RPause        string      `json:"r2r_pause,omitempty"`
}

// ClientUpdate is a partial update of a client entry. Nil fields are left
// unchanged.
type ClientUpdate struct {
	Name *string
	Tag  *int
	DSCP *DSCP
}

// Rule is a time window during which the clients of profile ProfileID are
// blocked. Start is the time of day as a duration since midnight; Duration
// is at most 24 hours. Both have minute resolution.
type Rule struct {
	ID          string         `json:"id"`
	ProfileID   string         `json:"profile_id"`
	Description string         `json:"description,omitempty"`
	Enabled     bool           `json:"enabled"`
	Start       time.Duration  `json:"start"`
	Duration    time.Duration  `json:"duration"`
	WeekDays    []time.Weekday `json:"week_days"`
}

// MarshalJSON renders Start and Duration in seconds and the week days by
// name.
func (r Rule) MarshalJSON() ([]byte, error) {
	type rule Rule
	days := make([]string, 0, len(r.WeekDays))
	for _, d := range r.WeekDays {
		days = append(days, d.String())
	}
	return json.Marshal(struct {
		rule
		Start    int64    `json:"start"`
		Duration int64    `json:"duration"`
		WeekDays []string `json:"week_days"`
	}{rule(r), int64(r.Start / time.Second), int64(r.Duration / time.Second), days})
}

// ruleEntry is a rule as the router sends it: times are "HH:MM" and week
// days 1 (Monday) to 7 (Sunday).
type ruleEntry struct {
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	StartTime   string `json:"start_time"`
	Duration    string `json:"duration"`
	WeekDays    []int  `json:"week_days"`
}

// rule converts e. Week days outside 1 to 7 are dropped.
func (e ruleEntry) rule(id string) (Rule, error) {
	start, err := parseClock(e.StartTime)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %s: start: %w", id, err)
	}
	duration, err := parseClock(e.Duration)
	if err != nil {
		return Rule{}, fmt.Errorf("rule %s: duration: %w", id, err)
	}

	profile, _, _ := strings.Cut(id, ".")
	r := Rule{
		ID:          id,
		ProfileID:   profile,
		Description: e.Description,
		Enabled:     e.Enabled,
		Start:       start,
		Duration:    duration,
		WeekDays:    []time.Weekday{},
	}
	for _, d := range e.WeekDays {
		if d >= 1 && d <= 7 {
			r.WeekDays = append(r.WeekDays, time.Weekday(d%7))
		}
	}
	return r, nil
}

// RuleUpdate is a partial update of a rule. Nil fields are left unchanged.
type RuleUpdate struct {
	Description *string
	Enabled     *bool
	Start       *time.Duration
	Duration    *time.Duration
	WeekDays    []time.Weekday
}

// options returns the router fields u sets.
func (u RuleUpdate) options() (map[string]any, error) {
	body := map[string]any{}
	if u.Description != nil {
		body["description"] = *u.Description
	}
	if u.Enabled != nil {
		body["enabled"] = *u.Enabled
	}
	if u.Start != nil {
		s, err := startText(*u.Start)
		if err != nil {
			return nil, err
		}
		body["start_time"] = s
	}
	if u.Duration != nil {
		s, err := durationText(*u.Duration)
		if err != nil {
			return nil, err
		}
		body["duration"] = s
	}
	if u.WeekDays != nil {
		body["week_days"] = weekDayNumbers(u.WeekDays)
	}
	return body, nil
}

// newRuleID returns a fresh rule ID for profileID.
func newRuleID(profileID string) string {
	return profileID + "." + strconv.FormatInt(time.Now().UnixNano()%math.MaxInt32, 10)
}

// Profiles lists the scheduled access control profiles, ordered by ID.
func Profiles(ctx context.Context, c *amplifi.Client) ([]Profile, error) {
	var m map[string]profileEntry
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlProfiles", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access profiles: %w", err)
	}
	return profileList(m), nil
}

// AddProfile creates profile p and returns its ID. An empty p.ID picks one
// more than the largest numeric ID in use.
func AddProfile(ctx context.Context, c *amplifi.Client, p Profile) (string, error) {
	if p.ID == "" {
		profiles, err := Profiles(ctx, c)
		if err != nil {
			return "", fmt.Errorf("add access profile: %w", err)
		}
		p.ID = nextProfileID(profiles)
	}

	body := map[string]any{
		"name":    p.Name,
		"enabled": p.Enabled,
		"paused":  p.Paused,
		"devices": macStrings(p.Clients),
	}
	if p.Tag != nil {
		body["tag"] = *p.Tag
	}
	if err := write(ctx, c, "add access profile", "AddScheduledAccessControlProfiles", map[string]any{p.ID: body}); err != nil {
		return "", err
	}
	return p.ID, nil
}

// nextProfileID is one more than the largest numeric profile ID.
func nextProfileID(profiles []Profile) string {
	next := 1
	for _, p := range profiles {
		if n, err := strconv.Atoi(p.ID); err == nil && n >= next {
			next = n + 1
		}
	}
	return strconv.Itoa(next)
}

// UpdateProfile applies u to profile id.
func UpdateProfile(ctx context.Context, c *amplifi.Client, id string, u ProfileUpdate) error {
	body := map[string]any{}
	if u.Name != nil {
		body["name"] = *u.Name
	}
	if u.Paused != nil {
		body["paused"] = *u.Paused
	}
	if u.Clients != nil {
		body["devices"] = macStrings(u.Clients)
	}
	return write(ctx, c, "update access profile", "ModifyScheduledAccessControlProfiles", map[string]any{id: body})
}

// SetProfilePaused blocks (paused true) or unblocks the internet access of
// every client in profile id.
func SetProfilePaused(ctx context.Context, c *amplifi.Client, id string, paused bool) error {
	return UpdateProfile(ctx, c, id, ProfileUpdate{Paused: &paused})
}

// DeleteProfiles deletes profiles by ID.
func DeleteProfiles(ctx context.Context, c *amplifi.Client, ids ...string) error {
	return write(ctx, c, "delete access profiles", "DeleteScheduledAccessControlProfiles", deleteKeys(ids, true))
}

// Clients lists the access control client table, ordered by MAC.
func Clients(ctx context.Context, c *amplifi.Client) ([]Client, error) {
	var m map[string]Client
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlDevices", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access clients: %w", err)
	}
	return clientList(m)
}

// AddClient adds cl to the client table under cl.MAC.
func AddClient(ctx context.Context, c *amplifi.Client, cl Client) error {
	if len(cl.MAC) == 0 {
		return errors.New("add access client: empty mac")
	}
	body := map[string]any{"name": cl.Name}
	if cl.Tag != nil {
		body["tag"] = *cl.Tag
	}
	if cl.DSCP != nil {
		body["dscp"] = int(*cl.DSCP)
	}
	return write(ctx, c, "add access client", "AddScheduledAccessControlDevices", map[string]any{cl.MAC.String(): body})
}

// UpdateClient applies u to the client entry for mac.
func UpdateClient(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, u ClientUpdate) error {
	body := map[string]any{}
	if u.Name != nil {
		body["name"] = *u.Name
	}
	if u.Tag != nil {
		body["tag"] = *u.Tag
	}
	if u.DSCP != nil {
		body["dscp"] = int(*u.DSCP)
	}
	return write(ctx, c, "update access client", "ModifyScheduledAccessControlDevices", map[string]any{mac.String(): body})
}

// DeleteClients removes client entries by MAC.
func DeleteClients(ctx context.Context, c *amplifi.Client, macs ...amplifi.MAC) error {
	return write(ctx, c, "delete access clients", "DeleteScheduledAccessControlDevices", deleteKeys(macStrings(macs), false))
}

// Rules lists the scheduled access control rules of all profiles, ordered by
// ID.
func Rules(ctx context.Context, c *amplifi.Client) ([]Rule, error) {
	var m map[string]ruleEntry
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlRules", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access rules: %w", err)
	}
	rules, err := ruleList(m)
	if err != nil {
		return nil, fmt.Errorf("access rules: %w", err)
	}
	return rules, nil
}

// AddRule creates rule r and returns its ID. An empty r.ID generates one for
// r.ProfileID; a given ID must be "<profile ID>.<n>".
func AddRule(ctx context.Context, c *amplifi.Client, r Rule) (string, error) {
	switch {
	case r.ID == "" && r.ProfileID == "":
		return "", errors.New("add access rule: no rule or profile ID")
	case r.ID == "":
		r.ID = newRuleID(r.ProfileID)
	case !strings.Contains(r.ID, "."):
		return "", fmt.Errorf("add access rule: id %q is not <profile>.<n>", r.ID)
	}

	body, err := r.update().options()
	if err != nil {
		return "", fmt.Errorf("add access rule: %w", err)
	}
	if err := write(ctx, c, "add access rule", "AddScheduledAccessControlRules", map[string]any{r.ID: body}); err != nil {
		return "", err
	}
	return r.ID, nil
}

// update returns every field of r as an update, for writing a new rule.
func (r Rule) update() RuleUpdate {
	u := RuleUpdate{Enabled: &r.Enabled, Start: &r.Start, Duration: &r.Duration, WeekDays: r.WeekDays}
	if u.WeekDays == nil {
		u.WeekDays = []time.Weekday{}
	}
	if r.Description != "" {
		u.Description = &r.Description
	}
	return u
}

// UpdateRule applies u to rule id.
func UpdateRule(ctx context.Context, c *amplifi.Client, id string, u RuleUpdate) error {
	body, err := u.options()
	if err != nil {
		return fmt.Errorf("update access rule: %w", err)
	}
	return write(ctx, c, "update access rule", "ModifyScheduledAccessControlRules", map[string]any{id: body})
}

// DeleteRules deletes rules by ID.
func DeleteRules(ctx context.Context, c *amplifi.Client, ids ...string) error {
	return write(ctx, c, "delete access rules", "DeleteScheduledAccessControlRules", deleteKeys(ids, false))
}

// write calls a Device access control writer; what describes the write in
// errors. Its {"changed": bool} answer says nothing useful, as an unchanged
// value is not an error.
func write(ctx context.Context, c *amplifi.Client, what, method string, arg any) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, method, arg, nil); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// deleteKeys builds the argument of the Delete* methods. Profile IDs are sent
// as numbers where they are numeric.
func deleteKeys(ids []string, numeric bool) map[string]any {
	keys := make([]any, 0, len(ids))
	for _, id := range ids {
		if n, err := strconv.Atoi(id); numeric && err == nil {
			keys = append(keys, n)
			continue
		}
		keys = append(keys, id)
	}
	return map[string]any{"keys": keys}
}

func macStrings(macs []amplifi.MAC) []string {
	out := make([]string, 0, len(macs))
	for _, m := range macs {
		out = append(out, m.String())
	}
	return out
}

func profileList(m map[string]profileEntry) []Profile {
	out := make([]Profile, 0, len(m))
	for id, e := range m {
		p := Profile{ID: id, Name: e.Name, Enabled: e.Enabled, Paused: e.Paused, Tag: e.Tag, Clients: e.Devices}
		if p.Clients == nil {
			p.Clients = []amplifi.MAC{}
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Profile) int { return rpc.CompareNumeric(a.ID, b.ID) })
	return out
}

func clientList(m map[string]Client) ([]Client, error) {
	out := make([]Client, 0, len(m))
	for key, cl := range m {
		mac, err := amplifi.ParseMAC(key)
		if err != nil {
			return nil, fmt.Errorf("access clients: %w", err)
		}
		cl.MAC = mac
		out = append(out, cl)
	}
	slices.SortFunc(out, func(a, b Client) int { return cmp.Compare(a.MAC.String(), b.MAC.String()) })
	return out, nil
}

func ruleList(m map[string]ruleEntry) ([]Rule, error) {
	out := make([]Rule, 0, len(m))
	for id, e := range m {
		r, err := e.rule(id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Rule) int { return compareRuleIDs(a.ID, b.ID) })
	return out, nil
}

// compareRuleIDs orders "<profile>.<n>" IDs numerically part by part, so
// "2.9" sorts before "10.1".
func compareRuleIDs(a, b string) int {
	ap, an, _ := strings.Cut(a, ".")
	bp, bn, _ := strings.Cut(b, ".")
	return cmp.Or(rpc.CompareNumeric(ap, bp), rpc.CompareNumeric(an, bn))
}

// parseClock parses the router's "HH:MM", from 00:00 to 24:00.
func parseClock(s string) (time.Duration, error) {
	h, m, ok := strings.Cut(s, ":")
	hours, herr := strconv.Atoi(h)
	minutes, merr := strconv.Atoi(m)
	if !ok || herr != nil || merr != nil || hours < 0 || minutes < 0 || minutes > 59 || hours*60+minutes > 24*60 {
		return 0, fmt.Errorf("invalid time %q: want HH:MM", s)
	}
	return time.Duration(hours)*time.Hour + time.Duration(minutes)*time.Minute, nil
}

// clockText formats d as "HH:MM", dropping seconds.
func clockText(d time.Duration) string {
	minutes := int(d / time.Minute)
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// startText formats a rule start, a time of day.
func startText(d time.Duration) (string, error) {
	if d < 0 || d >= 24*time.Hour {
		return "", fmt.Errorf("start %s is not a time of day", d)
	}
	return clockText(d), nil
}

// durationText formats a rule duration, from one minute to 24 hours.
func durationText(d time.Duration) (string, error) {
	if d < time.Minute || d > 24*time.Hour {
		return "", fmt.Errorf("duration %s: want 1m to 24h", d)
	}
	return clockText(d), nil
}

// weekDayNumbers converts week days to the router's 1 (Monday) to 7
// (Sunday).
func weekDayNumbers(days []time.Weekday) []int {
	out := make([]int, 0, len(days))
	for _, d := range days {
		if d == time.Sunday {
			out = append(out, 7)
			continue
		}
		out = append(out, int(d))
	}
	return out
}
