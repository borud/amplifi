// Package firmware checks for and installs firmware upgrades across an
// AmpliFi mesh (the FwUpdate interface), reports their progress, and reads
// and writes the automatic update settings. Upgrading only the unit a
// session is connected to is device.Upgrade.
package firmware

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// StatusCode is a firmware update status code, reported in Version.Code and
// CheckResult.Code.
type StatusCode int

// Firmware update status codes.
const (
	CodeValid          StatusCode = 1
	CodeGenuine        StatusCode = 2
	CodeInvalidFile    StatusCode = 3
	CodeReady          StatusCode = 4
	CodeNotAvailable   StatusCode = 5
	CodeUpgradeError   StatusCode = 6
	CodeUpgradeStarted StatusCode = 7
	CodeDownloadError  StatusCode = 8
	CodeSamePending    StatusCode = 9
	CodeDownloading    StatusCode = 16
	CodeAvailable      StatusCode = 32
	CodeUpgrading      StatusCode = 64
	CodeCheckError     StatusCode = 128
)

// State is the phase of a mesh-wide firmware check or upgrade.
type State string

// Firmware update states.
const (
	StateIdle        State = "idle"
	StateChecking    State = "checking"
	StateDownloading State = "downloading"
	StateStarted     State = "started"
	StateInProgress  State = "in_progress"
	StateUpToDate    State = "up_to_date"
)

var states = []State{StateIdle, StateChecking, StateDownloading, StateStarted, StateInProgress, StateUpToDate}

// String returns the state as the router names it.
func (s State) String() string { return string(s) }

// MarshalText renders the state by name.
func (s State) MarshalText() ([]byte, error) { return []byte(s), nil }

// ParseState parses a state name such as "idle" or "in_progress".
func ParseState(s string) (State, error) {
	if i := slices.Index(states, State(s)); i >= 0 {
		return states[i], nil
	}
	return "", fmt.Errorf("unknown firmware state %q (want one of %v)", s, states)
}

// Busy reports whether a check, download or upgrade is in progress.
func (s State) Busy() bool {
	switch s {
	case StateChecking, StateDownloading, StateStarted, StateInProgress:
		return true
	}
	return false
}

// Version identifies a firmware image.
type Version struct {
	Version  string     `json:"version"`
	Revision string     `json:"revision"`
	Status   string     `json:"status"`
	Code     StatusCode `json:"code"`
}

// UnitVersion is the firmware one unit runs and the upgrade available to
// it, if any.
type UnitVersion struct {
	MAC       amplifi.MAC  `json:"mac"`
	Role      amplifi.Role `json:"role"`
	Current   Version      `json:"current"`
	Upgrade   Version      `json:"upgrade"`
	LastCheck time.Time    `json:"last_check,omitzero"`
}

// HasUpgrade reports whether an upgrade other than the running firmware is
// available.
func (u UnitVersion) HasUpgrade() bool {
	return u.Upgrade.Version != "" && u.Upgrade.Version != u.Current.Version
}

// Versions is the mesh-wide firmware state: what each unit runs and what it
// can upgrade to, the router first.
type Versions struct {
	State State         `json:"state"`
	Units []UnitVersion `json:"units"`
}

// Progress is the progress of a firmware check or upgrade across the mesh,
// the router first.
type Progress struct {
	State State          `json:"state"`
	Units []UnitProgress `json:"units"`
}

// UnitProgress is one unit's part of a Progress.
type UnitProgress struct {
	UnitVersion
	Check CheckResult `json:"check"`
}

// CheckResult is the outcome of a unit's last update check.
type CheckResult struct {
	Code   StatusCode `json:"code"`
	Status string     `json:"status"`
}

// unitEntry is one unit's firmware as the router sends it.
type unitEntry struct {
	Type          string  `json:"type"`
	Current       Version `json:"current"`
	Upgrade       Version `json:"upgrade"`
	LastCheckTime int64   `json:"last_check_time"`
}

// version converts e for the unit keyed mac.
func (e unitEntry) version(mac string) UnitVersion {
	v := UnitVersion{Role: role(e.Type), Current: e.Current, Upgrade: e.Upgrade}
	v.MAC, _ = amplifi.ParseMAC(mac)
	if e.LastCheckTime > 0 {
		v.LastCheck = time.Unix(e.LastCheckTime, 0)
	}
	return v
}

// role converts the router's unit type.
func role(typ string) amplifi.Role {
	switch typ {
	case "router":
		return amplifi.RoleRouter
	case "extender", "mesh-point":
		return amplifi.RoleMeshPoint
	}
	return amplifi.Role(typ)
}

// compareUnits orders units with the router first and the rest by MAC.
func compareUnits(a, b UnitVersion) int {
	routerA, routerB := a.Role == amplifi.RoleRouter, b.Role == amplifi.RoleRouter
	if routerA != routerB {
		if routerA {
			return -1
		}
		return 1
	}
	return bytes.Compare(a.MAC, b.MAC)
}

// versionsEntry is a GetAvailableUpdates result, keyed by unit MAC.
type versionsEntry struct {
	Status State                `json:"status"`
	Nodes  map[string]unitEntry `json:"nodes"`
}

func (e versionsEntry) versions() *Versions {
	units := make([]UnitVersion, 0, len(e.Nodes))
	for _, mac := range slices.Sorted(maps.Keys(e.Nodes)) {
		units = append(units, e.Nodes[mac].version(mac))
	}
	slices.SortStableFunc(units, compareUnits)
	return &Versions{State: e.Status, Units: units}
}

// progressEntry is a GetFwUpdateStatus result, keyed by unit MAC.
type progressEntry struct {
	Status State `json:"status"`
	Nodes  map[string]struct {
		Check CheckResult `json:"check_result"`
		Info  unitEntry   `json:"info_result"`
	} `json:"nodes"`
}

func (e progressEntry) progress() *Progress {
	units := make([]UnitProgress, 0, len(e.Nodes))
	for _, mac := range slices.Sorted(maps.Keys(e.Nodes)) {
		n := e.Nodes[mac]
		units = append(units, UnitProgress{UnitVersion: n.Info.version(mac), Check: n.Check})
	}
	slices.SortStableFunc(units, func(a, b UnitProgress) int { return compareUnits(a.UnitVersion, b.UnitVersion) })
	return &Progress{State: e.Status, Units: units}
}

// GetVersions reports the firmware each unit in the mesh runs and any
// upgrade the router has found for it. It does not start a new check.
func GetVersions(ctx context.Context, c *amplifi.Client) (*Versions, error) {
	var e versionsEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceFwUpdate, "GetAvailableUpdates", nil, &e); err != nil {
		return nil, fmt.Errorf("firmware versions: %w", err)
	}
	return e.versions(), nil
}

// GetProgress reports the progress of a firmware check or upgrade.
func GetProgress(ctx context.Context, c *amplifi.Client) (*Progress, error) {
	var e progressEntry
	if err := rpc.Value(ctx, c, amplifi.IfaceFwUpdate, "GetFwUpdateStatus", nil, &e); err != nil {
		return nil, fmt.Errorf("firmware progress: %w", err)
	}
	return e.progress(), nil
}

// StartCheck asks the router to look for new firmware for every unit in the
// mesh. The check runs in the background; follow it with GetProgress and
// read the result with GetVersions.
func StartCheck(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceFwUpdate, "CheckForFwUpdate", nil); err != nil {
		return fmt.Errorf("start firmware check: %w", err)
	}
	return nil
}

// StartUpgrade starts upgrading every unit in the mesh that has an upgrade
// available. The units download, flash and reboot on their own; follow the
// progress with GetProgress until the router goes away.
func StartUpgrade(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceFwUpdate, "StartClusterUpgrade", nil); err != nil {
		return fmt.Errorf("start firmware upgrade: %w", err)
	}
	return nil
}

// GetConfig reads the automatic update settings as the router sends them;
// their schema is unknown.
func GetConfig(ctx context.Context, c *amplifi.Client) (map[string]any, error) {
	var cfg map[string]any
	if err := rpc.Value(ctx, c, amplifi.IfaceFwUpdate, "GetFwUpdateConfig", nil, &cfg); err != nil {
		return nil, fmt.Errorf("firmware update config: %w", err)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return cfg, nil
}

// SetConfig writes the automatic update settings. cfg should be the full
// object read with GetConfig, with changes applied. Sending it as a JSON
// argument is untested.
func SetConfig(ctx context.Context, c *amplifi.Client, cfg map[string]any) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceFwUpdate, "SetFwUpdateConfig", cfg, nil); err != nil {
		return fmt.Errorf("set firmware update config: %w", err)
	}
	return nil
}
