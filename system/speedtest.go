package system

import (
	"context"
	"fmt"

	"github.com/borud/amplifi"
	"github.com/borud/amplifi/internal/rpc"
)

// SpeedTestState is the phase of a speed test.
type SpeedTestState int

// Speed test states.
const (
	SpeedTestStateIdle     SpeedTestState = 0
	SpeedTestStateGetInfo  SpeedTestState = 1
	SpeedTestStatePing     SpeedTestState = 2
	SpeedTestStateDownload SpeedTestState = 3
	SpeedTestStateUpload   SpeedTestState = 4
)

// SpeedTestResult is the outcome of a speed test.
type SpeedTestResult int

// Speed test results.
const (
	SpeedTestResultNone    SpeedTestResult = 0
	SpeedTestResultSuccess SpeedTestResult = 1
	SpeedTestResultFailure SpeedTestResult = 2
)

// SpeedTest is the state and result of the unit's internet speed test.
// Speeds are in kbit/s. DownloadIface and UploadIface are what the WAN
// interface carried during the test, including other traffic. Latency is in milliseconds and Time is Unix
// seconds.
type SpeedTest struct {
	State         SpeedTestState  `json:"test_state"`
	StateText     string          `json:"test_state_str"`
	Result        SpeedTestResult `json:"test_result"`
	ResultText    string          `json:"test_result_str"`
	Progress      int             `json:"progress"`
	Latency       int             `json:"latency"`
	Download      int64           `json:"speed_dl"`
	DownloadIface int64           `json:"speed_dl_iface"`
	Upload        int64           `json:"speed_ul"`
	UploadIface   int64           `json:"speed_ul_iface"`
	Time          int64           `json:"timestamp"`
	ServerID      string          `json:"id"`
	Name          string          `json:"name"`
	Sponsor       string          `json:"sponsor"`
	City          string          `json:"city"`
	Country       string          `json:"country"`
	CountryCode   string          `json:"country_code"`
	Provider      string          `json:"provider"`
	URL           string          `json:"url"`
	DownloadURL   string          `json:"url_dl"`
}

// RunSpeedTest starts the unit's internet speed test. Follow it with
// SpeedTestStatus until the state returns to SpeedTestStateIdle.
func RunSpeedTest(ctx context.Context, c *amplifi.Client) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "SpeedTestRun", nil); err != nil {
		return fmt.Errorf("run speed test: %w", err)
	}
	return nil
}

// SpeedTestStatus reports the progress of a running speed test, or the
// result of the last one.
func SpeedTestStatus(ctx context.Context, c *amplifi.Client) (*SpeedTest, error) {
	v, err := c.Call(ctx, amplifi.IfaceDevice, "SpeedTestGetStatus", nil)
	if err != nil {
		return nil, fmt.Errorf("speed test status: %w", err)
	}
	return decodeSpeedTest(v)
}

// decodeSpeedTest decodes a SpeedTestGetStatus result, a msgpack string
// holding JSON. A nil result is an empty (idle) test.
func decodeSpeedTest(v any) (*SpeedTest, error) {
	st := &SpeedTest{}
	if v == nil {
		return st, nil
	}
	if _, ok := v.(string); !ok {
		return nil, fmt.Errorf("speed test status: expected a JSON string, got %T", v)
	}
	if err := rpc.Decode("SpeedTestGetStatus", v, st); err != nil {
		return nil, fmt.Errorf("speed test status: %w", err)
	}
	return st, nil
}

// speedTestHistoryArg is the SpeedTest argument asking for past results. Its
// "action" key follows the HomeKit method.
var speedTestHistoryArg = map[string]any{"action": "get_history"}

// SpeedTestHistory lists past speed test results, oldest first as the unit
// returns them. The request format is untested.
func SpeedTestHistory(ctx context.Context, c *amplifi.Client) ([]SpeedTest, error) {
	var res struct {
		Entries []SpeedTest `json:"entries"`
	}
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "SpeedTest", speedTestHistoryArg, &res); err != nil {
		return nil, fmt.Errorf("speed test history: %w", err)
	}
	if res.Entries == nil {
		return []SpeedTest{}, nil
	}
	return res.Entries, nil
}
