package creds

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadFind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "amplifi", fileName)

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Put(Unit{Name: "router", IP: "192.0.2.10", MAC: "00:00:5e:00:53:0c", Hash: []byte{1, 2, 3}, Saved: time.Now()})
	s.Put(Unit{Name: "deimos", IP: "192.0.2.11", MAC: "00:00:5e:00:53:0d", Hash: []byte{4}})
	deimos, _ := s.Find("deimos")
	s.SetDefault(deimos)
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	assertPerm(t, filepath.Dir(path), 0o700)
	assertPerm(t, path, 0o600)

	s, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"router", "ROUTER", "192.0.2.10", "00:00:5e:00:53:0c"} {
		u, ok := s.Find(key)
		if !ok || string(u.Hash) != "\x01\x02\x03" {
			t.Fatalf("Find(%q) = %+v, %v", key, u, ok)
		}
	}
	if _, ok := s.Find("nosuchunit"); ok {
		t.Fatal("found unknown unit")
	}
	if u, _ := s.DefaultUnit(); u.Name != "deimos" {
		t.Fatalf("default after reload = %q, want deimos", u.Name)
	}
}

func TestFingerprintRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	const fp = "SHA256:47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU"

	s, _ := Load(path)
	s.Put(Unit{Name: "router", IP: "192.0.2.10", Fingerprint: fp})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if u, _ := s.Find("router"); u.Fingerprint != fp {
		t.Fatalf("fingerprint = %q, want %q", u.Fingerprint, fp)
	}
}

func TestLoadWithoutFingerprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	old := `{"units": [{"name": "router", "ip": "192.0.2.10", "hash": "AQID", "saved": "2026-01-02T03:04:05Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := s.Find("router")
	if !ok || string(u.Hash) != "\x01\x02\x03" || u.Fingerprint != "" {
		t.Fatalf("got %+v, %v", u, ok)
	}
}

func TestLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileName)
	if err := os.WriteFile(path, []byte(`{"units": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("corrupt file loaded without error")
	}
}

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != want {
		t.Fatalf("%s mode = %v, want %v", path, fi.Mode().Perm(), want)
	}
}

func TestPutReplacesSameUnitOrIP(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), fileName))

	s.Put(Unit{Name: "router", IP: "192.0.2.10", MAC: "aa"})
	s.Put(Unit{Name: "deimos", IP: "192.0.2.11", MAC: "bb"})
	s.Put(Unit{Name: "router", IP: "192.0.2.20", MAC: "aa"}) // router moved
	s.Put(Unit{Name: "office", IP: "192.0.2.11", MAC: "cc"}) // took deimos' IP

	if len(s.Units) != 2 {
		t.Fatalf("got %d units, want 2: %+v", len(s.Units), s.Units)
	}
	if u, _ := s.Find("router"); u.IP != "192.0.2.20" {
		t.Fatalf("router IP = %q", u.IP)
	}
	if _, ok := s.Find("deimos"); ok {
		t.Fatal("stale deimos entry kept")
	}
}

func TestRemove(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), fileName))
	s.Put(Unit{Name: "router", IP: "192.0.2.10"})

	if !s.Remove("router") {
		t.Fatal("Remove returned false")
	}
	if s.Remove("router") {
		t.Fatal("second Remove returned true")
	}
}

func TestDefaultPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	p, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if p != "/tmp/xdg/amplifi/credentials.json" {
		t.Fatalf("got %q", p)
	}
}

func TestDefaultUnit(t *testing.T) {
	s, _ := Load(filepath.Join(t.TempDir(), fileName))
	if _, ok := s.DefaultUnit(); ok {
		t.Fatal("empty store has a default")
	}

	s.Put(Unit{Name: "router", IP: "192.0.2.10", MAC: "aa"})
	s.Put(Unit{Name: "deimos", IP: "192.0.2.11", MAC: "bb"})
	if u, _ := s.DefaultUnit(); u.Name != "router" {
		t.Fatalf("default = %q, want the first unit saved", u.Name)
	}

	// Logging in again to the first unit must not lose its place.
	s.Put(Unit{Name: "router", IP: "192.0.2.10", MAC: "aa", Hash: []byte{1}})
	if u, _ := s.DefaultUnit(); u.Name != "router" {
		t.Fatalf("default after re-login = %q", u.Name)
	}

	deimos, _ := s.Find("deimos")
	s.SetDefault(deimos)
	if u, _ := s.DefaultUnit(); u.Name != "deimos" || !s.IsDefault(deimos) {
		t.Fatalf("default = %q, want deimos", u.Name)
	}

	s.Remove("deimos")
	if u, _ := s.DefaultUnit(); u.Name != "router" {
		t.Fatalf("default after removing it = %q, want router", u.Name)
	}
}
