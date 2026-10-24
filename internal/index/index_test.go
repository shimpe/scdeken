package index

import (
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
)

// fixture builds the index shared by the index and search tests.
func fixture() *Index {
	return &Index{
		Schema: SchemaVersion,
		Packages: []Package{
			{Name: "fmx6", Summary: "Six operator synth", Repo: "https://github.com/a/fmx6",
				Kind: recipe.KindPlugin, Trust: Reviewed, Stars: 10,
				Versions: []Release{{Version: "1.0.0", Commit: "aaaaaaa1"}}},
			{Name: "sc3-plugins", Summary: "Community UGens", Repo: "https://github.com/b/sc3-plugins",
				Kind: recipe.KindPlugin, Trust: Reviewed, Stars: 500, UGens: []string{"FM7", "Gendy"},
				Versions: []Release{{Version: "3.13.0", Commit: "bbbbbbb1"}}},
			{Name: "Panola", Summary: "Pattern notation", Repo: "https://github.com/c/panola",
				Kind: recipe.KindQuark, Trust: Reviewed, Stars: 20, Classes: []string{"Panola"},
				Versions: []Release{{Version: "0.5.0", Commit: "ccccccc1"}}},
			{Name: "fm-thing", Summary: "Experiment", Repo: "https://github.com/d/fm-thing",
				Kind: recipe.KindPlugin, Trust: Unreviewed, Stars: 1,
				Versions: []Release{{Version: "0.1.0", Commit: "ddddddd1"}}},
			{Name: "presetter", Summary: "Utilities for FM synthesis presets", Repo: "https://github.com/e/presetter",
				Kind: recipe.KindQuark, Trust: Community, Stars: 5,
				Versions: []Release{{Version: "2.0.0", Commit: "eeeeeee1"}}},
		},
	}
}

func TestDecodeRejectsNewerSchema(t *testing.T) {
	_, err := Decode([]byte(`{"schema":2}`))
	if err == nil || !strings.Contains(err.Error(), "newer than this sc-deken") {
		t.Fatalf("got %v, want newer-schema error", err)
	}
}

func TestEncodeDecodeRoundTripSortsPackages(t *testing.T) {
	data, err := fixture().Encode()
	if err != nil {
		t.Fatal(err)
	}
	ix, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if ix.Packages[0].Name != "fm-thing" || ix.Packages[1].Name != "fmx6" {
		t.Errorf("packages not sorted by lowercase name: %v, %v", ix.Packages[0].Name, ix.Packages[1].Name)
	}
}

func TestFindCaseInsensitive(t *testing.T) {
	ix := fixture()
	p, ok := ix.Find("PANOLA")
	if !ok || p.Name != "Panola" {
		t.Fatalf("Find(PANOLA) = %v, %v", p, ok)
	}
	if _, ok := ix.Find("nope"); ok {
		t.Error("found a package that does not exist")
	}
}

func TestFindRepoNormalizes(t *testing.T) {
	p, ok := fixture().FindRepo("https://github.com/C/Panola.git")
	if !ok || p.Name != "Panola" {
		t.Fatalf("FindRepo = %v, %v", p, ok)
	}
}

func malwareIndex() (*Index, *Package) {
	ix := fixture()
	p := &ix.Packages[0]
	p.Versions = []Release{{Version: "1.1.0", Commit: "1111111a"}, {Version: "1.0.0", Commit: "aaaaaaa1"}}
	ix.Advisories = []advisory.Advisory{
		{ID: "SCD-2026-0001", Package: "fmx6", Versions: []string{"1.1.0"}, Severity: advisory.Malware},
	}
	return ix, p
}

func TestLatestSkipsMalwareRetracted(t *testing.T) {
	ix, p := malwareIndex()
	r, ok := ix.Latest(p)
	if !ok || r.Version != "1.0.0" {
		t.Fatalf("Latest = %v, %v; want 1.0.0", r, ok)
	}
}

func TestLatestPrefersStableThenUnstable(t *testing.T) {
	ix := fixture()
	p := &ix.Packages[0]
	p.Versions = []Release{{Version: "2.0.0-rc1", Unstable: true}, {Version: "1.0.0"}}
	if r, _ := ix.Latest(p); r.Version != "1.0.0" {
		t.Errorf("Latest = %s, want stable 1.0.0", r.Version)
	}
	p.Versions = []Release{{Version: "2.0.0-rc1", Unstable: true}}
	if r, _ := ix.Latest(p); r.Version != "2.0.0-rc1" {
		t.Errorf("Latest = %s, want unstable fallback", r.Version)
	}
	p.Versions = nil
	if _, ok := ix.Latest(p); ok {
		t.Error("Latest found a release in an empty package")
	}
}

func TestReleaseEmptyMeansLatest(t *testing.T) {
	ix, p := malwareIndex()
	if r, ok := ix.Release(p, ""); !ok || r.Version != "1.0.0" {
		t.Errorf("Release(empty) = %v, %v", r, ok)
	}
	if r, ok := ix.Release(p, "1.1.0"); !ok || r.Version != "1.1.0" {
		t.Errorf("Release(1.1.0) = %v, %v", r, ok)
	}
	if _, ok := ix.Release(p, "9.9.9"); ok {
		t.Error("found a release that does not exist")
	}
}

func TestRetractionPicksMostSevere(t *testing.T) {
	ix, p := malwareIndex()
	ix.Advisories = append([]advisory.Advisory{
		{ID: "SCD-2026-0002", Package: "fmx6", Versions: []string{"*"}, Severity: advisory.Broken},
	}, ix.Advisories...)
	a, ok := ix.Retraction(p, &p.Versions[0])
	if !ok || a.ID != "SCD-2026-0001" {
		t.Fatalf("Retraction = %v, %v; want the malware advisory", a, ok)
	}
	other := &ix.Packages[2]
	if _, ok := ix.Retraction(other, &other.Versions[0]); ok {
		t.Error("unaffected release reported as retracted")
	}
}

func TestBuildForUniversal(t *testing.T) {
	r := Release{Builds: []Build{
		{Platform: "garbage"},
		{Platform: "linux-x86_64-api3"},
		{Platform: "macos-universal-api3", URL: "u"},
	}}
	b, ok := r.BuildFor(platform.Key{OS: "macos", Arch: "arm64", API: 3})
	if !ok || b.Platform != "macos-universal-api3" {
		t.Fatalf("BuildFor = %v, %v", b, ok)
	}
	if _, ok := r.BuildFor(platform.Key{OS: "windows", Arch: "x86_64", API: 3}); ok {
		t.Error("found a build for a platform with none")
	}
}
