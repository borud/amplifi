package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/system"
)

type speedtestCmd struct {
	Run     speedtestRunCmd     `kong:"cmd,help='start an internet speed test on the unit'"`
	Status  speedtestStatusCmd  `kong:"cmd,help='show the running or last speed test'"`
	History speedtestHistoryCmd `kong:"cmd,help='list past speed test results (experimental)'"`
}

// speedtestPollInterval is how often --wait polls the test status.
const speedtestPollInterval = time.Second

// speedtestStartGrace is how long run --wait accepts an idle state before
// the test is seen running, as the unit can take a moment to start it.
const speedtestStartGrace = 15 * time.Second

type speedtestRunCmd struct {
	unitFlags   `kong:"embed"`
	Wait        bool          `kong:"help='wait for the test to finish and show the result'"`
	WaitTimeout time.Duration `kong:"help='maximum time to wait',default='3m'"`
}

// Run starts a speed test and, with --wait, follows it to the end.
func (c *speedtestRunCmd) Run(opt *Options) error {
	return c.session(opt, c.WaitTimeout, func(ctx context.Context, cl *amplifi.Client) error {
		if err := system.RunSpeedTest(ctx, cl); err != nil {
			return err
		}
		if !c.Wait {
			fmt.Fprintln(os.Stderr, "speed test started; see 'amplifi speedtest status'")
			return nil
		}

		st, err := speedtestWait(ctx, cl, !opt.JSON())
		if err != nil {
			return err
		}
		return speedtestPrint(opt, st)
	})
}

// speedtestWait polls the test until it is back to idle after having run,
// printing progress to stderr when progress is set. An idle test that was
// never seen running is accepted after speedtestStartGrace.
func speedtestWait(ctx context.Context, cl *amplifi.Client, progress bool) (*system.SpeedTest, error) {
	start := time.Now()
	seen := false
	for {
		st, err := system.SpeedTestStatus(ctx, cl)
		if err != nil {
			return nil, err
		}

		switch {
		case st.State != system.SpeedTestStateIdle:
			seen = true
			if progress {
				fmt.Fprintf(os.Stderr, "\r%-10s %3d%%", speedtestStateName(st.State), st.Progress)
			}
		case seen:
			if progress {
				fmt.Fprintln(os.Stderr)
			}
			return st, nil
		case time.Since(start) > speedtestStartGrace:
			return st, nil
		}

		if err := sleepCtx(ctx, speedtestPollInterval); err != nil {
			return nil, fmt.Errorf("gave up waiting: %w", err)
		}
	}
}

type speedtestStatusCmd struct {
	unitFlags `kong:"embed"`
}

// Run shows the current or last speed test.
func (c *speedtestStatusCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		st, err := system.SpeedTestStatus(ctx, cl)
		if err != nil {
			return err
		}
		return speedtestPrint(opt, st)
	})
}

func speedtestPrint(opt *Options, st *system.SpeedTest) error {
	if opt.JSON() {
		return printJSON(st)
	}
	return speedtestWrite(os.Stdout, st)
}

func speedtestWrite(w io.Writer, st *system.SpeedTest) error {
	state := speedtestStateName(st.State)
	if st.State != system.SpeedTestStateIdle {
		state += fmt.Sprintf(" (%d%%)", st.Progress)
	}

	rows := [][2]string{
		{"state", state},
		{"result", speedtestResultName(st.Result)},
	}
	if st.Time > 0 {
		rows = append(rows, [2]string{"time", unixTime(st.Time)})
	}
	if server := speedtestServer(st); server != "" {
		rows = append(rows, [2]string{"server", server})
	}
	rows = append(rows,
		[2]string{"latency", strconv.Itoa(st.Latency) + " ms"},
		[2]string{"download", mbit(st.Download) + "  (WAN total " + mbit(st.DownloadIface) + ")"},
		[2]string{"upload", mbit(st.Upload) + "  (WAN total " + mbit(st.UploadIface) + ")"},
	)
	return printPairs(w, rows)
}

type speedtestHistoryCmd struct {
	unitFlags `kong:"embed"`
}

// Run lists past speed tests.
func (c *speedtestHistoryCmd) Run(opt *Options) error {
	return c.session(opt, 0, func(ctx context.Context, cl *amplifi.Client) error {
		hist, err := system.SpeedTestHistory(ctx, cl)
		if err != nil {
			return err
		}
		if opt.JSON() {
			return printJSON(hist)
		}

		tw := newTable(os.Stdout)
		fmt.Fprintln(tw, "TIME\tRESULT\tLATENCY\tDOWNLOAD\tUPLOAD\tSERVER")
		for _, st := range hist {
			fmt.Fprintf(tw, "%s\t%s\t%d ms\t%s\t%s\t%s\n", unixTime(st.Time),
				speedtestResultName(st.Result), st.Latency, mbit(st.Download), mbit(st.Upload), speedtestServer(&st))
		}
		return tw.Flush()
	})
}

func speedtestServer(st *system.SpeedTest) string {
	s := st.Sponsor
	if s == "" {
		s = st.Provider
	}
	if st.Name != "" {
		s += " (" + st.Name + ")"
	}
	return s
}

// speedtestStates and speedtestResults name the speed test state and result
// codes.
var (
	speedtestStates = map[system.SpeedTestState]string{
		system.SpeedTestStateIdle:     "idle",
		system.SpeedTestStateGetInfo:  "preparing",
		system.SpeedTestStatePing:     "ping",
		system.SpeedTestStateDownload: "download",
		system.SpeedTestStateUpload:   "upload",
	}
	speedtestResults = map[system.SpeedTestResult]string{
		system.SpeedTestResultNone:    "none",
		system.SpeedTestResultSuccess: "success",
		system.SpeedTestResultFailure: "failure",
	}
)

func speedtestStateName(s system.SpeedTestState) string {
	if n, ok := speedtestStates[s]; ok {
		return n
	}
	return fmt.Sprintf("state %d", s)
}

func speedtestResultName(r system.SpeedTestResult) string {
	if n, ok := speedtestResults[r]; ok {
		return n
	}
	return fmt.Sprintf("result %d", r)
}
