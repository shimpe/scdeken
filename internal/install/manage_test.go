package install

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/state"
)

// setVersions replaces the releases of the named package.
func setVersions(t *testing.T, s *Service, name string, rels ...index.Release) {
	t.Helper()
	p, ok := s.Index.Find(name)
	if !ok {
		t.Fatalf("no package %s", name)
	}
	p.Versions = rels
}

// installManaged puts a fake managed fmx6 on disk and in the state file.
func installManaged(t *testing.T, s *Service, version string) string {
	t.Helper()
	dir := filepath.Join(s.Paths.Managed, "fmx6")
	writeTestFile(t, filepath.Join(dir, "FMX6.scx"), "plugin")
	s.State.Packages["fmx6"] = state.Entry{
		Name: "fmx6", Version: version, Commit: fmx6Commit, Kind: "plugin",
		Path: dir, Route: state.Prebuilt, Repo: "https://github.com/example/fmx6",
	}
	if err := s.State.Save(s.Paths.State); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestListMergesStateAndQuarks(t *testing.T) {
	s := applyService(t)
	installManaged(t, s, "1.0.0")
	setVersions(t, s, "Panola",
		index.Release{Version: "1.1.0", Commit: panolaCommit},
		index.Release{Version: "1.0.0", Commit: "9999999999999999999999999999999999999999"})
	s.Ctx.Quarks = []scenv.Quark{{Name: "Panola", URL: "https://github.com/example/Panola", Path: "/q/Panola", Commit: panolaCommit[:10]}}

	items := s.List()
	if len(items) != 2 || items[0].Name != "fmx6" || items[1].Name != "Panola" {
		t.Fatalf("items = %+v", items)
	}
	if q := items[1]; q.Version != "1.1.0" || q.Source != "quark" || q.Kind != "quark" {
		t.Errorf("quark = %+v", q)
	}
	if m := items[0]; m.Source != "managed" || m.Trust != "reviewed" {
		t.Errorf("managed = %+v", m)
	}
}

func TestUpdatesAvailable(t *testing.T) {
	s := applyService(t)
	installManaged(t, s, "0.2.0")
	setVersions(t, s, "fmx6", index.Release{Version: "0.3.0", Commit: fmx6Commit}, index.Release{Version: "0.2.0", Commit: "x"})
	up := s.Updates()
	if len(up) != 1 || up[0].Latest != "0.3.0" || !up[0].UpdateAvailable {
		t.Fatalf("updates = %+v", up)
	}
	s.State.Packages["fmx6"] = state.Entry{Name: "fmx6", Route: state.External, Path: "/x"}
	if up := s.Updates(); len(up) != 0 {
		t.Errorf("unknown version must not update: %+v", up)
	}
}

func TestRemoveManaged(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	if err := s.Remove("FMX6"); err != nil {
		t.Fatal(err)
	}
	if exists(dir) || exists(filepath.Join(filepath.Dir(dir), ".removing-fmx6")) || len(s.State.Packages) != 0 {
		t.Error("folder or state entry left behind")
	}
	saved, err := state.Load(s.Paths.State)
	if err != nil || len(saved.Packages) != 0 {
		t.Errorf("saved state = %+v, %v", saved, err)
	}
}

func TestRemoveRefusals(t *testing.T) {
	s := applyService(t)
	s.Ctx.Quarks = []scenv.Quark{{Name: "Panola"}}
	if err := s.Remove("Panola"); err == nil || !strings.Contains(err.Error(), "Deken.remove") {
		t.Errorf("quark err = %v", err)
	}
	s.State.Packages["hand"] = state.Entry{Name: "hand", Route: state.External, Path: "/ext/hand"}
	if err := s.Remove("hand"); err == nil || !strings.Contains(err.Error(), "installed outside sc-deken") {
		t.Errorf("external err = %v", err)
	}
}

func TestRemoveInUseMessage(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	real := rename
	rename = func(from, to string) error { return lockedError() }
	t.Cleanup(func() { rename = real })

	err := s.Remove("fmx6")
	if !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), "s.quit") {
		t.Fatalf("err = %v", err)
	}
	if !exists(filepath.Join(dir, "FMX6.scx")) || len(s.State.Packages) != 1 {
		t.Error("folder and state must be intact")
	}
}

func TestQuarantineMoves(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	got, err := s.Quarantine(dir, "SCD-2026-0001")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(s.Paths.Quarantine, "SCD-2026-0001", "fmx6")
	if got != want || !exists(filepath.Join(want, "FMX6.scx")) || exists(dir) {
		t.Errorf("got %s; moved files missing", got)
	}
	if len(s.State.Packages) != 0 {
		t.Error("state entry kept")
	}
	if err := s.ClearQuarantine(); err != nil || exists(s.Paths.Quarantine) {
		t.Errorf("clear: %v", err)
	}
}

func TestQuarantineRejectsBadID(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	for _, id := range []string{`..\..\x`, "../x", "", "SCD-1"} {
		if _, err := s.Quarantine(dir, id); err == nil || !strings.Contains(err.Error(), "not a valid advisory id") {
			t.Errorf("id %q: err = %v", id, err)
		}
	}
	if !exists(dir) || exists(s.Paths.Quarantine) {
		t.Error("nothing may move for a bad id")
	}
}

func TestRemoveKeepsWhatAnotherProcessSaved(t *testing.T) {
	s := applyService(t)
	installManaged(t, s, "1.0.0")
	saveByAnotherProcess(t, s, "other")
	if err := s.Remove("fmx6"); err != nil {
		t.Fatal(err)
	}
	saved, err := state.Load(s.Paths.State)
	if _, ok := saved.Packages["other"]; err != nil || !ok || len(saved.Packages) != 1 {
		t.Errorf("saved = %+v, %v", saved, err)
	}
}

func TestQuarantineKeepsWhatAnotherProcessSaved(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	saveByAnotherProcess(t, s, "other")
	if _, err := s.Quarantine(dir, "SCD-2026-0001"); err != nil {
		t.Fatal(err)
	}
	saved, err := state.Load(s.Paths.State)
	if _, ok := saved.Packages["other"]; err != nil || !ok || len(saved.Packages) != 1 {
		t.Errorf("saved = %+v, %v", saved, err)
	}
}
