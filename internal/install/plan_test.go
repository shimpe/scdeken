package install

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/state"
)

func mustPlan(t *testing.T, s *Service, name, version string) Plan {
	t.Helper()
	p, err := s.Plan(context.Background(), name, version)
	if err != nil {
		t.Fatalf("Plan(%q): %v", name, err)
	}
	return p
}

func planErr(t *testing.T, s *Service, name, version string) error {
	t.Helper()
	_, err := s.Plan(context.Background(), name, version)
	if err == nil {
		t.Fatalf("Plan(%q) succeeded, want an error", name)
	}
	return err
}

func planNames(plans []Plan) []string {
	var names []string
	for _, p := range plans {
		names = append(names, p.Package)
	}
	return names
}

func TestPlanPrebuilt(t *testing.T) {
	p := mustPlan(t, newService(t), "fmx6", "")
	if p.Route != RoutePrebuilt || p.Build == nil {
		t.Fatalf("route = %q, build = %v", p.Route, p.Build)
	}
	if p.Platform != hostKey() {
		t.Errorf("Platform = %q, want %q", p.Platform, hostKey())
	}
	if len(p.Confirm) != 0 {
		t.Errorf("Confirm = %v, want none", p.Confirm)
	}
	if want := []string{"recompile", "reboot-server"}; !reflect.DeepEqual(p.AfterInstall, want) {
		t.Errorf("AfterInstall = %v, want %v", p.AfterInstall, want)
	}
	if p.Version != "1.0.0" || p.Commit != fmx6Commit || p.IsUpdate {
		t.Errorf("unexpected plan %+v", p)
	}
}

func TestPlanNoBinaryLocalBuild(t *testing.T) {
	s := newService(t)
	s.Ctx.SCArch = "arm64"
	p := mustPlan(t, s, "fmx6", "")
	if p.Route == RoutePrebuilt {
		t.Skip("host OS has a universal build that runs on arm64")
	}
	if p.Route != RouteLocalBuild || !strings.Contains(p.Reason, "arm64-api3") {
		t.Errorf("route = %q, reason = %q", p.Route, p.Reason)
	}
	if p.Doctor == nil {
		t.Error("local build plan has no doctor report")
	}
}

func TestPlanUnknownSCVersionFallsBackToLocalBuild(t *testing.T) {
	s := newService(t)
	s.Ctx.SCVersion = "3.15.0-dev"
	p := mustPlan(t, s, "fmx6", "")
	if p.Route != RouteLocalBuild || p.Platform != "" {
		t.Errorf("route = %q, platform = %q", p.Route, p.Platform)
	}
	if !strings.Contains(p.Reason, "newer than the prebuilt packages know about") {
		t.Errorf("Reason = %q", p.Reason)
	}
}

func TestPlanQuark(t *testing.T) {
	p := mustPlan(t, newService(t), "Panola", "")
	if p.Route != RouteQuark || p.Quark == nil || p.Quark.Commit != panolaCommit {
		t.Fatalf("plan = %+v", p)
	}
	if want := []string{"recompile"}; !reflect.DeepEqual(p.AfterInstall, want) {
		t.Errorf("AfterInstall = %v, want %v", p.AfterInstall, want)
	}
}

func TestPlanPluginQuark(t *testing.T) {
	p := mustPlan(t, newService(t), "hybrid", "")
	if p.Route != RoutePrebuilt || p.Quark == nil || p.Quark.URL == "" {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanUnreviewedNeedsConfirm(t *testing.T) {
	p := mustPlan(t, newService(t), "Wild", "")
	if p.Route != RouteLocalBuild || !reflect.DeepEqual(p.Confirm, []string{"unreviewed"}) {
		t.Errorf("route = %q, confirm = %v", p.Route, p.Confirm)
	}
}

func TestPlanUnstableCountsAsUnreviewed(t *testing.T) {
	s := newService(t)
	pkg, _ := s.Index.Find("Panola")
	pkg.Versions[0].Unstable = true
	p := mustPlan(t, s, "Panola", "")
	if !reflect.DeepEqual(p.Confirm, []string{"unreviewed"}) {
		t.Errorf("Confirm = %v", p.Confirm)
	}
}

func TestPlanMalwareBlocked(t *testing.T) {
	err := planErr(t, newService(t), "Evil", "1.0.0")
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("err = %v, want ErrBlocked", err)
	}
	if want := "Evil 1.0.0 was withdrawn: steals files"; err.Error() != want {
		t.Errorf("message = %q, want %q", err, want)
	}
}

func TestPlanBrokenNeedsConfirm(t *testing.T) {
	p := mustPlan(t, newService(t), "Shaky", "")
	if !reflect.DeepEqual(p.Confirm, []string{"retracted"}) || p.Retraction == nil || p.Retraction.ID != "SCD-2" {
		t.Errorf("confirm = %v, retraction = %+v", p.Confirm, p.Retraction)
	}
}

func TestPlanDependsResolved(t *testing.T) {
	s := newService(t)
	p := mustPlan(t, s, "A", "")
	if names := planNames(p.Depends); !reflect.DeepEqual(names, []string{"C", "B"}) {
		t.Fatalf("Depends = %v, want [C B]", names)
	}
	s.State.Packages["c"] = state.Entry{Name: "C"}
	p = mustPlan(t, s, "A", "")
	if names := planNames(p.Depends); !reflect.DeepEqual(names, []string{"B"}) {
		t.Errorf("with C installed, Depends = %v, want [B]", names)
	}
}

func TestPlanDependsSkipsInstalledQuark(t *testing.T) {
	s := newService(t)
	s.Ctx.Quarks = []scenv.Quark{{Name: "B", URL: "git@github.com:Example/B.git"}}
	p := mustPlan(t, s, "A", "")
	if len(p.Depends) != 0 {
		t.Errorf("Depends = %v, want none (B is installed)", planNames(p.Depends))
	}
}

func TestPlanDependsCycle(t *testing.T) {
	err := planErr(t, newService(t), "Ping", "")
	if !strings.Contains(err.Error(), "Ping → Pong → Ping") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanDependsUnknown(t *testing.T) {
	err := planErr(t, newService(t), "Lost", "")
	if want := `Lost needs "Nowhere", which is not in the package list`; err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestPlanNoRecipe(t *testing.T) {
	err := planErr(t, newService(t), "Raw", "")
	if !strings.Contains(err.Error(), "cannot be installed automatically yet") {
		t.Errorf("err = %v", err)
	}
}

func TestPlanLookupErrors(t *testing.T) {
	s := newService(t)
	if err := planErr(t, s, "nope", ""); err.Error() != `no package named "nope"; try searching` {
		t.Errorf("err = %v", err)
	}
	if err := planErr(t, s, "Panola", "9.9"); err.Error() != `Panola has no version "9.9"` {
		t.Errorf("err = %v", err)
	}
}

func TestPlanIsUpdate(t *testing.T) {
	s := newService(t)
	s.State.Packages["fmx6"] = state.Entry{Name: "fmx6"}
	if !mustPlan(t, s, "fmx6", "").IsUpdate {
		t.Error("IsUpdate false with the package in state")
	}
	s.Ctx.Quarks = []scenv.Quark{{Name: "Panola", URL: "https://github.com/example/panola.git"}}
	if !mustPlan(t, s, "Panola", "").IsUpdate {
		t.Error("IsUpdate false with the quark installed")
	}
}

func TestPlanCarriesRepoForPackageAndDependencies(t *testing.T) {
	s := newService(t)
	p := mustPlan(t, s, "A", "")
	if p.Repo == "" {
		t.Fatal("plan has no repo")
	}
	for _, d := range p.Depends {
		if d.Repo == "" {
			t.Errorf("dependency %s has no repo", d.Package)
		}
	}
	if wild := mustPlan(t, s, "Wild", ""); wild.Repo == "" {
		t.Error("unreviewed plan has no repo")
	}
}
