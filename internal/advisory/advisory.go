// Package advisory reads the advisories maintainers publish to retract
// malicious or broken package versions, and matches them against what a
// user has installed.
package advisory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Severity says how bad an advisory is.
type Severity string

// The severities an advisory may carry.
const (
	Malware    Severity = "malware"
	Vulnerable Severity = "vulnerable"
	Broken     Severity = "broken"
)

// minCommitPrefix is the shortest commit prefix that counts as a match.
const minCommitPrefix = 7

var idPattern = regexp.MustCompile(`^SCD-\d{4}-\d{4}$`)

// Advisory retracts some versions or commits of one package.
type Advisory struct {
	ID        string   `toml:"id" json:"id"`
	Package   string   `toml:"package" json:"package,omitempty"`
	Repo      string   `toml:"repo" json:"repo,omitempty"`
	Versions  []string `toml:"versions" json:"versions,omitempty"` // "*" = all
	Commits   []string `toml:"commits" json:"commits,omitempty"`
	Severity  Severity `toml:"severity" json:"severity"`
	Summary   string   `toml:"summary" json:"summary"`
	Details   string   `toml:"details" json:"details"`
	Published string   `toml:"published" json:"published"` // YYYY-MM-DD
}

// file is Advisory as written in TOML, where "published" may be a bare date
// or a quoted string.
type file struct {
	Advisory
	Published any `toml:"published"`
}

// Parse reads one advisory from TOML, rejecting unknown keys and invalid
// content.
func Parse(data []byte) (Advisory, error) {
	var f file
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return Advisory{}, fmt.Errorf("read advisory: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Advisory{}, fmt.Errorf("unknown key %q in advisory", undecoded[0].String())
	}
	a := f.Advisory
	if a.Published, err = publishedText(f.Published); err != nil {
		return Advisory{}, err
	}
	if err := a.Validate(); err != nil {
		return Advisory{}, err
	}
	return a, nil
}

// publishedText turns a decoded "published" value into YYYY-MM-DD text.
func publishedText(v any) (string, error) {
	switch p := v.(type) {
	case nil:
		return "", nil
	case time.Time:
		return p.Format(time.DateOnly), nil
	case string:
		return p, nil
	}
	return "", fmt.Errorf("published must be a date like 2026-11-02")
}

// LoadDir reads every *.toml advisory in dir, in file-name order. Ids must be
// unique across the directory.
func LoadDir(dir string) ([]Advisory, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, fmt.Errorf("list advisories in %s: %w", dir, err)
	}
	sort.Strings(paths)
	var advs []Advisory
	seen := map[string]string{}
	for _, p := range paths {
		a, err := loadFile(p)
		if err != nil {
			return nil, err
		}
		if first, dup := seen[a.ID]; dup {
			return nil, fmt.Errorf("advisory id %s appears in both %s and %s", a.ID, first, p)
		}
		seen[a.ID] = p
		advs = append(advs, a)
	}
	return advs, nil
}

func loadFile(path string) (Advisory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Advisory{}, fmt.Errorf("read advisory %s: %w", path, err)
	}
	a, err := Parse(data)
	if err != nil {
		return Advisory{}, fmt.Errorf("%s: %w", path, err)
	}
	return a, nil
}

// Validate checks that the advisory is complete and well formed.
func (a Advisory) Validate() error {
	if !idPattern.MatchString(a.ID) {
		return fmt.Errorf("advisory id %q must look like SCD-2026-0001", a.ID)
	}
	if a.Package == "" && a.Repo == "" {
		return fmt.Errorf("advisory %s needs a package or a repo", a.ID)
	}
	if len(a.Versions) == 0 && len(a.Commits) == 0 {
		return fmt.Errorf("advisory %s needs versions or commits", a.ID)
	}
	switch a.Severity {
	case Malware, Vulnerable, Broken:
	default:
		return fmt.Errorf("advisory %s: severity %q must be malware, vulnerable or broken", a.ID, a.Severity)
	}
	if _, err := time.Parse(time.DateOnly, a.Published); err != nil {
		return fmt.Errorf("advisory %s: published %q must be a date like 2026-11-02", a.ID, a.Published)
	}
	return nil
}

// Affects reports whether the advisory covers the given installed copy.
// An unknown (empty) version matches only "*".
func (a Advisory) Affects(name, repo, version, commit string) bool {
	return a.sameIdentity(name, repo) && (a.coversVersion(version) || a.coversCommit(commit))
}

func (a Advisory) sameIdentity(name, repo string) bool {
	if a.Package != "" && strings.EqualFold(a.Package, name) {
		return true
	}
	return a.Repo != "" && repo != "" && NormalizeRepo(a.Repo) == NormalizeRepo(repo)
}

func (a Advisory) coversVersion(version string) bool {
	for _, v := range a.Versions {
		if v == "*" || (version != "" && v == version) {
			return true
		}
	}
	return false
}

func (a Advisory) coversCommit(commit string) bool {
	for _, c := range a.Commits {
		if len(c) >= minCommitPrefix && strings.HasPrefix(commit, c) {
			return true
		}
	}
	return false
}

// Installed describes one copy of a package found on the user's machine.
type Installed struct {
	Name, Repo, Version, Commit, Path string
	Source                            string // "managed" | "quark" | "external"
}

// Hit pairs an advisory with the installed copy it affects.
type Hit struct {
	Advisory  Advisory
	Installed Installed
}

// Match returns every (advisory, installed copy) pair where the advisory
// affects the copy, grouped by advisory in the order given.
func Match(advs []Advisory, inst []Installed) []Hit {
	var hits []Hit
	for _, a := range advs {
		for _, i := range inst {
			if a.Affects(i.Name, i.Repo, i.Version, i.Commit) {
				hits = append(hits, Hit{Advisory: a, Installed: i})
			}
		}
	}
	return hits
}

// ValidID reports whether id looks like SCD-2026-0001.
func ValidID(id string) bool { return idPattern.MatchString(id) }
