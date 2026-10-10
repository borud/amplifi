package system

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

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

// SpeedTest is the state and result of the unit's internet speed test. The
// WAN rates are what the WAN interface carried during the test, including
// other traffic.
type SpeedTest struct {
	State           SpeedTestState  `json:"state"`
	StateText       string          `json:"state_text"`
	Result          SpeedTestResult `json:"result"`
	ResultText      string          `json:"result_text"`
	Progress        int             `json:"progress"`
	Latency         time.Duration   `json:"latency"`
	DownloadKbps    int             `json:"download_kbps"`
	WANDownloadKbps int             `json:"wan_download_kbps"`
	UploadKbps      int             `json:"upload_kbps"`
	WANUploadKbps   int             `json:"wan_upload_kbps"`
	Time            time.Time       `json:"time,omitzero"`
	ServerID        string          `json:"server_id"`
	Name            string          `json:"name"`
	Sponsor         string          `json:"sponsor"`
	City            string          `json:"city"`
	Country         string          `json:"country"`
	CountryCode     string          `json:"country_code"`
	Provider        string          `json:"provider"`
	URL             string          `json:"url"`
	DownloadURL     string          `json:"download_url"`
}

// MarshalJSON renders Latency in seconds.
func (st SpeedTest) MarshalJSON() ([]byte, error) {
	type speedTest SpeedTest
	return json.Marshal(struct {
		speedTest
		Latency float64 `json:"latency"`
	}{speedTest(st), st.Latency.Seconds()})
}

// speedTestEntry is a speed test as the unit sends it: rates in kbit/s,
// latency in milliseconds and the time in Unix seconds.
type speedTestEntry struct {
	State         SpeedTestState  `json:"test_state"`
	StateText     string          `json:"test_state_str"`
	Result        SpeedTestResult `json:"test_result"`
	ResultText    string          `json:"test_result_str"`
	Progress      int             `json:"progress"`
	Latency       int             `json:"latency"`
	Download      int             `json:"speed_dl"`
	DownloadIface int             `json:"speed_dl_iface"`
	Upload        int             `json:"speed_ul"`
	UploadIface   int             `json:"speed_ul_iface"`
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

func (e speedTestEntry) speedTest() SpeedTest {
	st := SpeedTest{
		State:           e.State,
		StateText:       e.StateText,
		Result:          e.Result,
		ResultText:      e.ResultText,
		Progress:        e.Progress,
		Latency:         time.Duration(e.Latency) * time.Millisecond,
		DownloadKbps:    e.Download,
		WANDownloadKbps: e.DownloadIface,
		UploadKbps:      e.Upload,
		WANUploadKbps:   e.UploadIface,
		ServerID:        e.ServerID,
		Name:            e.Name,
		Sponsor:         e.Sponsor,
		City:            e.City,
		Country:         e.Country,
		CountryCode:     e.CountryCode,
		Provider:        e.Provider,
		URL:             e.URL,
		DownloadURL:     e.DownloadURL,
	}
	if e.Time > 0 {
		st.Time = time.Unix(e.Time, 0)
	}
	return st
}

// StartSpeedTest starts the unit's internet speed test. Follow it with
// GetSpeedTest until the state returns to SpeedTestStateIdle.
func StartSpeedTest(ctx context.Context, c *amplifi.Client) error {
	if err := rpc.Set(ctx, c, amplifi.IfaceDevice, "SpeedTestRun", nil); err != nil {
		return fmt.Errorf("start speed test: %w", err)
	}
	return nil
}

// GetSpeedTest reports the progress of a running speed test, or the result
// of the last one.
func GetSpeedTest(ctx context.Context, c *amplifi.Client) (*SpeedTest, error) {
	v, err := c.Call(ctx, amplifi.IfaceDevice, "SpeedTestGetStatus", nil)
	if err != nil {
		return nil, fmt.Errorf("speed test: %w", err)
	}
	return decodeSpeedTest(v)
}

// decodeSpeedTest decodes a SpeedTestGetStatus result, a msgpack string
// holding JSON. A nil result is an empty (idle) test.
func decodeSpeedTest(v any) (*SpeedTest, error) {
	if v == nil {
		return &SpeedTest{}, nil
	}
	if _, ok := v.(string); !ok {
		return nil, fmt.Errorf("speed test: expected a JSON string, got %T", v)
	}
	var e speedTestEntry
	if err := rpc.Decode("SpeedTestGetStatus", v, &e); err != nil {
		return nil, fmt.Errorf("speed test: %w", err)
	}
	st := e.speedTest()
	return &st, nil
}

// speedTestHistoryArg is the SpeedTest argument asking for past results. Its
// "action" key follows the HomeKit method.
var speedTestHistoryArg = map[string]any{"action": "get_history"}

// SpeedTestHistory lists past speed test results, oldest first as the unit
// returns them. The request format is untested.
func SpeedTestHistory(ctx context.Context, c *amplifi.Client) ([]SpeedTest, error) {
	var res struct {
		Entries []speedTestEntry `json:"entries"`
	}
	if err := rpc.JSON(ctx, c, amplifi.IfaceDevice, "SpeedTest", speedTestHistoryArg, &res); err != nil {
		return nil, fmt.Errorf("speed test history: %w", err)
	}
	out := make([]SpeedTest, 0, len(res.Entries))
	for _, e := range res.Entries {
		out = append(out, e.speedTest())
	}
	return out, nil
}
