package system

import (
	"context"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// HomeKit is the state of the unit's Apple HomeKit service.
type HomeKit struct {
	Enabled     bool `json:"administratively_enabled"`
	Provisioned bool `json:"provisioned"`
	CanRun      bool `json:"service_can_potentially_run"`
	CanRunNow   bool `json:"service_can_run_now"`
}

// GetHomeKit reports the state of the HomeKit service. Current firmware
// refuses this method with a router error.
func GetHomeKit(ctx context.Context, c *amplifi.Client) (*HomeKit, error) {
	var h HomeKit
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "HomeKit", map[string]any{"action": "get_status"}, &h); err != nil {
		return nil, fmt.Errorf("homekit status: %w", err)
	}
	return &h, nil
}

// DisableHomeKit turns the HomeKit service off (Device.HomeKit with action
// manage_service).
func DisableHomeKit(ctx context.Context, c *amplifi.Client) error {
	arg := map[string]any{"action": "manage_service", "administratively_enabled": false}
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "HomeKit", arg, nil); err != nil {
		return fmt.Errorf("disable homekit: %w", err)
	}
	return nil
}
