// Package srp implements the client side of the AmpliFi SRP-6a login: the RFC
// 5054 2048-bit group with SHA-256 and AmpliFi's own x, client-evidence and
// server-evidence routines.
package srp

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// SRP-6a parameters: the RFC 5054 2048-bit group with SHA-256 and the fixed
// username "admin".
const (
	// User is the fixed SRP username the router expects.
	User              = "admin"
	pbkdf2Iterations  = 42000
	pbkdf2KeyLenBytes = 32
)

// g is the group generator.
var g = big.NewInt(2)

// n is the RFC 5054 2048-bit safe prime.
var n = mustHex(`
AC6BDB41 324A9A9B F166DE5E 1389582F AF72B665 1987EE07 FC319294 3DB56050
A37329CB B4A099ED 8193E075 7767A13D D52312AB 4B03310D CD7F48A9 DA04FD50
E8083969 EDB767B0 CF609517 9A163AB3 661A05FB D5FAAAE8 2918A996 2F0B93B8
55F97993 EC975EEA A80D740A DBF4FF74 7359D041 D5C33EA7 1D281E44 6B14773B
CA97B43A 23FB8016 76BD207A 436C6481 F1D2B907 8717461A 5B9D32E6 88F87748
544523B5 24B0D57D 5EA77A27 75D2ECFA 032CFBDB F52FB378 61602790 04E57AE6
AF874E73 03CE5329 9CCC041C 7BC308D8 2A5698F3 A8D0C382 71AE35F8 E9DBFBB6
94B5C803 D89F7AE4 35DE236D 525F5475 9B65E372 FCD68EF2 0FA7111F 9E4AFF73`)

// Result holds the values produced by the client side of the exchange.
type Result struct {
	A          *big.Int // client public value
	M1         *big.Int // client evidence, sent to the router
	M2         *big.Int // server evidence expected from the router
	SessionKey []byte   // raw libsodium secretstream key
	X          *big.Int // password hash; see StepX
}

// Step computes the client's response to the router's SRP challenge.
// salt and b are the "s" and "B" values from the router's first reply.
func Step(password string, salt, b *big.Int) (*Result, error) {
	return StepX(computeX(password, salt), salt, b)
}

// StepX is Step with a precomputed x. A router's salt is fixed, so x is
// constant for a given router and password, and can stand in for the
// password. salt must still be the
// one from the challenge since it feeds the client evidence.
func StepX(x, salt, b *big.Int) (*Result, error) {
	a, err := randExp()
	if err != nil {
		return nil, err
	}
	return stepWithA(x, salt, b, a)
}

// stepWithA is StepX with the client's private ephemeral a.
func stepWithA(x, salt, b, a *big.Int) (*Result, error) {
	if new(big.Int).Mod(b, n).Sign() == 0 {
		return nil, errors.New("router returned invalid B")
	}

	bigA := new(big.Int).Exp(g, a, n)

	u := hashInt(padN(bigA), padN(b))
	if u.Sign() == 0 {
		return nil, errors.New("u is zero")
	}

	k := hashInt(padN(n), padN(g))

	// S = (B - k * g^x) ^ (a + u*x) mod N
	base := new(big.Int).Sub(b, new(big.Int).Mul(k, new(big.Int).Exp(g, x, n)))
	base.Mod(base, n)
	exp := new(big.Int).Add(a, new(big.Int).Mul(u, x))
	s := new(big.Int).Exp(base, exp, n)

	hs := sha256Sum(unsigned(s))

	m1 := clientEvidence(bigA, b, salt, s)
	return &Result{
		A:          bigA,
		M1:         m1,
		M2:         serverEvidence(bigA, m1, s),
		SessionKey: sessionKey(hs),
		X:          x,
	}, nil
}

// computeX computes the password-derived x:
//
//	dk = PBKDF2-HMAC-SHA256(password, salt, 42000, 32)
//	x  = SHA256( salt || SHA256(user || ":" || dk) )
func computeX(password string, salt *big.Int) *big.Int {
	saltBytes := unsigned(salt)
	dk := pbkdf2.Key([]byte(password), saltBytes, pbkdf2Iterations, pbkdf2KeyLenBytes, sha256.New)
	inner := sha256Sum([]byte(User), []byte(":"), dk)
	return new(big.Int).SetBytes(sha256Sum(saltBytes, inner))
}

// clientEvidence computes the client evidence message:
//
//	M1 = SHA256( (H(N) XOR H(g)) || H(user) || salt || A || B || H(S) )
func clientEvidence(a, b, salt, s *big.Int) *big.Int {
	hn := sha256Sum(unsigned(n))
	hg := sha256Sum(unsigned(g))
	xorNG := xorBytes(hn, hg)
	hUser := sha256Sum([]byte(User))
	hs := sha256Sum(unsigned(s))
	return new(big.Int).SetBytes(sha256Sum(xorNG, hUser, unsigned(salt), unsigned(a), unsigned(b), hs))
}

// serverEvidence computes the server evidence message:
//
//	M2 = SHA256( A || M1 || H(S) )
func serverEvidence(a, m1, s *big.Int) *big.Int {
	hs := sha256Sum(unsigned(s))
	return new(big.Int).SetBytes(sha256Sum(unsigned(a), unsigned(m1), hs))
}

// sessionKey returns the secretstream key: the raw 32 byte H(S), with no
// sign byte.
func sessionKey(hs []byte) []byte {
	return hs[:32]
}

// randExp returns a random private exponent in [1, N-1].
func randExp() (*big.Int, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("read random: %w", err)
	}
	a := new(big.Int).SetBytes(buf)
	a.Mod(a, new(big.Int).Sub(n, big.NewInt(1)))
	a.Add(a, big.NewInt(1))
	return a, nil
}

// unsigned returns the minimal big-endian unsigned encoding of x. The x,
// client-evidence and server-evidence routines use this unpadded form.
func unsigned(x *big.Int) []byte { return x.Bytes() }

// padN left-pads x with zeros to the byte length of N. The k and u values pad
// both operands to the modulus length before hashing; the evidence routines
// do not pad.
func padN(x *big.Int) []byte {
	l := (n.BitLen() + 7) / 8
	b := x.Bytes()
	if len(b) >= l {
		return b
	}
	out := make([]byte, l)
	copy(out[l-len(b):], b)
	return out
}

func hashInt(parts ...[]byte) *big.Int { return new(big.Int).SetBytes(sha256Sum(parts...)) }

func sha256Sum(parts ...[]byte) []byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}

func mustHex(s string) *big.Int {
	v, ok := new(big.Int).SetString(strings.Join(strings.Fields(s), ""), 16)
	if !ok {
		panic("srp: bad hex prime")
	}
	return v
}
