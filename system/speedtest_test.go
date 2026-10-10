package system

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecodeSpeedTest(t *testing.T) {
	st, err := decodeSpeedTest(`{"test_state":3,"test_state_str":"TS_DL","progress":40,"latency":12,"speed_dl":95000,"speed_ul_iface":2000,"name":"Chicago","timestamp":1700000000}`)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != SpeedTestStateDownload || st.StateText != "TS_DL" || st.Progress != 40 || st.Latency != 12*time.Millisecond ||
		st.DownloadKbps != 95000 || st.WANUploadKbps != 2000 || st.Name != "Chicago" || !st.Time.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("decoded %+v", st)
	}

	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"latency":0.012`) {
		t.Fatalf("latency not in seconds: %s", b)
	}

	st, err = decodeSpeedTest(nil)
	if err != nil || st.State != SpeedTestStateIdle || !st.Time.IsZero() {
		t.Fatalf("nil: %+v, %v", st, err)
	}

	if _, err := decodeSpeedTest(int8(1)); err == nil {
		t.Fatal("expected an error for a non-string result")
	}
}
