package system

import (
	"context"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// ResetStatistics clears the unit's throughput statistics (perf) and/or
// speed test history (speedtest).
func ResetStatistics(ctx context.Context, c *amplifi.Client, perf, speedtest bool) error {
	what := make([]string, 0, 2)
	if perf {
		what = append(what, "perf")
	}
	if speedtest {
		what = append(what, "speedtest")
	}

	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "ResetStatistics", what, nil); err != nil {
		return fmt.Errorf("reset statistics: %w", err)
	}
	return nil
}
