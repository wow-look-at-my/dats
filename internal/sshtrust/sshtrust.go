// Package sshtrust records which .dats files the operator has allowed to run
// their commands on which ssh target.
//
// A file naming a host is the first thing in the format that WIDENS what a
// run reaches: opening an unfamiliar suite would otherwise dial out using the
// reader's keys and agent. An approval is that consent, written down once and
// revocable, rather than a flag that blanket-permits every file in a run.
//
// The pair is keyed on the file's path, not its contents. A suite under
// development changes every few seconds, and re-approving on each edit would
// make the feature unusable. So an approval says "this file may reach this
// host", never "these commands may". That is the honest scope of it.
package sshtrust

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// formatVersion is bumped only for a breaking change to the file's shape.
const formatVersion = 1

// Entry is one approved pair.
type Entry struct {
	// File is the absolute, symlink-resolved path of the .dats file.
	File string `json:"file"`
	// Target is the ssh target that file may reach.
	Target string `json:"target"`
}

// Store is the on-disk approval list.
type Store struct {
	FormatVersion int     `json:"format_version"`
	Approvals     []Entry `json:"approvals"`

	path string
}

// Path is where the store lives: $XDG_CONFIG_HOME/dats/ssh-trust.json, else
// ~/.config/dats/ssh-trust.json.
func Path() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("ssh trust: locating the config directory: %w", err)
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "dats", "ssh-trust.json"), nil
}

// Load reads the store. A missing file is an empty store, not an error: no
// approvals yet is the normal starting state.
func Load() (*Store, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	store := &Store{FormatVersion: formatVersion, path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ssh trust: reading %s: %w", path, err)
	}
	// A corrupt store must not read as "nothing is approved": that would
	// silently drop approvals the operator made, and the fix is to say so.
	if err := json.Unmarshal(data, store); err != nil {
		return nil, fmt.Errorf("ssh trust: %s is not valid JSON: %w", path, err)
	}
	store.path = path
	return store, nil
}

// Key normalizes a .dats path into the form approvals are stored under.
func Key(datsPath string) (string, error) {
	abs, err := filepath.Abs(datsPath)
	if err != nil {
		return "", fmt.Errorf("ssh trust: resolving %s: %w", datsPath, err)
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	return abs, nil
}

// Approved reports whether this file may reach this target.
func (s *Store) Approved(datsPath, target string) (bool, error) {
	key, err := Key(datsPath)
	if err != nil {
		return false, err
	}
	for _, entry := range s.Approvals {
		if entry.File == key && entry.Target == target {
			return true, nil
		}
	}
	return false, nil
}

// Approve records a pair and writes the store. Approving twice is a no-op.
func (s *Store) Approve(datsPath, target string) error {
	key, err := Key(datsPath)
	if err != nil {
		return err
	}
	for _, entry := range s.Approvals {
		if entry.File == key && entry.Target == target {
			return nil
		}
	}
	s.Approvals = append(s.Approvals, Entry{File: key, Target: target})
	return s.save()
}

// Revoke removes a pair and writes the store, reporting whether it existed.
func (s *Store) Revoke(datsPath, target string) (bool, error) {
	key, err := Key(datsPath)
	if err != nil {
		return false, err
	}
	kept := make([]Entry, 0, len(s.Approvals))
	for _, entry := range s.Approvals {
		if entry.File == key && entry.Target == target {
			continue
		}
		kept = append(kept, entry)
	}
	if len(kept) == len(s.Approvals) {
		return false, nil
	}
	s.Approvals = kept
	return true, s.save()
}

// List returns the approvals in a stable order.
func (s *Store) List() []Entry {
	out := append([]Entry(nil), s.Approvals...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// save writes the store through a temp file and a rename, so an interrupted
// write cannot leave a half-written approval list behind.
func (s *Store) save() error {
	s.FormatVersion = formatVersion
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("ssh trust: creating %s: %w", filepath.Dir(s.path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("ssh trust: encoding: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "ssh-trust-*.json")
	if err != nil {
		return fmt.Errorf("ssh trust: creating a temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("ssh trust: writing: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("ssh trust: writing: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("ssh trust: setting permissions: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("ssh trust: saving %s: %w", s.path, err)
	}
	return nil
}
