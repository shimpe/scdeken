// Package recipe reads, checks and edits sc-deken recipe files: the TOML
// documents maintainers write in pull requests to describe one package.
package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/shimpe/scdeken/internal/version"
)

// Kind says what a package contains.
type Kind string

// The three package kinds.
const (
	KindPlugin      Kind = "plugin"
	KindQuark       Kind = "quark"
	KindPluginQuark Kind = "plugin+quark"
)

// defaultSCPathVar is the CMake variable that receives the SuperCollider source path.
const defaultSCPathVar = "SC_PATH"

var (
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// Recipe describes one package and the versions a reviewer approved.
type Recipe struct {
	Name     string                 `toml:"name"`
	Summary  string                 `toml:"summary"`
	Repo     string                 `toml:"repo"`
	License  string                 `toml:"license"`
	Authors  []string               `toml:"authors"`
	Kind     Kind                   `toml:"kind"`
	Build    *Build                 `toml:"build"`
	Requires map[string]Requirement `toml:"requires"`
	Depends  map[string]string      `toml:"depends"`
	Versions Versions               `toml:"versions"`
}

// Versions says how to find releases and which of them are approved.
type Versions struct {
	TagPrefix string            `toml:"tag_prefix"`
	Approved  map[string]string `toml:"approved"` // version → 40-hex commit
}

// Build tells the builder how to compile a plugin.
type Build struct {
	System     string   `toml:"system" json:"system"` // must be "cmake"
	Subdir     string   `toml:"subdir" json:"subdir"`
	SCPathVar  string   `toml:"sc_path_var" json:"sc_path_var"` // default "SC_PATH"
	CMakeFlags []string `toml:"cmake_flags" json:"cmake_flags"`
	Submodules bool     `toml:"submodules" json:"submodules"`
}

// Requirement names a system library in each package manager that has it.
type Requirement struct {
	Apt       string `toml:"apt" json:"apt"`
	Brew      string `toml:"brew" json:"brew"`
	Pacman    string `toml:"pacman" json:"pacman"`
	Dnf       string `toml:"dnf" json:"dnf"`
	Winget    string `toml:"winget" json:"winget"`
	Vcpkg     string `toml:"vcpkg" json:"vcpkg"`
	PkgConfig string `toml:"pkgconfig" json:"pkgconfig"`
}

// Parse decodes a recipe, fills in defaults and validates it.
// Keys the recipe format does not know are an error.
func Parse(data []byte) (Recipe, error) {
	r, _, err := decode(data)
	return r, err
}

// decode is Parse that also returns the document's key order.
func decode(data []byte) (Recipe, toml.MetaData, error) {
	var r Recipe
	md, err := toml.Decode(string(data), &r)
	if err != nil {
		return Recipe{}, md, fmt.Errorf("read recipe: %w", err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return Recipe{}, md, fmt.Errorf("unknown key %q in recipe", undecoded[0].String())
	}
	if r.Build != nil && r.Build.SCPathVar == "" {
		r.Build.SCPathVar = defaultSCPathVar
	}
	if err := r.Validate(); err != nil {
		return Recipe{}, md, err
	}
	return r, md, nil
}

// Load reads a recipe file. Besides Parse's checks, the approved-versions
// table must be the last table so new approvals can be appended as text.
func Load(path string) (Recipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Recipe{}, fmt.Errorf("load recipe: %w", err)
	}
	r, err := parseLayout(data)
	if err != nil {
		return Recipe{}, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// parseLayout is Parse plus the "approved table is last" check.
func parseLayout(data []byte) (Recipe, error) {
	r, md, err := decode(data)
	if err != nil {
		return Recipe{}, err
	}
	if err := checkApprovedLast(md); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// checkApprovedLast fails when any key follows the [versions.approved] table.
func checkApprovedLast(md toml.MetaData) error {
	keys := md.Keys()
	for i, k := range keys {
		if k.String() != "versions.approved" {
			continue
		}
		for _, later := range keys[i+1:] {
			if !strings.HasPrefix(later.String(), "versions.approved.") {
				return fmt.Errorf("[versions.approved] must be the last table, but %q comes after it", later.String())
			}
		}
	}
	return nil
}

// LoadDir loads every *.toml recipe in dir, sorted by name. Each file must be
// called <name>.toml.
func LoadDir(dir string) ([]Recipe, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.toml"))
	if err != nil {
		return nil, fmt.Errorf("list recipes in %s: %w", dir, err)
	}
	var recipes []Recipe
	for _, path := range paths {
		r, err := Load(path)
		if err != nil {
			return nil, err
		}
		if want := r.Name + ".toml"; filepath.Base(path) != want {
			return nil, fmt.Errorf("%s: file must be named %s to match the recipe name", path, want)
		}
		recipes = append(recipes, r)
	}
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].Name < recipes[j].Name })
	return recipes, nil
}

// Validate checks the recipe against the format's rules.
func (r Recipe) Validate() error {
	if !namePattern.MatchString(r.Name) {
		return fmt.Errorf("name %q must start with a letter or digit and use only letters, digits and . _ + -", r.Name)
	}
	if r.Summary == "" {
		return fmt.Errorf("recipe %s: summary must not be empty", r.Name)
	}
	if !strings.HasPrefix(r.Repo, "https://") {
		return fmt.Errorf("recipe %s: repo %q must start with https://", r.Name, r.Repo)
	}
	if err := r.validateKindAndBuild(); err != nil {
		return fmt.Errorf("recipe %s: %w", r.Name, err)
	}
	if err := r.validateApproved(); err != nil {
		return fmt.Errorf("recipe %s: %w", r.Name, err)
	}
	if err := r.validateDepends(); err != nil {
		return fmt.Errorf("recipe %s: %w", r.Name, err)
	}
	return nil
}

func (r Recipe) validateKindAndBuild() error {
	switch r.Kind {
	case KindPlugin, KindPluginQuark, KindQuark:
	default:
		return fmt.Errorf("kind %q must be plugin, quark or plugin+quark", r.Kind)
	}
	if r.HasPlugin() && r.Build == nil {
		return fmt.Errorf("kind %q needs a [build] table", r.Kind)
	}
	if r.Build != nil && r.Build.System != "cmake" {
		return fmt.Errorf("build system %q must be \"cmake\"", r.Build.System)
	}
	return nil
}

func (r Recipe) validateApproved() error {
	if len(r.Versions.Approved) == 0 {
		return fmt.Errorf("[versions.approved] needs at least one version")
	}
	for v, commit := range r.Versions.Approved {
		if _, err := version.Parse(v); err != nil {
			return fmt.Errorf("approved version %q: %w", v, err)
		}
		if !commitPattern.MatchString(commit) {
			return fmt.Errorf("commit for version %s must be 40 lowercase hex characters, got %q", v, commit)
		}
	}
	return nil
}

// HasPlugin reports whether the package ships a compiled plugin.
func (r Recipe) HasPlugin() bool {
	return r.Kind == KindPlugin || r.Kind == KindPluginQuark
}

// ApprovedVersions lists the approved versions, newest first.
func (r Recipe) ApprovedVersions() []string {
	type parsed struct {
		text string
		v    version.Version
	}
	var all []parsed
	for text := range r.Versions.Approved {
		if v, err := version.Parse(text); err == nil {
			all = append(all, parsed{text, v})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v.Compare(all[j].v) > 0 })
	out := make([]string, len(all))
	for i, p := range all {
		out[i] = p.text
	}
	return out
}

// validateDepends checks the [depends] table: each key is a package name or
// an https repository address, each value a version constraint.
func (r Recipe) validateDepends() error {
	for key, constraint := range r.Depends {
		if !namePattern.MatchString(key) && !IsDependencyURL(key) {
			return fmt.Errorf("dependency %q must be a package name or an https:// repository address", key)
		}
		if key == "https://" {
			return fmt.Errorf("dependency %q has no repository address", key)
		}
		if _, err := version.ParseConstraint(constraint); err != nil {
			return fmt.Errorf("dependency %s: %w", key, err)
		}
	}
	return nil
}

// IsDependencyURL reports whether a [depends] key is a repository address
// rather than a package name.
func IsDependencyURL(key string) bool { return strings.HasPrefix(key, "https://") }
