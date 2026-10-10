package system

import (
	"context"
	"fmt"
	"strconv"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// iperfSeconds is how long the unit runs the iperf server.
const iperfSeconds = 30

// StartIperf starts an iperf server (iperf -s) on the unit for 30 seconds,
// so a client on the LAN can measure throughput to it. A port of 0 uses the
// iperf default.
func StartIperf(ctx context.Context, c *amplifi.Client, port int) error {
	args := []any{"-s"}
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}

	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "RunIperf", map[string]any{"timeout": iperfSeconds, "args": args}, nil); err != nil {
		return fmt.Errorf("start iperf: %w", err)
	}
	return nil
}
