// Package secretstream frames and encrypts AmpliFi control messages with
// crypto_secretstream_xchacha20poly1305, byte-compatible with libsodium.
package secretstream

import (
	"errors"
	"fmt"
)

// Every post-authentication frame is:
//
//	byte 0        version, always 0x01
//	[24 bytes]    secretstream header, present only on the first frame
//	rest          ciphertext (plaintext length + 17)
//
// Each message uses additional data = the single version byte {0x01} and tag
// TAG_MESSAGE (0).
const versionByte = 0x01

// ad is the additional data authenticated with every message.
var ad = []byte{versionByte}

// Stream holds the push (send) and pull (receive) states of one session. Each
// direction is initialised lazily from the first frame sent or received.
type Stream struct {
	key  []byte
	push *state
	pull *state
}

// New returns a Stream keyed with the SRP-derived session key. Only the first
// 32 bytes of key are used.
func New(key []byte) (*Stream, error) {
	if len(key) < keyBytes {
		return nil, fmt.Errorf("secretstream: session key too short: %d bytes", len(key))
	}
	return &Stream{key: key[:keyBytes]}, nil
}

// Seal frames and encrypts one plaintext message.
func (s *Stream) Seal(plaintext []byte) ([]byte, error) {
	out := []byte{versionByte}

	if s.push == nil {
		st, header, err := initPush(s.key)
		if err != nil {
			return nil, fmt.Errorf("secretstream: %w", err)
		}
		s.push = st
		out = append(out, header...)
	}

	ct, err := s.push.push(plaintext, ad, tagMessage)
	if err != nil {
		return nil, fmt.Errorf("secretstream: %w", err)
	}
	return append(out, ct...), nil
}

// Open decrypts one framed message. A frame that fails authentication leaves
// the stream unchanged.
func (s *Stream) Open(frame []byte) ([]byte, error) {
	if len(frame) == 0 {
		return nil, errors.New("secretstream: empty frame")
	}
	if frame[0] != versionByte {
		return nil, fmt.Errorf("secretstream: bad frame version 0x%02x", frame[0])
	}
	body := frame[1:]

	st := s.pull
	if st == nil {
		if len(body) < headerBytes {
			return nil, fmt.Errorf("secretstream: first frame too short for header: %d bytes", len(body))
		}
		var err error
		st, err = initPull(s.key, body[:headerBytes])
		if err != nil {
			return nil, fmt.Errorf("secretstream: %w", err)
		}
		body = body[headerBytes:]
	}

	pt, tag, err := st.pull(body, ad)
	if err != nil {
		return nil, fmt.Errorf("secretstream: %w (bad key or corrupt frame)", err)
	}
	s.pull = st
	if tag != tagMessage {
		return nil, fmt.Errorf("secretstream: unexpected tag %d", tag)
	}
	return pt, nil
}
