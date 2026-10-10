//go:build sodium

// Package sodiumref wraps libsodium's crypto_secretstream_xchacha20poly1305
// via cgo. It exists only as a reference to cross-check the pure Go
// implementation in the parent package; build with -tags sodium.
package sodiumref

/*
#cgo darwin CFLAGS: -I/opt/homebrew/include -I/usr/local/include
#cgo darwin LDFLAGS: -L/opt/homebrew/lib -L/usr/local/lib -lsodium
#cgo linux LDFLAGS: -lsodium
#include <sodium.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const (
	headerBytes = 24
	aBytes      = 17
)

func init() {
	if C.sodium_init() < 0 {
		panic("sodium_init failed")
	}
}

// Pusher encrypts a stream.
type Pusher struct {
	st C.crypto_secretstream_xchacha20poly1305_state
}

// NewPusher starts an encrypting stream and returns it with its header.
func NewPusher(key []byte) (*Pusher, []byte) {
	p := &Pusher{}
	header := make([]byte, headerBytes)
	C.crypto_secretstream_xchacha20poly1305_init_push(&p.st, ptr(header), ptr(key))
	return p, header
}

// Push encrypts one message.
func (p *Pusher) Push(m, ad []byte, tag byte) []byte {
	out := make([]byte, len(m)+aBytes)
	C.crypto_secretstream_xchacha20poly1305_push(&p.st, ptr(out), nil,
		ptr(m), C.ulonglong(len(m)), ptr(ad), C.ulonglong(len(ad)), C.uchar(tag))
	return out
}

// Puller decrypts a stream.
type Puller struct {
	st C.crypto_secretstream_xchacha20poly1305_state
}

// NewPuller starts a decrypting stream from its header.
func NewPuller(key, header []byte) *Puller {
	p := &Puller{}
	C.crypto_secretstream_xchacha20poly1305_init_pull(&p.st, ptr(header), ptr(key))
	return p
}

// Pull decrypts one message.
func (p *Puller) Pull(c, ad []byte) ([]byte, byte, error) {
	m := make([]byte, len(c)-aBytes)
	var tag C.uchar
	if C.crypto_secretstream_xchacha20poly1305_pull(&p.st, ptr(m), nil, &tag,
		ptr(c), C.ulonglong(len(c)), ptr(ad), C.ulonglong(len(ad))) != 0 {
		return nil, 0, fmt.Errorf("pull failed")
	}
	return m, byte(tag), nil
}

// ptr returns a pointer usable by cgo even for an empty slice.
func ptr(b []byte) *C.uchar {
	if len(b) == 0 {
		return (*C.uchar)(unsafe.Pointer(&zero))
	}
	return (*C.uchar)(&b[0])
}

var zero byte
