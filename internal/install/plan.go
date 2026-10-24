package install

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/doctor"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
)

// Route is how a package gets installed.
type Route string

// The routes a plan can take.
const (
	RoutePrebuilt   Route = "prebuilt"
	RouteLocalBuild Route = "local-build"
	RouteQuark      Route = "quark"
)

// Confirmations a plan can ask the user for.
const (
	ConfirmUnreviewed = "unreviewed"
	ConfirmRetracted  = "retracted"
	ConfirmDuplicates = "duplicates"
)

// Steps that must happen after the files are in place.
const (
	AfterRecompile    = "recompile"
	AfterRebootServer = "reboot-server"
)

// ErrBlocked marks a version that was withdrawn as malware.
var ErrBlocked = errors.New("blocked")

// blockedError carries the musician-facing message and matches ErrBlocked.
type blockedError struct{ msg string }

func (e blockedError) Error() string { return e.msg }
func (e blockedError) Unwrap() error { return ErrBlocked }

// QuarkRef names the git repository and commit of a quark.
type QuarkRef struct {
	URL    string `json:"url"`
	Commit string `json:"commit"`
}

// Duplicate is a copy of the package that is already on the machine.
type Duplicate struct {
	Path    string `json:"path"`
	Matched string `json:"matched"`
}

// Plan says how one package will be installed and what the user must
// confirm first.
type Plan struct {
	Package      string             `json:"package"`
	Version      string             `json:"version"`
	Commit       string             `json:"commit"`
	Repo         string             `json:"repo"` // the package's repository address
	Kind         recipe.Kind        `json:"kind"`
	Trust        index.Trust        `json:"trust"`
	Route        Route              `json:"route"`
	Platform     string             `json:"platform,omitempty"` // platform key string when known
	Build        *index.Build       `json:"build,omitempty"`    // prebuilt archive
	Quark        *QuarkRef          `json:"quark,omitempty"`    // route quark, or quark part of plugin+quark
	Reason       string             `json:"reason,omitempty"`   // why local build
	Confirm      []string           `json:"confirm,omitempty"`  // "unreviewed", "retracted", "duplicates"
	Retraction   *advisory.Advisory `json:"retraction,omitempty"`
	Duplicates   []Duplicate        `json:"duplicates,omitempty"`
	Doctor       *doctor.Report     `json:"doctor,omitempty"`  // local-build only
	Depends      []Plan             `json:"depends,omitempty"` // install these first, in order
	AfterInstall []string           `json:"after_install"`     // "recompile" and/or "reboot-server"
	IsUpdate     bool               `json:"is_update"`
}

// Plan works out how to install version of the named package (the latest
// when version is empty), including the dependencies that must come first.
func (s *Service) Plan(ctx context.Context, name, version string) (Plan, error) {
	pkg, rel, err := s.lookup(name, version)
	if err != nil {
		return Plan{}, err
	}
	plan, err := s.planOne(ctx, pkg, rel)
	if err != nil {
		return Plan{}, err
	}
	plan.IsUpdate = s.installed(pkg)
	walk := dependencyWalk{s: s, done: map[string]bool{}}
	if err := walk.visit(ctx, pkg, []string{pkg.Name}); err != nil {
		return Plan{}, err
	}
	plan.Depends = walk.order
	return plan, nil
}

// lookup finds the package and the release to install.
func (s *Service) lookup(name, version string) (*index.Package, *index.Release, error) {
	pkg, ok := s.Index.Find(name)
	if !ok {
		return nil, nil, fmt.Errorf("no package named %q; try searching", name)
	}
	rel, ok := s.Index.Release(pkg, version)
	switch {
	case ok:
		return pkg, rel, nil
	case version == "":
		return nil, nil, fmt.Errorf("%s has no versions to install", pkg.Name)
	default:
		return nil, nil, fmt.Errorf("%s has no version %q", pkg.Name, version)
	}
}

// planOne plans a single package, leaving out Depends and IsUpdate.
func (s *Service) planOne(ctx context.Context, pkg *index.Package, rel *index.Release) (Plan, error) {
	p := Plan{
		Package: pkg.Name, Version: rel.Version, Commit: rel.Commit, Repo: pkg.Repo,
		Kind: pkg.Kind, Trust: pkg.Trust,
		AfterInstall: afterInstall(pkg.Kind),
	}
	if err := s.applyRetraction(&p, pkg, rel); err != nil {
		return Plan{}, err
	}
	if pkg.Trust == index.Unreviewed || rel.Unstable {
		p.Confirm = append(p.Confirm, ConfirmUnreviewed)
	}
	if err := s.chooseRoute(ctx, &p, pkg, rel); err != nil {
		return Plan{}, err
	}
	if err := s.applyDuplicates(&p, pkg); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// applyDuplicates records copies installed outside sc-deken and asks the
// user what to do about them.
func (s *Service) applyDuplicates(p *Plan, pkg *index.Package) error {
	dups, err := s.findDuplicates(pkg)
	if err != nil {
		return fmt.Errorf("look for existing copies of %s: %w", pkg.Name, err)
	}
	if len(dups) > 0 {
		p.Duplicates = dups
		p.Confirm = append(p.Confirm, ConfirmDuplicates)
	}
	return nil
}

// afterInstall lists the steps needed once the files are in place.
func afterInstall(kind recipe.Kind) []string {
	steps := []string{AfterRecompile}
	if hasPlugin(kind) {
		steps = append(steps, AfterRebootServer)
	}
	return steps
}

func hasPlugin(kind recipe.Kind) bool {
	return kind == recipe.KindPlugin || kind == recipe.KindPluginQuark
}

// applyRetraction blocks malware and asks for confirmation on other
// retractions.
func (s *Service) applyRetraction(p *Plan, pkg *index.Package, rel *index.Release) error {
	adv, ok := s.Index.Retraction(pkg, rel)
	if !ok {
		return nil
	}
	if adv.Severity == advisory.Malware {
		return blockedError{fmt.Sprintf("%s %s was withdrawn: %s", pkg.Name, rel.Version, adv.Summary)}
	}
	p.Retraction = adv
	p.Confirm = append(p.Confirm, ConfirmRetracted)
	return nil
}

// chooseRoute fills in Route and everything that depends on it.
func (s *Service) chooseRoute(ctx context.Context, p *Plan, pkg *index.Package, rel *index.Release) error {
	if pkg.Kind == recipe.KindQuark {
		p.Route = RouteQuark
		p.Quark = &QuarkRef{URL: pkg.Repo, Commit: rel.Commit}
		return nil
	}
	if pkg.Kind == recipe.KindPluginQuark {
		p.Quark = &QuarkRef{URL: pkg.Repo, Commit: rel.Commit}
	}
	build, reason, err := s.findPrebuilt(p, pkg, rel)
	if err != nil {
		return err
	}
	if build != nil {
		p.Route, p.Build = RoutePrebuilt, build
		return nil
	}
	if pkg.Build == nil {
		return fmt.Errorf("%s cannot be installed automatically yet (no build recipe)", pkg.Name)
	}
	report := doctor.Check(ctx, s.Runner, s.Env, pkg.Requires)
	p.Route, p.Reason, p.Doctor = RouteLocalBuild, reason, &report
	return nil
}

// findPrebuilt returns the archive to download, or the reason there is none.
// It also records the user's platform key on p when the API is known.
func (s *Service) findPrebuilt(p *Plan, pkg *index.Package, rel *index.Release) (*index.Build, string, error) {
	key, known, err := s.platformKey()
	if err != nil {
		return nil, "", err
	}
	if !known {
		return nil, fmt.Sprintf("Your SuperCollider version (%s) is newer than the prebuilt packages know about, so it will be built on this computer.", s.Ctx.SCVersion), nil
	}
	p.Platform = key.String()
	reason := fmt.Sprintf("No prebuilt version for %s, so it will be built on this computer.", key)
	if pkg.Trust != index.Reviewed {
		return nil, reason, nil
	}
	if b, ok := rel.BuildFor(key); ok {
		return b, "", nil
	}
	return nil, reason, nil
}

// platformKey is the key of this machine; known is false when the
// SuperCollider version is too new to have a plugin API in the index.
func (s *Service) platformKey() (key platform.Key, known bool, err error) {
	v, err := s.Ctx.Version()
	if err != nil {
		return key, false, fmt.Errorf("read SuperCollider version: %w", err)
	}
	arch, err := s.Ctx.Arch()
	if err != nil {
		return key, false, fmt.Errorf("read CPU architecture: %w", err)
	}
	api, ok := platform.APIFor(v, s.Index.SCVersions)
	if !ok {
		return key, false, nil
	}
	return platform.Key{OS: platform.HostOS(), Arch: arch, API: api}, true, nil
}

// installed reports whether the package is already on this machine, either
// through sc-deken or as a quark.
func (s *Service) installed(pkg *index.Package) bool {
	if s.State != nil {
		if _, ok := s.State.Packages[strings.ToLower(pkg.Name)]; ok {
			return true
		}
	}
	repo := advisory.NormalizeRepo(pkg.Repo)
	for _, q := range s.Ctx.Quarks {
		if advisory.NormalizeRepo(q.URL) == repo {
			return true
		}
	}
	return false
}

// dependencyWalk collects the plans of all missing dependencies, children
// before parents.
type dependencyWalk struct {
	s     *Service
	done  map[string]bool // lowercase names already handled
	order []Plan
}

// visit plans every dependency of pkg. path holds the names leading here,
// for cycle errors.
func (w *dependencyWalk) visit(ctx context.Context, pkg *index.Package, path []string) error {
	for _, depName := range sortedKeys(pkg.Depends) {
		dep, ok := w.s.Index.Find(depName)
		if !ok {
			return fmt.Errorf("%s needs %q, which is not in the package list", pkg.Name, depName)
		}
		if err := w.visitOne(ctx, dep, path); err != nil {
			return err
		}
	}
	return nil
}

func (w *dependencyWalk) visitOne(ctx context.Context, dep *index.Package, path []string) error {
	if cycle := cycleAt(path, dep.Name); cycle != "" {
		return fmt.Errorf("dependency cycle: %s", cycle)
	}
	key := strings.ToLower(dep.Name)
	if w.done[key] {
		return nil
	}
	w.done[key] = true
	if w.s.installed(dep) {
		return nil
	}
	rel, ok := w.s.Index.Latest(dep)
	if !ok {
		return fmt.Errorf("%s has no versions to install", dep.Name)
	}
	if err := w.visit(ctx, dep, append(path[:len(path):len(path)], dep.Name)); err != nil {
		return err
	}
	plan, err := w.s.planOne(ctx, dep, rel)
	if err != nil {
		return err
	}
	w.order = append(w.order, plan)
	return nil
}

// cycleAt returns "A → B → A" when name is already on path, else "".
func cycleAt(path []string, name string) string {
	for i, n := range path {
		if strings.EqualFold(n, name) {
			return strings.Join(append(path[i:len(path):len(path)], name), " → ")
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
