package install

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/state"
	"github.com/shimpe/scdeken/internal/version"
)

// AdvisoryHit says that an advisory affects an installed package.
type AdvisoryHit struct {
	Advisory     advisory.Advisory `json:"advisory"`
	Item         Item              `json:"item"`
	FixedVersion string            `json:"fixed_version,omitempty"` // newest unaffected release newer than installed
}

// CheckAdvisories finds installed packages, including copies put there by
// hand, that an advisory withdraws. Advisories the user chose to keep are
// left out.
func (s *Service) CheckAdvisories() []AdvisoryHit {
	advs := s.unkeptAdvisories()
	items := s.List()
	items = append(items, s.externalCopies(advs, items)...)
	var inst []advisory.Installed
	for _, it := range items {
		inst = append(inst, advisory.Installed{
			Name: it.Name, Repo: s.repoOf(it), Version: it.Version,
			Commit: it.Commit, Path: it.Path, Source: it.Source,
		})
	}
	var hits []AdvisoryHit
	for _, h := range advisory.Match(advs, inst) {
		it := findItem(items, h.Installed.Path, h.Installed.Name)
		hits = append(hits, AdvisoryHit{
			Advisory: h.Advisory, Item: it,
			FixedVersion: s.fixedVersion(it),
		})
	}
	return hits
}

// findItem returns the item with the given path and name.
func findItem(items []Item, path, name string) Item {
	for _, it := range items {
		if it.Path == path && it.Name == name {
			return it
		}
	}
	return Item{}
}

// unkeptAdvisories are the index advisories the user has not chosen to keep.
func (s *Service) unkeptAdvisories() []advisory.Advisory {
	kept := map[string]bool{}
	for _, id := range s.State.KeptAdvisories {
		kept[id] = true
	}
	var out []advisory.Advisory
	for _, a := range s.Index.Advisories {
		if !kept[a.ID] {
			out = append(out, a)
		}
	}
	return out
}

// externalCopies scans the extension folders for copies of the packages the
// advisories talk about that are not already in known.
func (s *Service) externalCopies(advs []advisory.Advisory, known []Item) []Item {
	seen := map[string]bool{}
	for _, it := range known {
		seen[strings.ToLower(filepath.Clean(it.Path))] = true
	}
	var out []Item
	scanned := map[string]bool{}
	for _, a := range advs {
		pkg, ok := s.advisoryPackage(a)
		if !ok || scanned[pkg.Name] {
			continue
		}
		scanned[pkg.Name] = true
		dups, _ := s.findDuplicates(pkg) // the scan itself never fails
		for _, d := range dups {
			key := strings.ToLower(filepath.Clean(d.Path))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Item{
				Name: pkg.Name, Kind: string(pkg.Kind), Path: d.Path,
				Source: SourceExternal, Trust: string(pkg.Trust), repo: pkg.Repo,
			})
		}
	}
	return out
}

// advisoryPackage finds the indexed package an advisory is about.
func (s *Service) advisoryPackage(a advisory.Advisory) (*index.Package, bool) {
	if a.Package != "" {
		if pkg, ok := s.Index.Find(a.Package); ok {
			return pkg, true
		}
	}
	if a.Repo != "" {
		return s.Index.FindRepo(a.Repo)
	}
	return nil, false
}

// Keep remembers that the user wants to keep the packages advisoryID
// withdraws, so CheckAdvisories stops reporting it.
func (s *Service) Keep(advisoryID string) error {
	err := s.updateState(func(st *state.State) {
		if !slices.Contains(st.KeptAdvisories, advisoryID) {
			st.KeptAdvisories = append(st.KeptAdvisories, advisoryID)
		}
	})
	if err != nil {
		return fmt.Errorf("remember kept advisory %s: %w", advisoryID, err)
	}
	return nil
}

// repoOf is the repository an installed copy came from, "" when unknown.
// Only a copy with no recorded origin is looked up by name, so a quark
// that merely shares a name with an indexed package is not taken for it.
func (s *Service) repoOf(it Item) string {
	if it.repo != "" {
		return it.repo
	}
	if pkg, ok := s.Index.Find(it.Name); ok {
		return pkg.Repo
	}
	return ""
}

// packageOf finds the indexed package an installed copy belongs to.
func (s *Service) packageOf(it Item) (*index.Package, bool) {
	if repo := s.repoOf(it); repo != "" {
		return s.Index.FindRepo(repo)
	}
	return nil, false
}

// fixedVersion is the newest release newer than the installed one that no
// advisory withdraws; "" when there is none or the installed version is
// unknown. Stable releases are preferred over unstable ones.
func (s *Service) fixedVersion(it Item) string {
	pkg, ok := s.packageOf(it)
	if !ok || it.Version == "" {
		return ""
	}
	var stable, unstable string
	for i := range pkg.Versions {
		r := &pkg.Versions[i]
		if _, withdrawn := s.Index.Retraction(pkg, r); withdrawn || !newer(r.Version, it.Version) {
			continue
		}
		if isPrerelease(r) {
			unstable = newest(unstable, r.Version)
		} else {
			stable = newest(stable, r.Version)
		}
	}
	if stable != "" {
		return stable
	}
	return unstable
}

// isPrerelease reports whether r is unstable or has a pre-release version.
func isPrerelease(r *index.Release) bool {
	v, err := version.Parse(r.Version)
	return r.Unstable || (err == nil && v.IsPrerelease())
}

// newest returns the later of two versions; "" counts as none.
func newest(a, b string) string {
	if a == "" || newer(b, a) {
		return b
	}
	return a
}
