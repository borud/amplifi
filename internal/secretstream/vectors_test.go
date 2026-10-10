package secretstream

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

// The vector below was produced by libsodium (TestPrintVector, -tags sodium)
// with key 0x42 * 32 and ad {0x01}. Pulling and pushing it pins the port to
// libsodium without needing cgo.
var (
	vectorMessages = []string{"", "hello", `{"type":"call","iface":"com.ubnt.UnifiHome.Device","seqId":2,"method":"GetSystemBoardInfo","payload":{"msgpack":""}}`, "after rekey"}
	vectorTags     = []byte{tagMessage, tagMessage, tagRekey, tagMessage}
	vectorHeader   = "c03cc43291a53d7cd3e19809da0cb5e6ed7487fc43ffe7dc"
	vectorFrames   = []string{
		"343e63e1bb4ab263c843d759179d038c5d",
		"cda07c102b202f7d618fed16cb5242772c597510fb1b",
		"ba4e2be204c718ba3f5152f0752a963266721d1fafcacbb37a7066a010ea5f04386dc965a1d6564a9836f9c4bb7eccf94c5c03b30158979dd2ae73f88756b9ca1a946ab2e6799858100e1bc360ffd77130493a7058e6ed1a366f0b854dc205aeb724b1ac861b4070e611e395b343ad4816112502992ebca644c8569fa1fac228e060038d06",
		"f90d65ce358dfa8d2facaef075a98d9b733fc2de1d85fcb3c594b30f",
	}
)

func TestLibsodiumVector(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, keyBytes)
	st, err := initPull(key, mustHex(t, vectorHeader))
	if err != nil {
		t.Fatal(err)
	}

	for i, f := range vectorFrames {
		m, tag, err := st.pull(mustHex(t, f), ad)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if string(m) != vectorMessages[i] || tag != vectorTags[i] {
			t.Fatalf("frame %d: got %q tag %d", i, m, tag)
		}
	}
}

func TestStreamRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, keyBytes)
	tx, _ := New(key)
	rx, _ := New(key)

	for _, m := range vectorMessages {
		frame, err := tx.Seal([]byte(m))
		if err != nil {
			t.Fatal(err)
		}
		got, err := rx.Open(frame)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != m {
			t.Fatalf("got %q, want %q", got, m)
		}
	}
}

func TestLibsodiumVectorPush(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, keyBytes)
	st, err := initPull(key, mustHex(t, vectorHeader))
	if err != nil {
		t.Fatal(err)
	}

	for i, m := range vectorMessages {
		c, err := st.push([]byte(m), ad, vectorTags[i])
		if err != nil {
			t.Fatal(err)
		}
		if got := hex.EncodeToString(c); got != vectorFrames[i] {
			t.Fatalf("frame %d:\n got %s\nwant %s", i, got, vectorFrames[i])
		}
	}
}

func TestTamperedFrameRejected(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, keyBytes)
	tx, _ := New(key)
	rx, _ := New(key)

	for i, m := range []string{"hello", "world", "again"} {
		frame, err := tx.Seal([]byte(m))
		if err != nil {
			t.Fatal(err)
		}

		// The sealed message is the encrypted tag byte, the ciphertext and
		// the MAC, at the end of the frame.
		start := len(frame) - aBytes - len(m)
		tamper := map[string]int{
			"tag byte":   start,
			"ciphertext": start + 2,
			"mac":        len(frame) - 1,
		}
		for name, j := range tamper {
			bad := bytes.Clone(frame)
			bad[j] ^= 1
			if _, err := rx.Open(bad); err == nil {
				t.Fatalf("frame %d: tampered %s accepted", i, name)
			}
		}

		got, err := rx.Open(frame)
		if err != nil || string(got) != m {
			t.Fatalf("frame %d: valid frame after tampering: %q, %v", i, got, err)
		}
	}
}

// TestCounterWrap checks that push and pull stay in step across the counter
// wrapping to zero, which forces a rekey.
func TestCounterWrap(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, keyBytes)
	header := mustHex(t, vectorHeader)
	tx, _ := initPull(key, header)
	rx, _ := initPull(key, header)
	binary.LittleEndian.PutUint32(tx.nonce[:counterBytes], 0xffffffff)
	binary.LittleEndian.PutUint32(rx.nonce[:counterBytes], 0xffffffff)
	keyBefore := tx.k

	for _, m := range vectorMessages {
		c, err := tx.push([]byte(m), ad, tagMessage)
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := rx.pull(c, ad)
		if err != nil || string(got) != m {
			t.Fatalf("got %q, %v, want %q", got, err, m)
		}
	}
	if tx.k == keyBefore {
		t.Fatal("no rekey after counter wrap")
	}
}

func TestOpenRejectsMalformedFrames(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, keyBytes)
	tx, _ := New(key)
	frame, _ := tx.Seal([]byte("hello"))

	badVersion := bytes.Clone(frame)
	badVersion[0] = 0x02

	tests := map[string][]byte{
		"empty":            nil,
		"bad version":      badVersion,
		"short header":     frame[:1+headerBytes-1],
		"short ciphertext": frame[:1+headerBytes+aBytes-1],
	}
	for name, f := range tests {
		rx, _ := New(key)
		if _, err := rx.Open(f); err == nil {
			t.Errorf("%s: frame accepted", name)
		}
	}
}

func TestNewRejectsShortKey(t *testing.T) {
	if _, err := New(make([]byte, keyBytes-1)); err == nil {
		t.Fatal("short key accepted")
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
