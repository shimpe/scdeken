package install

import (
	"path/filepath"
	"testing"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/scenv"
	"github.com/sc-deken/sc-deken/internal/state"
)

// malwareService has the Evil quark installed, a hand-made copy of its class
// in the extensions folder, and an advisory withdrawing every version.
func malwareService(t *testing.T) (*Service, string) {
	t.Helper()
	s := applyService(t)
	p, _ := s.Index.Find("Evil")
	p.Classes = []string{"EvilThing"}
	s.Index.Advisories = []advisory.Advisory{
		{ID: "SCD-2026-0001", Repo: "https://github.com/example/Evil", Versions: []string{"*"}, Severity: advisory.Malware, Summary: "steals files"},
	}
	s.Ctx.Quarks = []scenv.Quark{{Name: "Evil", URL: p.Repo, Path: filepath.Join(t.TempDir(), "Evil"), Commit: panolaCommit}}
	dup := filepath.Join(s.Ctx.ExtensionDir, "handmade")
	writeTestFile(t, filepath.Join(dup, "Thing.sc"), "EvilThing { }")
	return s, dup
}

func TestCheckAdvisoriesFindsQuarkAndExternal(t *testing.T) {
	s, dup := malwareService(t)
	hits := s.CheckAdvisories()
	if len(hits) != 2 {
		t.Fatalf("hits = %+v", hits)
	}
	sources := map[string]string{}
	for _, h := range hits {
		sources[h.Item.Source] = h.Item.Path
	}
	if sources["quark"] == "" || sources["external"] != dup {
		t.Errorf("sources = %v, want external %s", sources, dup)
	}
}

func TestCheckAdvisoriesFixedVersion(t *testing.T) {
	s := applyService(t)
	installManaged(t, s, "1.2.0")
	setVersions(t, s, "fmx6",
		index.Release{Version: "1.3.0", Commit: "c"},
		index.Release{Version: "1.2.0", Commit: fmx6Commit},
		index.Release{Version: "1.1.0", Commit: "b"})
	s.Index.Advisories = []advisory.Advisory{
		{ID: "SCD-2026-0002", Package: "fmx6", Versions: []string{"1.2.0"}, Severity: advisory.Malware},
	}
	hits := s.CheckAdvisories()
	if len(hits) != 1 || hits[0].FixedVersion != "1.3.0" || hits[0].Item.Version != "1.2.0" {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestKeepSuppresses(t *testing.T) {
	s, _ := malwareService(t)
	for i := 0; i < 2; i++ {
		if err := s.Keep("SCD-2026-0001"); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.State.KeptAdvisories) != 1 {
		t.Errorf("kept = %v", s.State.KeptAdvisories)
	}
	if hits := s.CheckAdvisories(); len(hits) != 0 {
		t.Errorf("hits = %+v", hits)
	}
	saved, err := state.Load(s.Paths.State)
	if err != nil || len(saved.KeptAdvisories) != 1 {
		t.Errorf("saved = %+v, %v", saved, err)
	}
}
