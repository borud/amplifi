package amplifi

import (
	"encoding/json"
	"testing"
)

func TestMACJSON(t *testing.T) {
	m, err := ParseMAC("00:00:5e:00:53:0c")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(struct {
		M MAC `json:"m"`
		E MAC `json:"e,omitempty"`
	}{M: m})
	if err != nil || string(b) != `{"m":"00:00:5e:00:53:0c"}` {
		t.Fatalf("got %s, %v", b, err)
	}

	var back struct{ M MAC }
	if err := json.Unmarshal([]byte(`{"M":"00:00:5e:00:53:0c"}`), &back); err != nil || !back.M.Equal(m) {
		t.Fatalf("round trip: %v, %v", back.M, err)
	}
}

func TestParseMAC(t *testing.T) {
	valid := []string{"00:00:5e:00:53:0c", "00-00-5E-00-53-0C", "0000.5e00.530c"}
	for _, s := range valid {
		m, err := ParseMAC(s)
		if err != nil || m.String() != "00:00:5e:00:53:0c" {
			t.Errorf("ParseMAC(%q) = %v, %v", s, m, err)
		}
	}

	invalid := []string{
		"",
		"00:00:5e:00:53",
		"00:00:5e:00:53:0c:01",
		"00:00:5e:ff:fe:00:53:0c",
		"00:00:5e:00:53:zz",
		"0:0:5e:0:53:c",
	}
	for _, s := range invalid {
		if m, err := ParseMAC(s); err == nil {
			t.Errorf("ParseMAC(%q) = %v, want error", s, m)
		}
	}
}

func TestMACUnmarshalTextEmpty(t *testing.T) {
	m := MAC{1}
	if err := m.UnmarshalText(nil); err != nil || m != nil {
		t.Fatalf("got %v, %v", m, err)
	}
}
