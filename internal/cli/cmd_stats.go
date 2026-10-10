package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type systemStatsCmd struct {
	Reset systemStatsResetCmd `kong:"cmd,help='clear the throughput statistics, the speed test history or both'"`
}

type systemStatsResetCmd struct {
	unitFlags `kong:"embed"`
	yesFlag   `kong:"embed"`
	Perf      bool `kong:"help='clear only the throughput statistics'"`
	Speedtest bool `kong:"help='clear only the speed test history'"`
}

// Run clears the selected statistics; both when none is selected.
func (c *systemStatsResetCmd) Run(opt *Options) error {
	perf, speedtest := c.Perf, c.Speedtest
	if !perf && !speedtest {
		perf, speedtest = true, true
	}
	what := make([]string, 0, 2)
	if perf {
		what = append(what, "throughput statistics")
	}
	if speedtest {
		what = append(what, "speed test history")
	}

	if !c.approve(fmt.Sprintf("clear the %s on %s?", strings.Join(what, " and "), c.Host)) {
		return nil
	}

	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		if err := system.ResetStatistics(ctx, cl, perf, speedtest); err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(map[string][]string{"cleared": what})
		}
		fmt.Printf("cleared %s\n", strings.Join(what, " and "))
		return nil
	})
}
