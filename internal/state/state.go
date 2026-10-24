// Package state records what sc-deken installed. It is written atomically so
// a crash never loses the last good copy.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Route is how a package got onto the machine.
type Route string

// The ways a package can be installed.
const (
	Prebuilt   Route = "prebuilt"
	LocalBuild Route = "local-build"
	External   Route = "external"
)

// Entry describes one installed package.
type Entry struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Commit      string    `json:"commit"`
	Repo        string    `json:"repo"`
	Platform    string    `json:"platform"`
	Path        string    `json:"path"`
	Kind        string    `json:"kind"`
	Route       Route     `json:"route"`
	InstalledAt time.Time `json:"installed_at"`
}

// State is the whole install record.
type State struct {
	Packages       map[string]Entry `json:"packages"`        // key: lowercase name
	KeptAdvisories []string         `json:"kept_advisories"` // advisory IDs the user chose to keep
}

// Load reads the state file. A missing file gives an empty state; a corrupt
// one is an error so the user's records are never silently reset.
func Load(path string) (*State, error) {
	s := &State{}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		s.Packages = map[string]Entry{}
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state %s: %w", path, err)
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", path, err)
	}
	if s.Packages == nil {
		s.Packages = map[string]Entry{}
	}
	return s, nil
}

// Save writes the state to path+".tmp", flushes it to disk, then renames it
// over path, so readers see either the old or the new file, never half of one.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state folder: %w", err)
	}
	tmp := path + ".tmp"
	if err := writeSynced(tmp, data); err != nil {
		return fmt.Errorf("write state %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state %s: %w", path, err)
	}
	return nil
}

// writeSynced writes data to a fresh file and fsyncs it before closing.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
