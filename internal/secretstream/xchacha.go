package secretstream

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/chacha20"
	//lint:ignore SA1019 libsodium's construction needs raw one-time Poly1305
	"golang.org/x/crypto/poly1305"
)

// This is a port of libsodium's crypto_secretstream_xchacha20poly1305 (from
// crypto_secretstream/xchacha20poly1305/secretstream_xchacha20poly1305.c),
// including additional data and rekeying, which the router's framing needs.
// The libsodium original is kept as a test reference in sodiumref.

const (
	keyBytes     = 32
	headerBytes  = 24
	aBytes       = 17 // tag byte + 16 byte MAC
	counterBytes = 4
	inonceBytes  = 8

	tagMessage = 0x00
	tagRekey   = 0x02
)

var pad0 [16]byte

// state is crypto_secretstream_xchacha20poly1305_state: a subkey and a 12
// byte ChaCha20 IETF nonce made of a 4 byte counter and an 8 byte inonce.
type state struct {
	k     [keyBytes]byte
	nonce [counterBytes + inonceBytes]byte
}

// initPush starts an encrypting stream and returns its random header.
func initPush(key []byte) (*state, []byte, error) {
	header := make([]byte, headerBytes)
	if _, err := rand.Read(header); err != nil {
		return nil, nil, fmt.Errorf("read random: %w", err)
	}
	st, err := initPull(key, header)
	if err != nil {
		return nil, nil, err
	}
	return st, header, nil
}

// initPull starts a decrypting stream from the peer's header.
func initPull(key, header []byte) (*state, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("key must be %d bytes, got %d", keyBytes, len(key))
	}
	if len(header) != headerBytes {
		return nil, fmt.Errorf("header must be %d bytes, got %d", headerBytes, len(header))
	}

	k, err := chacha20.HChaCha20(key, header[:16])
	if err != nil {
		return nil, fmt.Errorf("hchacha20: %w", err)
	}

	st := &state{}
	copy(st.k[:], k)
	st.resetCounter()
	copy(st.nonce[counterBytes:], header[16:])
	return st, nil
}

// push encrypts m with additional data ad and returns tag||ciphertext||mac.
func (st *state) push(m, ad []byte, tag byte) ([]byte, error) {
	cipher, mac, err := st.begin(ad)
	if err != nil {
		return nil, err
	}

	out := make([]byte, 1+len(m)+poly1305.TagSize)

	var block [64]byte
	block[0] = tag
	cipher.XORKeyStream(block[:], block[:])
	mac.Write(block[:])
	out[0] = block[0]

	c := out[1 : 1+len(m)]
	cipher.XORKeyStream(c, m)
	st.finish(mac, c, ad, out[1+len(m):])
	st.advance(tag)
	return out, nil
}

// pull authenticates and decrypts in (as produced by push) with additional
// data ad, returning the plaintext and its tag.
func (st *state) pull(in, ad []byte) ([]byte, byte, error) {
	if len(in) < aBytes {
		return nil, 0, fmt.Errorf("ciphertext too short")
	}
	mlen := len(in) - aBytes

	cipher, mac, err := st.begin(ad)
	if err != nil {
		return nil, 0, err
	}

	var block [64]byte
	block[0] = in[0]
	cipher.XORKeyStream(block[:], block[:])
	tag := block[0]
	block[0] = in[0]
	mac.Write(block[:])

	c := in[1 : 1+mlen]
	stored := in[1+mlen:]

	// finish advances the nonce, so verify against a copy of the MAC first.
	var sum [poly1305.TagSize]byte
	saved := st.nonce
	st.finish(mac, c, ad, sum[:])
	if subtle.ConstantTimeCompare(sum[:], stored) != 1 {
		st.nonce = saved
		return nil, 0, fmt.Errorf("authentication failed")
	}

	m := make([]byte, mlen)
	cipher.XORKeyStream(m, c)
	st.advance(tag)
	return m, tag, nil
}

// begin derives the one-time Poly1305 key from keystream block 0 and absorbs
// the padded additional data. The returned cipher is positioned at block 1.
func (st *state) begin(ad []byte) (*chacha20.Cipher, *poly1305.MAC, error) {
	cipher, err := chacha20.NewUnauthenticatedCipher(st.k[:], st.nonce[:])
	if err != nil {
		return nil, nil, fmt.Errorf("chacha20: %w", err)
	}

	var block [64]byte
	cipher.XORKeyStream(block[:], block[:])
	var polyKey [32]byte
	copy(polyKey[:], block[:32])
	mac := poly1305.New(&polyKey)

	mac.Write(ad)
	mac.Write(pad0[:(0x10-len(ad))&0xf])
	return cipher, mac, nil
}

// finish absorbs the ciphertext and lengths, writes the MAC to sum, and
// advances the stream state as libsodium does after each message.
func (st *state) finish(mac *poly1305.MAC, c, ad, sum []byte) {
	mac.Write(c)
	mac.Write(pad0[:(0x10-64+len(c))&0xf])

	var slen [8]byte
	binary.LittleEndian.PutUint64(slen[:], uint64(len(ad)))
	mac.Write(slen[:])
	binary.LittleEndian.PutUint64(slen[:], uint64(64+len(c)))
	mac.Write(slen[:])
	copy(sum, mac.Sum(nil))

	for i := range inonceBytes {
		st.nonce[counterBytes+i] ^= sum[i]
	}
	st.incrementCounter()
}

// advance applies the post-message rekey rule: rekey on an explicit rekey tag
// or when the counter wraps to zero.
func (st *state) advance(tag byte) {
	if tag&tagRekey != 0 || binary.LittleEndian.Uint32(st.nonce[:counterBytes]) == 0 {
		st.rekey()
	}
}

// rekey replaces the subkey and inonce with keystream derived from them.
func (st *state) rekey() {
	var buf [keyBytes + inonceBytes]byte
	copy(buf[:keyBytes], st.k[:])
	copy(buf[keyBytes:], st.nonce[counterBytes:])

	// Key and nonce sizes are fixed, so this cannot fail.
	cipher, _ := chacha20.NewUnauthenticatedCipher(st.k[:], st.nonce[:])
	cipher.XORKeyStream(buf[:], buf[:])

	copy(st.k[:], buf[:keyBytes])
	copy(st.nonce[counterBytes:], buf[keyBytes:])
	st.resetCounter()
}

func (st *state) resetCounter() {
	binary.LittleEndian.PutUint32(st.nonce[:counterBytes], 1)
}

func (st *state) incrementCounter() {
	n := binary.LittleEndian.Uint32(st.nonce[:counterBytes])
	binary.LittleEndian.PutUint32(st.nonce[:counterBytes], n+1)
}
