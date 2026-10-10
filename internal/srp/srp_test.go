package srp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"
)

const testPassword = "correct horse battery staple"

var testSalt = big.NewInt(0x0badf00ddeadbeef)

// TestComputeX pins x for a fixed password and salt.
func TestComputeX(t *testing.T) {
	want := mustHex("cfff2201b9187dabfdb5e7958c456152f38c673db3639e03a0979d6bf35b86eb")
	if got := computeX(testPassword, testSalt); got.Cmp(want) != 0 {
		t.Fatalf("x = %x, want %x", got, want)
	}
}

// TestStepKnownAnswer pins A, M1, M2 and the session key for fixed inputs.
// The second case has an S whose minimal encoding is one byte shorter than
// N, which catches hashing a padded S.
func TestStepKnownAnswer(t *testing.T) {
	baseA := mustHex("9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08")
	tests := []struct {
		name    string
		a       *big.Int
		sLen    int
		wantA   string
		wantM1  string
		wantM2  string
		wantKey string
	}{
		{
			name: "full length S",
			a:    baseA,
			sLen: 256,
			wantA: `
			2d8a0ddc6600fdcea0e0a0ce363304e06550225651fac28e3cf2396e43354b6d
			e7fa07be487910cb101c4e5222ecc9eb5d342dc95841d4bd2b97f5f40f3741d8
			6df47f8bd5b905b676ee4a5989d023a332385168650dc3517b69056c902e316e
			3a130a1ffc9adf249b11dd668f3271db0c3125d0d3589354b1812a4eaf42356e
			b69879ad45e234b2d409185c788dc938f454153b7509b045291f122712e7bb43
			7fba21751088a3c14f7ee163c7d89f5fca22e68a98cfad4a5b56d715d4519ca0
			b61e02933a2c924f2b86b76f92f1ca7ed511e6d09ed1df2629692794d239d8b7
			2280a1c9496cb3e014375c39d571964a472ff3abb81db85e49aa7eefd45f1420`,
			wantM1:  "9674a89fed95b1f170ee75d5df8284555e63e1196836eac9c542622b2a11bf69",
			wantM2:  "bbde15955811d2aeffe781949a6a7985ffa863bec1f666adf9bb46e156d9cfe5",
			wantKey: "57c17c2ffe7ad65b941d45293bce8f55c0d82aaa4ce271792add50dccda487ee",
		},
		{
			name: "S with leading zero byte",
			a:    new(big.Int).Add(baseA, big.NewInt(32)),
			sLen: 255,
			wantA: `
			a34345f35e81801f82c02a5c26bad526ea957345a76c9ecb1c481e7e8ced8c85
			3648ac12669f8e8c46ad483dfcc1184772e2c770fb462ec55d2b23a1aa9a0a55
			ea190e8d9a2828e1b5716037a084f4af767f6fba9f2416dcd0c0d8a78fe706c4
			129a09029d9276afaaee99db50002979f2d04c551a61b29fe1cfae01efcba3ff
			d4a6e3bde7ad7f7d56a9919c24c5c17df6e73e4336f1b9b94a81c916d5e6eb31
			185511fda5dd36810825c95ec7639ca70032f4733b6e7f8f2fb1aeeee788ebb7
			61c2e8b8c0149275284fa68e8c12302847d707c0138973b78b76a857212f9bfd
			44ba0b1af9c076c29d2b37b9227c5347fed2a6f2667d60695de21afc6c71712f`,
			wantM1:  "acb6bc03a026be59a91adedcee8d2b079aa2c96c2ebf9338b6e2faeb964ae280",
			wantM2:  "cb7ce8dc221e9370271717d2acd090c2e04ba0291334d6dfc1e78e48a310bbf7",
			wantKey: "ebe1212765a91dbfcb1b4d31d52402b31ea4ce60100e2ec72e97b687af10f70d",
		},
	}

	srv := newServer(testPassword, testSalt, big.NewInt(0x5eed))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := stepWithA(computeX(testPassword, testSalt), testSalt, srv.bPub, tt.a)
			if err != nil {
				t.Fatal(err)
			}
			if l := len(srv.secret(res.A).Bytes()); l != tt.sLen {
				t.Fatalf("S is %d bytes, want %d", l, tt.sLen)
			}
			if res.A.Cmp(mustHex(tt.wantA)) != 0 {
				t.Errorf("A = %x", res.A)
			}
			if res.M1.Cmp(mustHex(tt.wantM1)) != 0 {
				t.Errorf("M1 = %x", res.M1)
			}
			if res.M2.Cmp(mustHex(tt.wantM2)) != 0 {
				t.Errorf("M2 = %x", res.M2)
			}
			if got := hex.EncodeToString(res.SessionKey); got != tt.wantKey {
				t.Errorf("session key = %s", got)
			}
		})
	}
}

// TestSRPRoundTrip plays the server side of the exchange against Step with a
// random a and checks that both sides agree on S and the evidence values.
func TestSRPRoundTrip(t *testing.T) {
	b, err := randExp()
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(testPassword, testSalt, b)

	res, err := Step(testPassword, testSalt, srv.bPub)
	if err != nil {
		t.Fatal(err)
	}

	s := srv.secret(res.A)
	if want := clientEvidence(res.A, srv.bPub, testSalt, s); res.M1.Cmp(want) != 0 {
		t.Fatalf("client evidence mismatch:\n got %x\nwant %x", res.M1, want)
	}
	if want := serverEvidence(res.A, res.M1, s); res.M2.Cmp(want) != 0 {
		t.Fatalf("server evidence mismatch:\n got %x\nwant %x", res.M2, want)
	}
	if hs := sha256.Sum256(s.Bytes()); !bytes.Equal(res.SessionKey, hs[:]) {
		t.Fatal("session key not derived from the server's S")
	}
}

// TestStepXMatchesPassword checks that Result.X is the password's x and that
// StepX accepts it in place of the password.
func TestStepXMatchesPassword(t *testing.T) {
	srv := newServer(testPassword, testSalt, big.NewInt(0x5eed))

	res, err := Step(testPassword, testSalt, srv.bPub)
	if err != nil {
		t.Fatal(err)
	}
	if res.X.Cmp(computeX(testPassword, testSalt)) != 0 {
		t.Fatal("Result.X is not the password's x")
	}

	resX, err := StepX(res.X, testSalt, srv.bPub)
	if err != nil {
		t.Fatal(err)
	}
	s := srv.secret(resX.A)
	if resX.M1.Cmp(clientEvidence(resX.A, srv.bPub, testSalt, s)) != 0 {
		t.Fatal("StepX evidence does not match the server")
	}
}

func TestStepXRejectsBadB(t *testing.T) {
	x := computeX(testPassword, testSalt)
	for _, b := range []*big.Int{big.NewInt(0), n, new(big.Int).Lsh(n, 1)} {
		if _, err := StepX(x, testSalt, b); err == nil {
			t.Errorf("B = %x accepted", b)
		}
	}
}

// server is the router's side of the exchange.
type server struct {
	v    *big.Int
	b    *big.Int
	bPub *big.Int
}

// newServer derives the verifier v = g^x and B = k*v + g^b mod N.
func newServer(password string, salt, b *big.Int) server {
	v := new(big.Int).Exp(g, computeX(password, salt), n)
	k := hashInt(padN(n), padN(g))
	bPub := new(big.Int).Add(new(big.Int).Mul(k, v), new(big.Int).Exp(g, b, n))
	return server{v: v, b: b, bPub: bPub.Mod(bPub, n)}
}

// secret computes S = (A * v^u)^b mod N.
func (s server) secret(a *big.Int) *big.Int {
	u := hashInt(padN(a), padN(s.bPub))
	base := new(big.Int).Mul(a, new(big.Int).Exp(s.v, u, n))
	return new(big.Int).Exp(base.Mod(base, n), s.b, n)
}
