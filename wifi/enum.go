package wifi

import (
	"strings"

	"github.com/borud/amplifi/internal/rpc"
)

// Encryption is a network's encryption.
type Encryption int

// Encryption codes.
const (
	EncryptionNone     Encryption = 1
	EncryptionWEP      Encryption = 2
	EncryptionWPA      Encryption = 3
	EncryptionWPA2     Encryption = 4
	EncryptionWPA2WPA3 Encryption = 5
	EncryptionWPA3     Encryption = 6
)

var encryptionNames = map[string]string{
	"1": "none", "2": "wep", "3": "wpa", "4": "wpa2", "5": "wpa2-wpa3", "6": "wpa3",
}

// encryptionAliases are the hostapd names, accepted when parsing.
var encryptionAliases = map[string]Encryption{
	"psk": EncryptionWPA, "psk2": EncryptionWPA2, "sae-mixed": EncryptionWPA2WPA3, "sae": EncryptionWPA3,
}

// String returns the encryption's name, or "#N" for an unknown code.
func (e Encryption) String() string { return rpc.CodeName(encryptionNames, int64(e)) }

// MarshalText renders the encryption by name.
func (e Encryption) MarshalText() ([]byte, error) { return []byte(e.String()), nil }

// ParseEncryption parses an encryption name: none, wep, wpa, wpa2,
// wpa2-wpa3 or wpa3 (or the hostapd names psk, psk2, sae-mixed, sae).
func ParseEncryption(s string) (Encryption, error) {
	s = strings.TrimSpace(s)
	if e, ok := encryptionAliases[strings.ToLower(s)]; ok {
		return e, nil
	}
	n, err := rpc.Code("encryption", encryptionNames, s)
	return Encryption(n), err
}

// PMF is the 802.11w (protected management frames) mode. The codes follow
// hostapd's ieee80211w setting; they are untested.
type PMF int

// PMF modes.
const (
	PMFOff      PMF = 0
	PMFOptional PMF = 1
	PMFRequired PMF = 2
)

var pmfNames = map[string]string{"0": "off", "1": "optional", "2": "required"}

// String returns the mode's name, or "#N" for an unknown code.
func (p PMF) String() string { return rpc.CodeName(pmfNames, int64(p)) }

// MarshalText renders the mode by name.
func (p PMF) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// ParsePMF parses an 802.11w mode: off (or disabled), optional or required.
func ParsePMF(s string) (PMF, error) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "disabled") {
		return PMFOff, nil
	}
	n, err := rpc.Code("802.11w mode", pmfNames, s)
	return PMF(n), err
}

// Bandwidth is a radio's channel width.
type Bandwidth int

// Bandwidth codes.
const (
	BandwidthMax      Bandwidth = 1
	Bandwidth20       Bandwidth = 2
	Bandwidth40       Bandwidth = 3
	Bandwidth80       Bandwidth = 4
	Bandwidth80Plus80 Bandwidth = 5
	Bandwidth160      Bandwidth = 6
)

var bandwidthNames = map[string]string{"1": "max", "2": "20", "3": "40", "4": "80", "5": "80+80", "6": "160"}

// String returns the width in MHz or "max", or "#N" for an unknown code.
func (b Bandwidth) String() string { return rpc.CodeName(bandwidthNames, int64(b)) }

// MarshalText renders the width by name.
func (b Bandwidth) MarshalText() ([]byte, error) { return []byte(b.String()), nil }

// ParseBandwidth parses a channel width in MHz (20, 40, 80, 80+80, 160) or
// max. A "MHz" suffix is accepted.
func ParseBandwidth(s string) (Bandwidth, error) {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), "mhz")
	n, err := rpc.Code("bandwidth", bandwidthNames, s)
	return Bandwidth(n), err
}

// modeNames and opModeNames map WifiMode and WifiOperationMode codes.
var (
	modeNames   = map[string]string{"1": "a", "2": "b", "3": "g", "4": "n", "5": "ac", "6": "ax"}
	opModeNames = map[string]string{"1": "ap", "2": "sta", "3": "adhoc", "4": "wds", "5": "monitor", "6": "mesh"}
)

// bandCodes maps band names accepted when parsing to WifiBand codes.
var bandCodes = map[string]int64{"2.4": 1, "2.4ghz": 1, "2g": 1, "5": 2, "5ghz": 2, "5g": 2}
