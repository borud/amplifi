// Package firmware checks for and installs firmware upgrades across an
// AmpliFi mesh (the FwUpdate interface), reports their progress, and reads
// and writes the automatic update settings.
package firmware

import (
	"context"
	"fmt"
	"slices"

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

// Version identifies a firmware image.
type Version struct {
	Version  string     `json:"version"`
	Revision string     `json:"revision"`
	Status   string     `json:"status"`
	Code     StatusCode `json:"code"`
}

// NodeVersion is the firmware one unit runs and the upgrade available to it,
// if any.
type NodeVersion struct {
	Type    string  `json:"type"`
	Current Version `json:"current"`
	Upgrade Version `json:"upgrade"`

	// LastCheckTime is in Unix seconds.
	LastCheckTime int64 `json:"last_check_time"`
}

// HasUpgrade reports whether an upgrade other than the running firmware is
// available.
func (n NodeVersion) HasUpgrade() bool {
	return n.Upgrade.Version != "" && n.Upgrade.Version != n.Current.Version
}

// Versions is the mesh-wide firmware state: what each unit runs and what it
// can upgrade to. Nodes is keyed by unit MAC as the router sends it.
type Versions struct {
	Status string                 `json:"status"`
	Nodes  map[string]NodeVersion `json:"nodes"`
}

// Updates reports the firmware each unit in the mesh runs and any upgrade
// the router has found for it. It does not trigger a new check.
func Updates(ctx context.Context, c *amplifi.Client) (*Versions, error) {
	var v Versions
	if err := rpc.Value(ctx, c, amplifi.IfaceFwUpdate, "GetAvailableUpdates", nil, &v); err != nil {
		return nil, fmt.Errorf("firmware updates: %w", err)
	}
	if v.Nodes == nil {
		v.Nodes = map[string]NodeVersion{}
	}
	return &v, nil
}

// Progress is the progress of a firmware check or upgrade across the mesh
//. Status is one of "idle", "checking", "downloading",
// "started", "in_progress" or "up_to_date". Nodes is keyed by unit MAC as the
// router sends it.
type Progress struct {
	Status string                  `json:"status"`
	Nodes  map[string]NodeProgress `json:"nodes"`
}

// NodeProgress is one unit's part of a Progress.
type NodeProgress struct {
	Check CheckResult `json:"check_result"`
	Info  NodeVersion `json:"info_result"`
}

// CheckResult is the outcome of a unit's last update check.
type CheckResult struct {
	Code   StatusCode `json:"code"`
	Status string     `json:"status"`
}

// fwBusyStates are the Progress.Status values of a check or upgrade still in
// progress.
var fwBusyStates = []string{"checking", "downloading", "started", "in_progress"}

// Busy reports whether a check, download or upgrade is in progress.
func (p *Progress) Busy() bool {
	return slices.Contains(fwBusyStates, p.Status)
}

// Status reports the progress of a firmware check or upgrade.
func Status(ctx context.Context, c *amplifi.Client) (*Progress, error) {
	var p Progress
	if err := rpc.Value(ctx, c, amplifi.IfaceFwUpdate, "GetFwUpdateStatus", nil, &p); err != nil {
		return nil, fmt.Errorf("firmware status: %w", err)
	}
	if p.Nodes == nil {
		p.Nodes = map[string]NodeProgress{}
	}
	return &p, nil
}

// CheckForUpdate asks the router to look for new firmware for every unit in
// the mesh. The check runs in the background; follow it with Status and read
// the result with Updates.
func CheckForUpdate(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceFwUpdate, "CheckForFwUpdate", nil); err != nil {
		return fmt.Errorf("check for firmware update: %w", err)
	}
	return nil
}

// Upgrade starts upgrading every unit in the mesh that has an upgrade
// available. The units download, flash and reboot on their own; follow the
// progress with Status until the router goes away.
func Upgrade(ctx context.Context, c *amplifi.Client) error {
	if _, err := c.Call(ctx, amplifi.IfaceFwUpdate, "StartClusterUpgrade", nil); err != nil {
		return fmt.Errorf("start firmware upgrade: %w", err)
	}
	return nil
}

// UpgradeUnit upgrades only the unit this session is connected to, with the
// upgrade it has already found. The unit may drop the connection to flash
// and reboot, which counts as success; the session is unusable afterwards.
func UpgradeUnit(ctx context.Context, c *amplifi.Client) error {
	if err := c.CallDisconnecting(ctx, amplifi.IfaceDevice, "UpgradeFirmware", nil); err != nil {
		return fmt.Errorf("upgrade unit firmware: %w", err)
	}
	return nil
}

// Config reads the automatic update settings as the router sends them; their
// schema is unknown.
func Config(ctx context.Context, c *amplifi.Client) (map[string]any, error) {
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
// object read with Config, with changes applied. Sending it as a JSON
// argument is untested.
func SetConfig(ctx context.Context, c *amplifi.Client, cfg map[string]any) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceFwUpdate, "SetFwUpdateConfig", cfg, nil); err != nil {
		return fmt.Errorf("set firmware update config: %w", err)
	}
	return nil
}
