// Package creds stores saved logins for the CLI so the admin password is
// entered once per unit. It stores the SRP password hash (see
// amplifi.Client.PasswordHash), never the password, plus the unit's name and
// address so it can be addressed by name, and its certificate fingerprint so
// a replaced unit is noticed.
package creds

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fileName is the store's file inside the config directory.
const fileName = "credentials.json"

// Unit is one saved login.
type Unit struct {
	Name  string    `json:"name,omitempty"`
	IP    string    `json:"ip"`
	MAC   string    `json:"mac,omitempty"`
	Hash  []byte    `json:"hash"`
	Saved time.Time `json:"saved"`

	// Fingerprint is the unit's certificate fingerprint (see
	// amplifi.Client.CertificateFingerprint), empty in files written before
	// it was recorded.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Store is the set of saved logins backed by a JSON file. Units are kept in
// the order they were first saved.
type Store struct {
	path string

	// Default is the MAC, or failing that the IP, of the default unit.
	Default string `json:"default,omitempty"`
	Units   []Unit `json:"units"`
}

// DefaultPath returns $XDG_CONFIG_HOME/amplifi/credentials.json, falling back
// to ~/.config/amplifi/credentials.json.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "amplifi", fileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "amplifi", fileName), nil
}

// Load reads the store at path. A missing file is an empty store.
func Load(path string) (*Store, error) {
	s := &Store{path: path, Units: []Unit{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials %q: %w", path, err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse credentials %q: %w", path, err)
	}
	return s, nil
}

// Find returns the unit whose name (case-insensitive), IP or MAC is key.
func (s *Store) Find(key string) (Unit, bool) {
	for _, u := range s.Units {
		if u.matches(key) {
			return u, true
		}
	}
	return Unit{}, false
}

// Put saves u, replacing any entry for the same unit (same MAC) or the same
// IP, since an address now held by u can no longer belong to another unit. A
// replaced entry keeps its place, so the first unit saved stays first.
func (s *Store) Put(u Unit) {
	kept := make([]Unit, 0, len(s.Units)+1)
	var placed bool
	for _, old := range s.Units {
		if old.IP != u.IP && (u.MAC == "" || !strings.EqualFold(old.MAC, u.MAC)) {
			kept = append(kept, old)
			continue
		}
		if !placed {
			kept = append(kept, u)
			placed = true
		}
	}
	if !placed {
		kept = append(kept, u)
	}
	s.Units = kept
}

// DefaultUnit returns the unit to use when none is named: the one chosen with
// SetDefault, or else the first unit saved.
func (s *Store) DefaultUnit() (Unit, bool) {
	if s.Default != "" {
		if u, ok := s.Find(s.Default); ok {
			return u, true
		}
	}
	if len(s.Units) == 0 {
		return Unit{}, false
	}
	return s.Units[0], true
}

// SetDefault makes u the default unit.
func (s *Store) SetDefault(u Unit) {
	s.Default = u.IP
	if u.MAC != "" {
		s.Default = u.MAC
	}
}

// IsDefault reports whether u is the default unit.
func (s *Store) IsDefault(u Unit) bool {
	d, ok := s.DefaultUnit()
	return ok && d.IP == u.IP
}

// Remove deletes the unit matching key and reports whether one was found.
// Removing the default unit makes the first remaining unit the default.
func (s *Store) Remove(key string) bool {
	for i, u := range s.Units {
		if !u.matches(key) {
			continue
		}
		if s.Default != "" && u.matches(s.Default) {
			s.Default = ""
		}
		s.Units = append(s.Units[:i], s.Units[i+1:]...)
		return true
	}
	return false
}

// Save writes the store atomically. The directory is created 0700 and the
// file 0600, as the hashes are password-equivalent.
func (s *Store) Save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	tmp, err := os.CreateTemp(dir, fileName+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}

// Path returns the file backing the store.
func (s *Store) Path() string { return s.path }

func (u Unit) matches(key string) bool {
	return strings.EqualFold(u.Name, key) || u.IP == key || (u.MAC != "" && strings.EqualFold(u.MAC, key))
}
