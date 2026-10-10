// Package access manages scheduled access control (parental
// controls).
//
// The router keeps three tables, each a JSON object keyed by ID: profiles (a
// named group of client MACs that can be paused), devices (per-client
// settings keyed by MAC) and rules (time windows when a profile's clients are
// blocked, keyed by "<profile ID>.<n>"). Writes take an object of ID ->
// fields; the router answers {"changed": bool}, which is ignored. The router
// announces changes with the Device signal
// ScheduledAccessControlConfigChanged.
package access

import (
	"cmp"
	"context"
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
	Devices []amplifi.MAC `json:"devices"`
}

// ProfileUpdate is a partial update of a profile. Nil fields are left
// unchanged; a non-nil Devices replaces the whole member list.
type ProfileUpdate struct {
	Name    *string
	Paused  *bool
	Devices []amplifi.MAC
}

// DSCP is the traffic priority assigned to a client.
type DSCP int

// Known DSCP values.
const (
	DSCPNone      DSCP = 0
	DSCPStreaming DSCP = 40
	DSCPGaming    DSCP = 56
)

// Device is a client's entry in the access control device table.
type Device struct {
	MAC             amplifi.MAC `json:"mac"`
	Name            string      `json:"name,omitempty"`
	Tag             *int        `json:"tag,omitempty"`
	DSCP            *DSCP       `json:"dscp,omitempty"`
	R2RAccessID     string      `json:"r2r_access_id,omitempty"`
	R2RAccessIDType string      `json:"r2r_access_id_type,omitempty"`
	R2RPause        string      `json:"r2r_pause,omitempty"`
}

// DeviceUpdate is a partial update of a device entry. Nil fields are left
// unchanged.
type DeviceUpdate struct {
	Name *string
	Tag  *int
	DSCP *DSCP
}

// Rule is a time window during which a profile's clients are blocked.
// StartTime and Duration are "HH:MM"; WeekDays are 1 (Monday) to 7 (Sunday).
type Rule struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Enabled     bool   `json:"enabled"`
	StartTime   string `json:"start_time"`
	Duration    string `json:"duration"`
	WeekDays    []int  `json:"week_days"`
}

// ProfileID returns the ID of the profile the rule belongs to, the part of
// its ID before the first dot.
func (r Rule) ProfileID() string {
	id, _, _ := strings.Cut(r.ID, ".")
	return id
}

// RuleUpdate is a partial update of a rule. Nil fields are left unchanged.
type RuleUpdate struct {
	Description *string
	Enabled     *bool
	StartTime   *string
	Duration    *string
	WeekDays    []int
}

// NewRuleID returns a fresh rule ID for profileID.
func NewRuleID(profileID string) string {
	return profileID + "." + strconv.FormatInt(time.Now().UnixNano()%math.MaxInt32, 10)
}

// Profiles lists the scheduled access control profiles, ordered by ID.
func Profiles(ctx context.Context, c *amplifi.Client) ([]Profile, error) {
	var m map[string]Profile
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlProfiles", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access profiles: %w", err)
	}
	return profileList(m), nil
}

// AddProfile creates profile p under p.ID.
func AddProfile(ctx context.Context, c *amplifi.Client, p Profile) error {
	if p.ID == "" {
		return errors.New("add access profile: empty id")
	}
	body := map[string]any{
		"name":    p.Name,
		"enabled": p.Enabled,
		"paused":  p.Paused,
		"devices": macStrings(p.Devices),
	}
	if p.Tag != nil {
		body["tag"] = *p.Tag
	}
	return write(ctx, c, "add access profile", "AddScheduledAccessControlProfiles", map[string]any{p.ID: body})
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
	if u.Devices != nil {
		body["devices"] = macStrings(u.Devices)
	}
	return write(ctx, c, "update access profile", "ModifyScheduledAccessControlProfiles", map[string]any{id: body})
}

// PauseProfile blocks (paused true) or unblocks the internet access of every
// client in profile id.
func PauseProfile(ctx context.Context, c *amplifi.Client, id string, paused bool) error {
	return UpdateProfile(ctx, c, id, ProfileUpdate{Paused: &paused})
}

// DeleteProfiles deletes profiles by ID.
func DeleteProfiles(ctx context.Context, c *amplifi.Client, ids ...string) error {
	return write(ctx, c, "delete access profiles", "DeleteScheduledAccessControlProfiles", deleteKeys(ids, true))
}

// Devices lists the access control device table, ordered by MAC.
func Devices(ctx context.Context, c *amplifi.Client) ([]Device, error) {
	var m map[string]Device
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlDevices", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access devices: %w", err)
	}
	return deviceList(m)
}

// AddDevice adds d to the device table under d.MAC.
func AddDevice(ctx context.Context, c *amplifi.Client, d Device) error {
	if len(d.MAC) == 0 {
		return errors.New("add access device: empty mac")
	}
	body := map[string]any{"name": d.Name}
	if d.Tag != nil {
		body["tag"] = *d.Tag
	}
	if d.DSCP != nil {
		body["dscp"] = int(*d.DSCP)
	}
	return write(ctx, c, "add access device", "AddScheduledAccessControlDevices", map[string]any{d.MAC.String(): body})
}

// UpdateDevice applies u to the device entry for mac.
func UpdateDevice(ctx context.Context, c *amplifi.Client, mac amplifi.MAC, u DeviceUpdate) error {
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
	return write(ctx, c, "update access device", "ModifyScheduledAccessControlDevices", map[string]any{mac.String(): body})
}

// DeleteDevices removes device entries by MAC.
func DeleteDevices(ctx context.Context, c *amplifi.Client, macs ...amplifi.MAC) error {
	return write(ctx, c, "delete access devices", "DeleteScheduledAccessControlDevices", deleteKeys(macStrings(macs), false))
}

// Rules lists the scheduled access control rules of all profiles, ordered by
// ID.
func Rules(ctx context.Context, c *amplifi.Client) ([]Rule, error) {
	var m map[string]Rule
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "GetScheduledAccessControlRules", map[string]any{}, &m); err != nil {
		return nil, fmt.Errorf("access rules: %w", err)
	}
	return ruleList(m), nil
}

// AddRule creates rule r under r.ID, which must start with the ID of its
// profile and a dot (see NewRuleID).
func AddRule(ctx context.Context, c *amplifi.Client, r Rule) error {
	if !strings.Contains(r.ID, ".") {
		return fmt.Errorf("add access rule: id %q is not <profile>.<n>", r.ID)
	}
	if r.WeekDays == nil {
		r.WeekDays = []int{}
	}
	body := map[string]any{
		"enabled":    r.Enabled,
		"start_time": r.StartTime,
		"duration":   r.Duration,
		"week_days":  r.WeekDays,
	}
	if r.Description != "" {
		body["description"] = r.Description
	}
	return write(ctx, c, "add access rule", "AddScheduledAccessControlRules", map[string]any{r.ID: body})
}

// UpdateRule applies u to rule id.
func UpdateRule(ctx context.Context, c *amplifi.Client, id string, u RuleUpdate) error {
	body := map[string]any{}
	if u.Description != nil {
		body["description"] = *u.Description
	}
	if u.Enabled != nil {
		body["enabled"] = *u.Enabled
	}
	if u.StartTime != nil {
		body["start_time"] = *u.StartTime
	}
	if u.Duration != nil {
		body["duration"] = *u.Duration
	}
	if u.WeekDays != nil {
		body["week_days"] = u.WeekDays
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

func profileList(m map[string]Profile) []Profile {
	out := make([]Profile, 0, len(m))
	for id, p := range m {
		p.ID = id
		if p.Devices == nil {
			p.Devices = []amplifi.MAC{}
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Profile) int { return rpc.CompareNumeric(a.ID, b.ID) })
	return out
}

func deviceList(m map[string]Device) ([]Device, error) {
	out := make([]Device, 0, len(m))
	for key, d := range m {
		mac, err := amplifi.ParseMAC(key)
		if err != nil {
			return nil, fmt.Errorf("access devices: %w", err)
		}
		d.MAC = mac
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b Device) int { return cmp.Compare(a.MAC.String(), b.MAC.String()) })
	return out, nil
}

func ruleList(m map[string]Rule) []Rule {
	out := make([]Rule, 0, len(m))
	for id, r := range m {
		r.ID = id
		if r.WeekDays == nil {
			r.WeekDays = []int{}
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Rule) int { return compareRuleIDs(a.ID, b.ID) })
	return out
}

// compareRuleIDs orders "<profile>.<n>" IDs numerically part by part, so
// "2.9" sorts before "10.1".
func compareRuleIDs(a, b string) int {
	ap, an, _ := strings.Cut(a, ".")
	bp, bn, _ := strings.Cut(b, ".")
	return cmp.Or(rpc.CompareNumeric(ap, bp), rpc.CompareNumeric(an, bn))
}
