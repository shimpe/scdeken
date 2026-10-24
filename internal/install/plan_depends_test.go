package install

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/state"
)

// quarkWith is a reviewed quark with the given versions (newest first) and
// dependencies.
func quarkWith(name string, depends map[string]string, versions ...string) index.Package {
	p := pkg(name, recipe.KindQuark, index.Reviewed, panolaCommit)
	p.Versions = nil
	for _, v := range versions {
		p.Versions = append(p.Versions, index.Release{Version: v, Commit: panolaCommit})
	}
	p.Depends = depends
	return p
}

// depsService is a Service over just the given packages.
func depsService(t *testing.T, pkgs ...index.Package) *Service {
	t.Helper()
	s := newService(t)
	s.Index.Packages = pkgs
	return s
}

func planVersions(plans []Plan) []string {
	var out []string
	for _, p := range plans {
		out = append(out, p.Package+" "+p.Version)
	}
	return out
}

func TestPlanPicksNewestVersionSatisfyingConstraint(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"Lib": ">=1.0,<2.0"}, "1.0.0"),
		quarkWith("Lib", nil, "2.1.0", "1.5.0", "1.0.0"),
	)
	got := planVersions(mustPlan(t, s, "App", "").Depends)
	if !reflect.DeepEqual(got, []string{"Lib 1.5.0"}) {
		t.Errorf("Depends = %v", got)
	}
}

func TestPlanCombinesConstraintsOfAllDependents(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"X": ">=1.0", "Y": "*"}, "1.0.0"),
		quarkWith("Y", map[string]string{"X": "<2.0"}, "1.0.0"),
		quarkWith("X", nil, "2.0.0", "1.9.0", "0.5.0"),
	)
	got := planVersions(mustPlan(t, s, "App", "").Depends)
	if !reflect.DeepEqual(got, []string{"X 1.9.0", "Y 1.0.0"}) {
		t.Errorf("Depends = %v", got)
	}
}

func TestPlanConflictingConstraints(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"P": ">=2.0", "Q": "*"}, "1.0.0"),
		quarkWith("Q", map[string]string{"P": "<2.0"}, "1.0.0"),
		quarkWith("P", nil, "2.0.0", "1.0.0"),
	)
	err := planErr(t, s, "App", "")
	if want := "P: App needs >=2.0 but Q needs <2.0; no version satisfies both"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestPlanNoVersionSatisfies(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"Lib": ">=3.0"}, "1.0.0"),
		quarkWith("Lib", nil, "2.1.0", "1.0.0"),
	)
	err := planErr(t, s, "App", "")
	if want := "App needs Lib >=3.0, but the package list has no such version (latest is 2.1.0)"; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestPlanSkipsWithdrawnMalwareVersion(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"Lib": ">=1.0"}, "1.0.0"),
		quarkWith("Lib", nil, "1.5.0", "1.0.0"),
	)
	s.Index.Advisories = []advisory.Advisory{{ID: "SCD-9", Package: "Lib", Versions: []string{"1.5.0"}, Severity: advisory.Malware, Summary: "bad"}}
	got := planVersions(mustPlan(t, s, "App", "").Depends)
	if !reflect.DeepEqual(got, []string{"Lib 1.0.0"}) {
		t.Errorf("Depends = %v", got)
	}
}

func TestPlanInstalledDependencyThatSatisfiesIsSkipped(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"Lib": ">=1.0"}, "1.0.0"),
		quarkWith("Lib", map[string]string{"Deep": "*"}, "2.0.0", "1.0.0"),
		quarkWith("Deep", nil, "1.0.0"),
	)
	s.State.Packages["lib"] = state.Entry{Name: "Lib", Version: "1.0.0"}
	if got := mustPlan(t, s, "App", "").Depends; len(got) != 0 {
		t.Errorf("Depends = %v, want none", planVersions(got))
	}
}

func TestPlanInstalledDependencyThatDoesNotSatisfyIsUpdated(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"Lib": ">=1.5"}, "1.0.0"),
		quarkWith("Lib", map[string]string{"Deep": "*"}, "2.0.0", "1.0.0"),
		quarkWith("Deep", nil, "1.0.0"),
	)
	s.State.Packages["lib"] = state.Entry{Name: "Lib", Version: "1.0.0"}
	deps := mustPlan(t, s, "App", "").Depends
	if got := planVersions(deps); !reflect.DeepEqual(got, []string{"Deep 1.0.0", "Lib 2.0.0"}) {
		t.Fatalf("Depends = %v", got)
	}
	if deps[0].IsUpdate || !deps[1].IsUpdate {
		t.Errorf("IsUpdate = %v, %v; want false, true", deps[0].IsUpdate, deps[1].IsUpdate)
	}
}

func TestPlanInstalledQuarkVersionFoundByCommit(t *testing.T) {
	lib := quarkWith("Lib", nil, "2.0.0", "1.0.0")
	lib.Versions[1].Commit = fmx6Commit
	s := depsService(t, quarkWith("App", map[string]string{"Lib": ">=1.5"}, "1.0.0"), lib)
	s.Ctx.Quarks = []scenv.Quark{{Name: "Lib", URL: lib.Repo, Commit: fmx6Commit}}
	if got := planVersions(mustPlan(t, s, "App", "").Depends); !reflect.DeepEqual(got, []string{"Lib 2.0.0"}) {
		t.Errorf("Depends = %v", got)
	}
}

func TestPlanUnresolvedURLDependency(t *testing.T) {
	app := quarkWith("App", nil, "1.0.0")
	app.DependsUnresolved = map[string]string{"https://example.org/x/Elsewhere.git": "=2.0"}
	s := depsService(t, app)
	p := mustPlan(t, s, "App", "")
	if len(p.Depends) != 1 {
		t.Fatalf("Depends = %+v", p.Depends)
	}
	d := p.Depends[0]
	if d.Package != "Elsewhere" || d.Version != "0.0.0+head" || d.Commit != "" || d.Repo != "https://example.org/x/Elsewhere.git" ||
		d.Route != RouteQuark || d.Trust != index.Unreviewed || d.Kind != recipe.KindQuark ||
		!reflect.DeepEqual(d.Confirm, []string{ConfirmUnreviewed}) ||
		d.Quark == nil || d.Quark.URL != d.Repo || d.Quark.Commit != "" {
		t.Errorf("dependency plan = %+v", d)
	}
}

func TestPlanUnresolvedURLInstalledIsSkipped(t *testing.T) {
	app := quarkWith("App", nil, "1.0.0")
	app.DependsUnresolved = map[string]string{"https://example.org/x/Elsewhere": "*"}
	s := depsService(t, app)
	s.Ctx.Quarks = []scenv.Quark{{Name: "Elsewhere", URL: "git@example.org:X/Elsewhere.git"}}
	if got := mustPlan(t, s, "App", "").Depends; len(got) != 0 {
		t.Errorf("Depends = %+v, want none", got)
	}
}

func TestPlanUnresolvedURLOfDependencyComesFirst(t *testing.T) {
	lib := quarkWith("Lib", nil, "1.0.0")
	lib.DependsUnresolved = map[string]string{"https://example.org/x/Elsewhere": "*"}
	s := depsService(t, quarkWith("App", map[string]string{"Lib": "*"}, "1.0.0"), lib)
	names := planNames(mustPlan(t, s, "App", "").Depends)
	if !reflect.DeepEqual(names, []string{"Elsewhere", "Lib"}) {
		t.Errorf("Depends = %v", names)
	}
}

func TestPlanConflictMessageNamesBothDependents(t *testing.T) {
	s := depsService(t,
		quarkWith("App", map[string]string{"P": "=1.0", "Q": "*"}, "1.0.0"),
		quarkWith("Q", map[string]string{"P": "=2.0"}, "1.0.0"),
		quarkWith("P", nil, "2.0.0", "1.0.0"),
	)
	if err := planErr(t, s, "App", ""); !strings.Contains(err.Error(), "no version satisfies both") {
		t.Errorf("err = %v", err)
	}
}

func unresolvedApp() index.Package {
	app := quarkWith("App", nil, "1.0.0")
	app.DependsUnresolved = map[string]string{"https://example.org/x/Elsewhere": "*"}
	return app
}

func TestPlanUnresolvedURLMalwareIsBlocked(t *testing.T) {
	s := depsService(t, unresolvedApp())
	s.Index.Advisories = []advisory.Advisory{{ID: "SCD-7", Repo: "https://example.org/x/elsewhere.git", Versions: []string{"*"}, Severity: advisory.Malware, Summary: "steals files"}}
	err := planErr(t, s, "App", "")
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("err = %v, want a blocked error", err)
	}
}

func TestPlanUnresolvedURLBrokenNeedsConfirmation(t *testing.T) {
	s := depsService(t, unresolvedApp())
	s.Index.Advisories = []advisory.Advisory{{ID: "SCD-8", Repo: "https://example.org/x/Elsewhere", Versions: []string{"*"}, Severity: advisory.Broken, Summary: "crashes"}}
	d := mustPlan(t, s, "App", "").Depends[0]
	if !reflect.DeepEqual(d.Confirm, []string{ConfirmUnreviewed, ConfirmRetracted}) || d.Retraction == nil || d.Retraction.ID != "SCD-8" {
		t.Errorf("confirm = %v, retraction = %+v", d.Confirm, d.Retraction)
	}
}
