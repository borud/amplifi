package system

import (
	"context"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// ResetThroughputStats clears the unit's throughput statistics.
func ResetThroughputStats(ctx context.Context, c *amplifi.Client) error {
	return resetStatistics(ctx, c, "perf")
}

// ResetSpeedTestHistory clears the unit's speed test history.
func ResetSpeedTestHistory(ctx context.Context, c *amplifi.Client) error {
	return resetStatistics(ctx, c, "speedtest")
}

// resetStatistics clears one kind of statistics, "perf" or "speedtest".
func resetStatistics(ctx context.Context, c *amplifi.Client, what string) error {
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "ResetStatistics", []string{what}, nil); err != nil {
		return fmt.Errorf("reset %s statistics: %w", what, err)
	}
	return nil
}
