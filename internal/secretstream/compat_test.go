//go:build sodium

package secretstream

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/borud/amplifi/internal/secretstream/sodiumref"
)

// These tests cross-check the pure Go port against libsodium in both
// directions. Run with: go test -tags sodium ./internal/secretstream

func TestCompatGoToSodium(t *testing.T) {
	key := randBytes(t, keyBytes)
	st, header, err := initPush(key)
	if err != nil {
		t.Fatal(err)
	}
	ref := sodiumref.NewPuller(key, header)

	for i, m := range compatMessages() {
		a := compatAD(i)
		tag := compatTag(i)
		c, err := st.push(m, a, tag)
		if err != nil {
			t.Fatal(err)
		}
		got, gotTag, err := ref.Pull(c, a)
		if err != nil {
			t.Fatalf("message %d: libsodium rejected Go ciphertext: %v", i, err)
		}
		if !bytes.Equal(got, m) || gotTag != tag {
			t.Fatalf("message %d: plaintext or tag mismatch", i)
		}
	}
}

func TestCompatSodiumToGo(t *testing.T) {
	key := randBytes(t, keyBytes)
	ref, header := sodiumref.NewPusher(key)
	st, err := initPull(key, header)
	if err != nil {
		t.Fatal(err)
	}

	for i, m := range compatMessages() {
		a := compatAD(i)
		tag := compatTag(i)
		got, gotTag, err := st.pull(ref.Push(m, a, tag), a)
		if err != nil {
			t.Fatalf("message %d: Go rejected libsodium ciphertext: %v", i, err)
		}
		if !bytes.Equal(got, m) || gotTag != tag {
			t.Fatalf("message %d: plaintext or tag mismatch", i)
		}
	}
}

// TestPrintVector emits a libsodium-produced stream for vectors_test.go.
func TestPrintVector(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, keyBytes)
	ref, header := sodiumref.NewPusher(key)
	fmt.Printf("header: %x\n", header)
	for i, m := range vectorMessages {
		fmt.Printf("frame %d: %x\n", i, ref.Push([]byte(m), ad, vectorTags[i]))
	}
}

// compatMessages covers empty, sub-block, block-boundary and multi-block
// lengths, enough to exercise padding and nonce chaining.
func compatMessages() [][]byte {
	sizes := []int{0, 1, 15, 16, 17, 63, 64, 65, 200, 1000, 4096}
	msgs := make([][]byte, 0, len(sizes)*3)
	for range 3 {
		for _, n := range sizes {
			b := make([]byte, n)
			_, _ = rand.Read(b)
			msgs = append(msgs, b)
		}
	}
	return msgs
}

func compatAD(i int) []byte {
	switch i % 3 {
	case 0:
		return ad
	case 1:
		return nil
	default:
		return bytes.Repeat([]byte{byte(i)}, i%40)
	}
}

// compatTag sends a rekey tag now and then so the rekey path is compared too.
func compatTag(i int) byte {
	if i%7 == 6 {
		return tagRekey
	}
	return tagMessage
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
