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

// RunIperf starts an iperf server (iperf -s) on the unit for 30 seconds, so
// a client on the LAN can measure throughput to it. A port of 0 uses the
// iperf default. The result is returned as the unit sends it.
func RunIperf(ctx context.Context, c *amplifi.Client, port int) (any, error) {
	args := []any{"-s"}
	if port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}

	var out any
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "RunIperf", map[string]any{"timeout": iperfSeconds, "args": args}, &out); err != nil {
		return nil, fmt.Errorf("run iperf: %w", err)
	}
	return out, nil
}
