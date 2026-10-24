// Package state records what sc-deken installed. It is written atomically,
// so a crash never loses the last good copy, and changed under a lock, so
// two scdeken processes never undo each other's changes.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/shimpe/scdeken/internal/filelock"
	"github.com/shimpe/scdeken/internal/robust"
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

// Save writes the state atomically (see robust.WriteFile): readers see
// either the old or the new file, never half of one. Use Update to change
// the state another scdeken process may be changing too.
func (s *State) Save(path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state folder: %w", err)
	}
	if err := robust.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("save state %s: %w", path, err)
	}
	return nil
}

// lockWait is how long Update waits for another scdeken process to finish
// with the state file.
const lockWait = 30 * time.Second

// Update changes the state file at path while holding the lock file
// state.lock next to it: it reads the latest saved state, lets change
// modify it, saves it and returns it. So two scdeken processes that run at
// the same time never undo each other's changes, as long as each change
// touches only its own records.
func Update(path string, change func(*State)) (*State, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state folder: %w", err)
	}
	unlock, err := filelock.Lock(filepath.Join(dir, "state.lock"), lockWait)
	if err != nil {
		return nil, err
	}
	defer unlock()
	s, err := Load(path)
	if err != nil {
		return nil, err
	}
	change(s)
	if err := s.Save(path); err != nil {
		return nil, err
	}
	return s, nil
}
