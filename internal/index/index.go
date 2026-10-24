// Package index defines the signed JSON catalog of all packages that the
// client downloads, and the lookups the rest of sc-deken runs over it.
package index

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
)

// SchemaVersion is the index format this sc-deken understands.
const SchemaVersion = 1

// Trust says how much review a package has had.
type Trust string

// The trust levels a package may carry.
const (
	Reviewed   Trust = "reviewed"
	Community  Trust = "community"
	Unreviewed Trust = "unreviewed"
)

// Index is the whole catalog.
type Index struct {
	Schema         int                  `json:"schema"`
	Generated      time.Time            `json:"generated"`
	SCVersions     []platform.SCVersion `json:"sc_versions"`
	Packages       []Package            `json:"packages"`
	Advisories     []advisory.Advisory  `json:"advisories"`
	NextPublicKeys []string             `json:"next_public_keys,omitempty"`
}

// Package is one installable plugin or quark.
type Package struct {
	Name          string                        `json:"name"`
	Summary       string                        `json:"summary"`
	Repo          string                        `json:"repo"`
	License       string                        `json:"license"`
	Authors       []string                      `json:"authors,omitempty"`
	Kind          recipe.Kind                   `json:"kind"`
	Trust         Trust                         `json:"trust"`
	Stars         int                           `json:"stars"`
	Pushed        time.Time                     `json:"pushed"`
	ScannedCommit string                        `json:"scanned_commit"`
	Classes       []string                      `json:"classes,omitempty"`
	UGens         []string                      `json:"ugens,omitempty"`
	QuarkName     string                        `json:"quark_name,omitempty"`    // scan result: name in the .quark file
	QuarkSummary  string                        `json:"quark_summary,omitempty"` // scan result: summary in the .quark file
	LicenseFile   string                        `json:"license_file,omitempty"`  // scan result: SPDX guess from the license file
	PluginFiles   []string                      `json:"plugin_files,omitempty"`  // e.g. "FMX6.scx", "FMX6_supernova.scx"
	Depends       map[string]string             `json:"depends,omitempty"`
	Build         *recipe.Build                 `json:"build,omitempty"`
	Requires      map[string]recipe.Requirement `json:"requires,omitempty"`
	Versions      []Release                     `json:"versions"` // newest first
}

// Release is one version of a package.
type Release struct {
	Version  string  `json:"version"`
	Commit   string  `json:"commit"`
	Unstable bool    `json:"unstable,omitempty"`
	Builds   []Build `json:"builds,omitempty"`
}

// Build is a prebuilt archive of a release for one platform.
type Build struct {
	Platform string `json:"platform"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// Decode reads an index from JSON and refuses formats newer than this
// sc-deken understands.
func Decode(data []byte) (*Index, error) {
	var ix Index
	if err := json.Unmarshal(data, &ix); err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	if ix.Schema != SchemaVersion {
		return nil, fmt.Errorf("index format %d is newer than this sc-deken; please update sc-deken", ix.Schema)
	}
	return &ix, nil
}

// Encode writes the index as indented JSON with packages sorted by
// lowercase name, so the output is stable.
func (ix *Index) Encode() ([]byte, error) {
	out := *ix
	out.Packages = append([]Package(nil), ix.Packages...)
	sort.SliceStable(out.Packages, func(i, j int) bool {
		return strings.ToLower(out.Packages[i].Name) < strings.ToLower(out.Packages[j].Name)
	})
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write index: %w", err)
	}
	return data, nil
}

// Find returns the package with the given name, ignoring case.
func (ix *Index) Find(name string) (*Package, bool) {
	for i := range ix.Packages {
		if strings.EqualFold(ix.Packages[i].Name, name) {
			return &ix.Packages[i], true
		}
	}
	return nil, false
}

// FindRepo returns the package whose repository is the same as repo, after
// normalizing both.
func (ix *Index) FindRepo(repo string) (*Package, bool) {
	want := advisory.NormalizeRepo(repo)
	for i := range ix.Packages {
		if advisory.NormalizeRepo(ix.Packages[i].Repo) == want {
			return &ix.Packages[i], true
		}
	}
	return nil, false
}

// Release returns version v of p, or the latest release when v is empty.
func (ix *Index) Release(p *Package, v string) (*Release, bool) {
	if v == "" {
		return ix.Latest(p)
	}
	for i := range p.Versions {
		if p.Versions[i].Version == v {
			return &p.Versions[i], true
		}
	}
	return nil, false
}

// Latest returns the newest stable release that is not retracted as
// malware, or failing that the newest unstable one.
func (ix *Index) Latest(p *Package) (*Release, bool) {
	if r, ok := ix.newest(p, false); ok {
		return r, true
	}
	return ix.newest(p, true)
}

// newest returns the first release of p with the given stability that is not
// retracted as malware.
func (ix *Index) newest(p *Package, unstable bool) (*Release, bool) {
	for i := range p.Versions {
		r := &p.Versions[i]
		if r.Unstable != unstable {
			continue
		}
		if a, hit := ix.Retraction(p, r); hit && a.Severity == advisory.Malware {
			continue
		}
		return r, true
	}
	return nil, false
}

// Retraction returns the most severe advisory affecting release r of p. When
// two are equally severe the first listed wins.
func (ix *Index) Retraction(p *Package, r *Release) (*advisory.Advisory, bool) {
	var worst *advisory.Advisory
	for i := range ix.Advisories {
		a := &ix.Advisories[i]
		if !a.Affects(p.Name, p.Repo, r.Version, r.Commit) {
			continue
		}
		if worst == nil || severityRank(a.Severity) > severityRank(worst.Severity) {
			worst = a
		}
	}
	return worst, worst != nil
}

// severityRank orders severities: malware > vulnerable > broken.
func severityRank(s advisory.Severity) int {
	switch s {
	case advisory.Malware:
		return 3
	case advisory.Vulnerable:
		return 2
	case advisory.Broken:
		return 1
	}
	return 0
}

// BuildFor returns the prebuilt archive that runs on platform k. Builds whose
// platform string cannot be parsed are skipped.
func (r *Release) BuildFor(k platform.Key) (*Build, bool) {
	for i := range r.Builds {
		bk, err := platform.ParseKey(r.Builds[i].Platform)
		if err == nil && k.Accepts(bk) {
			return &r.Builds[i], true
		}
	}
	return nil, false
}
