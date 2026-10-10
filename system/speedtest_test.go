package system

import "testing"

func TestDecodeSpeedTest(t *testing.T) {
	st, err := decodeSpeedTest(`{"test_state":3,"test_state_str":"TS_DL","progress":40,"latency":12,"speed_dl":95000,"speed_ul_iface":2000,"name":"Chicago","timestamp":1700000000}`)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != SpeedTestStateDownload || st.StateText != "TS_DL" || st.Progress != 40 || st.Latency != 12 ||
		st.Download != 95000 || st.UploadIface != 2000 || st.Name != "Chicago" || st.Time != 1700000000 {
		t.Fatalf("decoded %+v", st)
	}

	st, err = decodeSpeedTest(nil)
	if err != nil || st.State != SpeedTestStateIdle {
		t.Fatalf("nil: %+v, %v", st, err)
	}

	if _, err := decodeSpeedTest(int8(1)); err == nil {
		t.Fatal("expected an error for a non-string result")
	}
}
