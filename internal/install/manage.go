package install

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/scenv"
	"github.com/sc-deken/sc-deken/internal/state"
	"github.com/sc-deken/sc-deken/internal/version"
)

// Where an installed package came from.
const (
	SourceManaged  = "managed"  // installed by sc-deken
	SourceQuark    = "quark"    // installed as a quark by SuperCollider
	SourceExternal = "external" // put there by hand, only recorded by sc-deken
)

// minCommitPrefix is the shortest commit prefix that identifies a release.
const minCommitPrefix = 7

// Item is one installed package as shown in the Installed and Updates views.
type Item struct {
	Name            string             `json:"name"`
	Version         string             `json:"version"`
	Commit          string             `json:"commit"`
	Kind            string             `json:"kind"`
	Path            string             `json:"path"`
	Source          string             `json:"source"` // "managed" | "quark" | "external"
	Trust           string             `json:"trust,omitempty"`
	Latest          string             `json:"latest,omitempty"`
	UpdateAvailable bool               `json:"update_available"`
	Advisory        *advisory.Advisory `json:"advisory,omitempty"`

	repo string // where this copy came from; "" when unknown
}

// List returns everything sc-deken knows is installed: its own records and
// the quarks SuperCollider reports, sorted by name.
func (s *Service) List() []Item {
	var items []Item
	for _, e := range s.State.Packages {
		items = append(items, s.entryItem(e))
	}
	for _, q := range s.Ctx.Quarks {
		items = append(items, s.quarkItem(q))
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
	return items
}

// Updates returns the installed packages that have a newer version.
func (s *Service) Updates() []Item {
	var out []Item
	for _, it := range s.List() {
		if it.UpdateAvailable {
			out = append(out, it)
		}
	}
	return out
}

// entryItem describes a package recorded in the state file.
func (s *Service) entryItem(e state.Entry) Item {
	it := Item{
		Name: e.Name, Version: e.Version, Commit: e.Commit,
		Kind: e.Kind, Path: e.Path, Source: SourceManaged, repo: e.Repo,
	}
	if e.Route == state.External {
		it.Source = SourceExternal
	}
	pkg, ok := s.Index.Find(e.Name)
	if !ok && e.Repo != "" {
		pkg, _ = s.Index.FindRepo(e.Repo)
	}
	s.describe(&it, pkg)
	return it
}

// quarkItem describes a quark reported by SuperCollider.
func (s *Service) quarkItem(q scenv.Quark) Item {
	it := Item{Name: q.Name, Commit: q.Commit, Kind: "quark", Path: q.Path, Source: SourceQuark, repo: q.URL}
	pkg, ok := s.Index.FindRepo(q.URL)
	if ok {
		it.Version = releaseForCommit(pkg, q.Commit)
	}
	s.describe(&it, pkg)
	return it
}

// describe adds what the index knows: trust, newest version, update flag and
// the worst advisory. pkg may be nil for packages the index does not list.
func (s *Service) describe(it *Item, pkg *index.Package) {
	if pkg == nil {
		return
	}
	it.Trust = string(pkg.Trust)
	if it.Kind == "" {
		it.Kind = string(pkg.Kind)
	}
	if latest, ok := s.Index.Latest(pkg); ok {
		it.Latest = latest.Version
		it.UpdateAvailable = newer(latest.Version, it.Version)
	}
	rel := &index.Release{Version: it.Version, Commit: it.Commit}
	if adv, ok := s.Index.Retraction(pkg, rel); ok {
		it.Advisory = adv
	}
}

// newer reports whether candidate is a later version than installed. An
// unknown or unreadable version is never older.
func newer(candidate, installed string) bool {
	if installed == "" {
		return false
	}
	c, err := version.Parse(candidate)
	if err != nil {
		return false
	}
	i, err := version.Parse(installed)
	if err != nil {
		return false
	}
	return c.Compare(i) > 0
}

// releaseForCommit returns the version of the release whose commit and the
// given commit agree up to the shorter one (at least 7 characters), or "".
func releaseForCommit(pkg *index.Package, commit string) string {
	for _, r := range pkg.Versions {
		if sameCommit(r.Commit, commit) {
			return r.Version
		}
	}
	return ""
}

func sameCommit(a, b string) bool {
	if len(a) < minCommitPrefix || len(b) < minCommitPrefix {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// Remove deletes a package that sc-deken installed. The folder is renamed
// first, so a file SuperCollider has open is noticed before anything is lost.
func (s *Service) Remove(name string) error {
	entry, err := s.removable(name)
	if err != nil {
		return err
	}
	removing := filepath.Join(filepath.Dir(entry.Path), ".removing-"+filepath.Base(entry.Path))
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("clear leftover %s: %w", removing, err)
	}
	if err := rename(entry.Path, removing); err != nil && !os.IsNotExist(err) {
		return moveError("remove", entry.Name, err)
	}
	if err := s.forget(entry); err != nil {
		if rerr := rename(removing, entry.Path); rerr != nil {
			err = fmt.Errorf("%w (your copy is in %s: %v)", err, removing, rerr)
		}
		return err
	}
	if err := os.RemoveAll(removing); err != nil {
		return fmt.Errorf("%s was removed, but %s could not be deleted: %w", entry.Name, removing, err)
	}
	return nil
}

// removable finds the state entry for name and checks that sc-deken may
// delete it.
func (s *Service) removable(name string) (state.Entry, error) {
	entry, ok := s.State.Packages[strings.ToLower(name)]
	if !ok {
		for _, q := range s.Ctx.Quarks {
			if strings.EqualFold(q.Name, name) {
				return state.Entry{}, fmt.Errorf("remove quarks from SuperCollider (Deken.remove does this)")
			}
		}
		return state.Entry{}, fmt.Errorf("%s is not installed by sc-deken", name)
	}
	if entry.Route == state.External {
		return state.Entry{}, fmt.Errorf("%s was installed outside sc-deken; remove it by hand from %s", entry.Name, entry.Path)
	}
	if !inside(s.Paths.Managed, entry.Path) {
		return state.Entry{}, fmt.Errorf("%s is not in sc-deken's folder; remove it by hand from %s", entry.Name, entry.Path)
	}
	return entry, nil
}

// forget drops entry from the state file; memory is restored on failure.
func (s *Service) forget(entry state.Entry) error {
	key := strings.ToLower(entry.Name)
	delete(s.State.Packages, key)
	if err := s.State.Save(s.Paths.State); err != nil {
		s.State.Packages[key] = entry
		return err
	}
	return nil
}

// Quarantine moves the folder or file at path into the quarantine folder
// for advisory advisoryID and returns where it went. Nothing is deleted.
func (s *Service) Quarantine(path, advisoryID string) (string, error) {
	if !advisory.ValidID(advisoryID) {
		return "", fmt.Errorf("not a valid advisory id: %q", advisoryID)
	}
	base := filepath.Base(path)
	dir := filepath.Join(s.Paths.Quarantine, advisoryID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create quarantine folder %s: %w", dir, err)
	}
	dst := freePath(filepath.Join(dir, base))
	if err := moveTree(path, dst); err != nil {
		return "", moveError("move", base, err)
	}
	if err := s.forgetPath(path); err != nil {
		return dst, fmt.Errorf("%s was moved to %s, but the install record could not be saved: %w", base, dst, err)
	}
	return dst, nil
}

// forgetPath drops every state entry installed at path and saves.
func (s *Service) forgetPath(path string) error {
	var dropped []state.Entry
	for key, e := range s.State.Packages {
		if samePath(e.Path, path) {
			dropped = append(dropped, e)
			delete(s.State.Packages, key)
		}
	}
	if len(dropped) == 0 {
		return nil
	}
	if err := s.State.Save(s.Paths.State); err != nil {
		for _, e := range dropped {
			s.State.Packages[strings.ToLower(e.Name)] = e
		}
		return err
	}
	return nil
}

// samePath compares paths, ignoring case on Windows.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if platform.HostOS() == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// ClearQuarantine permanently deletes everything in quarantine. Callers
// must ask the user first.
func (s *Service) ClearQuarantine() error {
	if err := os.RemoveAll(s.Paths.Quarantine); err != nil {
		return fmt.Errorf("clear quarantine %s: %w", s.Paths.Quarantine, err)
	}
	return nil
}
