package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shimpe/scdeken/internal/doctor"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/install"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
)

// Set with -ldflags "-X github.com/shimpe/scdeken/internal/cli.buildVersion=..."
var (
	buildVersion = "dev"
	buildCommit  = ""
)

func runVersion(_ context.Context, _ Request, _ *session) (any, error) {
	return map[string]string{"version": buildVersion, "commit": buildCommit}, nil
}

// platformInfo is the answer to "platform".
type platformInfo struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	API       int    `json:"api"`
	APIKnown  bool   `json:"api_known"`
	SCVersion string `json:"sc_version"`
	Key       string `json:"key"` // empty when the plugin API is not known
}

func runPlatform(ctx context.Context, req Request, s *session) (any, error) {
	ix, err := s.loadIndex(ctx, 0)
	if err != nil {
		return nil, err
	}
	sc, err := req.Context.Version()
	if err != nil {
		return nil, err
	}
	arch, err := req.Context.Arch()
	if err != nil {
		return nil, err
	}
	info := platformInfo{OS: platform.HostOS(), Arch: arch, SCVersion: req.Context.SCVersion}
	if info.API, info.APIKnown = platform.APIFor(sc, ix.SCVersions); info.APIKnown {
		info.Key = platform.Key{OS: info.OS, Arch: arch, API: info.API}.String()
	}
	return info, nil
}

func runSearch(ctx context.Context, req Request, s *session) (any, error) {
	kind := recipe.Kind(req.Kind)
	switch kind {
	case "", recipe.KindPlugin, recipe.KindQuark, recipe.KindPluginQuark:
	default:
		return nil, usageError{fmt.Sprintf("unknown kind %q; use plugin, quark or plugin+quark", req.Kind)}
	}
	ix, err := s.loadIndex(ctx, 0)
	if err != nil {
		return nil, err
	}
	hits := index.Search(ix, req.Query, index.SearchOptions{IncludeUnreviewed: req.IncludeUnreviewed, Kind: kind})
	if hits == nil {
		hits = []index.Result{}
	}
	return hits, nil
}

// retraction says that an advisory affects one version of a package.
type retraction struct {
	Version  string `json:"version"`
	Advisory string `json:"advisory"`
}

func runInfo(ctx context.Context, req Request, s *session) (any, error) {
	if req.Package == "" {
		return nil, usageError{"info needs a package name"}
	}
	ix, err := s.loadIndex(ctx, 0)
	if err != nil {
		return nil, err
	}
	p, ok := ix.Find(req.Package)
	if !ok {
		return nil, notFound(req.Package)
	}
	return map[string]any{"package": p, "retractions": retractions(ix, p)}, nil
}

// retractions lists every advisory that affects any version of p.
func retractions(ix *index.Index, p *index.Package) []retraction {
	found := []retraction{}
	for _, r := range p.Versions {
		for _, a := range ix.Advisories {
			if a.Affects(p.Name, p.Repo, r.Version, r.Commit) {
				found = append(found, retraction{Version: r.Version, Advisory: a.ID})
			}
		}
	}
	return found
}

func notFound(name string) error {
	return fmt.Errorf("There is no package called %q in the package list. Try: scdeken search %s", name, name)
}

func runPlan(ctx context.Context, req Request, s *session) (any, error) {
	if req.Package == "" {
		return nil, usageError{"plan needs a package name"}
	}
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	return svc.Plan(ctx, req.Package, req.Version)
}

// installed is the answer to "install" and "update": one result per
// package, each dependency before the package that needs it.
type installed struct {
	Results []install.Result `json:"results"`
}

func runInstall(ctx context.Context, req Request, s *session) (any, error) {
	if req.Package == "" {
		return nil, usageError{"install needs a package name"}
	}
	if err := req.Choices.Validate(); err != nil {
		return nil, err
	}
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := svc.Plan(ctx, req.Package, req.Version)
	if err != nil {
		return nil, err
	}
	plans := append(append([]install.Plan{}, plan.Depends...), plan)
	if err := checkConfirmed(plans, req.Choices); err != nil {
		return plan, err
	}
	return applyAll(ctx, svc, plans, req.Choices)
}

// updatePlans is what "update" without a package answers when the user has
// to confirm something first: every plan, in install order.
type updatePlans struct {
	Plans []install.Plan `json:"plans"`
}

// runUpdate installs the newest version of one package, or of every
// package that has an update. Nothing is installed before the user has
// answered for all of them.
func runUpdate(ctx context.Context, req Request, s *session) (any, error) {
	if req.Package != "" {
		return runInstall(ctx, req, s)
	}
	if err := req.Choices.Validate(); err != nil {
		return nil, err
	}
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	plans, err := planUpdates(ctx, svc)
	if err != nil {
		return nil, err
	}
	if err := checkConfirmed(plans, req.Choices); err != nil {
		return updatePlans{Plans: plans}, err
	}
	return applyAll(ctx, svc, plans, req.Choices)
}

// planUpdates plans the newest version of every package that has an
// update, each package once and its dependencies first.
func planUpdates(ctx context.Context, svc *install.Service) ([]install.Plan, error) {
	plans := []install.Plan{}
	seen := map[string]bool{}
	for _, it := range svc.Updates() {
		plan, err := svc.Plan(ctx, it.Name, "")
		if err != nil {
			return nil, err
		}
		for _, p := range append(append([]install.Plan{}, plan.Depends...), plan) {
			if key := strings.ToLower(p.Package); !seen[key] {
				seen[key] = true
				plans = append(plans, p)
			}
		}
	}
	return plans, nil
}

// checkConfirmed returns a ConfirmationError when the choices leave a
// confirmation open on any of the plans, or were given for other plans
// than these (the package list changed since the user looked).
func checkConfirmed(plans []install.Plan, c install.Choices) error {
	missing := missingAcross(plans, c)
	if !c.Confirmed(plans) {
		return &install.ConfirmationError{Needed: append(missing, install.ConfirmChanged), Message: install.ChangedMessage}
	}
	if len(missing) > 0 {
		return &install.ConfirmationError{Needed: missing}
	}
	return nil
}

// applyAll installs the plans in order and stops at the first failure.
func applyAll(ctx context.Context, svc *install.Service, plans []install.Plan, c install.Choices) (any, error) {
	out := installed{Results: []install.Result{}}
	for _, p := range plans {
		res, err := svc.ApplyWith(ctx, p, c)
		if err != nil {
			return nil, err
		}
		out.Results = append(out.Results, res)
	}
	return out, nil
}

// missingAcross returns the sorted, de-duplicated confirmations that the
// choices leave open on any of the plans, so nothing is installed before
// the user has answered for everything they were shown.
func missingAcross(plans []install.Plan, c install.Choices) []string {
	seen := map[string]bool{}
	for _, p := range plans {
		for _, m := range p.Missing(c) {
			seen[m] = true
		}
	}
	var all []string
	for m := range seen {
		all = append(all, m)
	}
	sort.Strings(all)
	return all
}

func runRemove(_ context.Context, req Request, s *session) (any, error) {
	if req.Package == "" {
		return nil, usageError{"remove needs a package name"}
	}
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if err := svc.Remove(req.Package); err != nil {
		return nil, err
	}
	return map[string]string{"removed": req.Package}, nil
}

func runList(ctx context.Context, _ Request, s *session) (any, error) {
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	return nonNil(svc.List()), nil
}

func runUpdates(ctx context.Context, _ Request, s *session) (any, error) {
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	return nonNil(svc.Updates()), nil
}

// nonNil makes an empty list print as [] rather than null.
func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// runDoctor checks the build tools. Only a package's own requirements need
// the package index, so plain "doctor" works offline.
func runDoctor(ctx context.Context, req Request, s *session) (any, error) {
	var requires map[string]recipe.Requirement
	if req.Package != "" {
		ix, err := s.loadIndex(ctx, 0)
		if err != nil {
			return nil, err
		}
		p, ok := ix.Find(req.Package)
		if !ok {
			return nil, notFound(req.Package)
		}
		requires = p.Requires
	}
	return doctor.Check(ctx, s.deps.Runner, doctor.DetectEnv(), requires), nil
}

func runAdvisoriesCheck(ctx context.Context, _ Request, s *session) (any, error) {
	svc, err := s.serviceWithIndex(ctx)
	if err != nil {
		return nil, err
	}
	return nonNil(svc.CheckAdvisories()), nil
}

func runAdvisoriesKeep(_ context.Context, req Request, s *session) (any, error) {
	if req.AdvisoryID == "" {
		return nil, usageError{"advisories keep needs an advisory id"}
	}
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if err := svc.Keep(req.AdvisoryID); err != nil {
		return nil, err
	}
	return map[string]string{"kept": req.AdvisoryID}, nil
}

func runQuarantine(_ context.Context, req Request, s *session) (any, error) {
	if req.Path == "" || req.AdvisoryID == "" {
		return nil, usageError{"quarantine needs a path and --id <advisory-id>"}
	}
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	moved, err := svc.Quarantine(req.Path, req.AdvisoryID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"moved_to": moved}, nil
}

func runQuarantineClear(_ context.Context, _ Request, s *session) (any, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if err := svc.ClearQuarantine(); err != nil {
		return nil, err
	}
	return struct{}{}, nil
}

// runRefresh downloads the package index again, even if the saved copy is
// recent.
func runRefresh(ctx context.Context, _ Request, s *session) (any, error) {
	ix, err := s.loadIndex(ctx, time.Nanosecond)
	if err != nil {
		return nil, err
	}
	return map[string]any{"packages": len(ix.Packages), "generated": ix.Generated}, nil
}
