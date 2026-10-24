package install

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/index"
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
			FixedVersion: s.fixedVersion(h.Advisory, it),
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

// repoOf is the repository an installed item came from, "" when unknown.
func (s *Service) repoOf(it Item) string {
	if pkg, ok := s.Index.Find(it.Name); ok {
		return pkg.Repo
	}
	if e, ok := s.State.Packages[strings.ToLower(it.Name)]; ok {
		return e.Repo
	}
	for _, q := range s.Ctx.Quarks {
		if q.Path == it.Path {
			return q.URL
		}
	}
	return ""
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
				Source: SourceExternal, Trust: string(pkg.Trust),
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

// fixedVersion is the newest release newer than the installed one that the
// advisory does not affect; "" when there is none or the installed version
// is unknown.
func (s *Service) fixedVersion(a advisory.Advisory, it Item) string {
	pkg, ok := s.Index.Find(it.Name)
	if !ok {
		pkg, ok = s.advisoryPackage(a)
	}
	if !ok || it.Version == "" {
		return ""
	}
	best := ""
	for _, r := range pkg.Versions {
		if a.Affects(pkg.Name, pkg.Repo, r.Version, r.Commit) || !newer(r.Version, it.Version) {
			continue
		}
		if best == "" || newer(r.Version, best) {
			best = r.Version
		}
	}
	return best
}

// Keep remembers that the user wants to keep the packages advisoryID
// withdraws, so CheckAdvisories stops reporting it.
func (s *Service) Keep(advisoryID string) error {
	for _, id := range s.State.KeptAdvisories {
		if id == advisoryID {
			return nil
		}
	}
	s.State.KeptAdvisories = append(s.State.KeptAdvisories, advisoryID)
	if err := s.State.Save(s.Paths.State); err != nil {
		s.State.KeptAdvisories = s.State.KeptAdvisories[:len(s.State.KeptAdvisories)-1]
		return fmt.Errorf("remember kept advisory %s: %w", advisoryID, err)
	}
	return nil
}
