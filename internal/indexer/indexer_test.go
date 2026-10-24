package indexer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/discover"
	"github.com/shimpe/scdeken/internal/github"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
)

const (
	sha0 = "0000000000000000000000000000000000000000"
	sha1 = "1111111111111111111111111111111111111111"
	sha9 = "9999999999999999999999999999999999999999"
)

// fakeScanner answers from fixed tables and counts scans.
type fakeScanner struct {
	refs    map[string]fakeRefs
	scans   map[string]discover.Scan
	scanErr map[string]error
	scanned []string
}

type fakeRefs struct {
	head string
	tags map[string]string
}

func (f *fakeScanner) Refs(_ context.Context, url string) (string, map[string]string, error) {
	r, ok := f.refs[url]
	if !ok {
		return "", nil, errors.New("no such repository")
	}
	return r.head, r.tags, nil
}

func (f *fakeScanner) Scan(_ context.Context, url, _ string) (discover.Scan, error) {
	f.scanned = append(f.scanned, url)
	if err := f.scanErr[url]; err != nil {
		return discover.Scan{}, err
	}
	return f.scans[url], nil
}

const fmxURL = "https://github.com/someone/fmx6"

func fmxRecipe() recipe.Recipe {
	return recipe.Recipe{
		Name: "fmx6", Summary: "Six operator FM", Repo: fmxURL, License: "GPL-3.0", Kind: recipe.KindPlugin,
		Build:    &recipe.Build{System: "cmake", SCPathVar: "SC_PATH"},
		Versions: recipe.Versions{Approved: map[string]string{"0.3.0": sha1, "0.2.0": sha0}},
	}
}

func build(t *testing.T, s Scanner, in Inputs) *index.Index {
	t.Helper()
	in.Now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ix, err := Build(context.Background(), s, in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ix
}

func pkgNamed(t *testing.T, ix *index.Index, name string) *index.Package {
	t.Helper()
	p, ok := ix.Find(name)
	if !ok {
		t.Fatalf("package %s missing from %d packages", name, len(ix.Packages))
	}
	return p
}

func releaseWithAsset(tag string, names ...string) github.Release {
	r := github.Release{Tag: tag}
	for _, n := range names {
		r.Assets = append(r.Assets, github.Asset{Name: n, URL: "https://example.org/" + tag + "/" + n, Size: 42})
	}
	return r
}

func TestBuildReviewedWithReleases(t *testing.T) {
	ix := build(t, &fakeScanner{}, Inputs{
		Recipes:  []recipe.Recipe{fmxRecipe()},
		Releases: []github.Release{releaseWithAsset("fmx6-0.3.0", "windows-x86_64-api3.zip", "windows-x86_64-api3.files.txt")},
		Sums:     map[string]map[string]string{"fmx6-0.3.0": {"windows-x86_64-api3.zip": "abc123"}},
		Files: map[string]map[string][]string{"fmx6-0.3.0": {
			"windows-x86_64-api3": {"FMX6.scx"}, "linux-x86_64-api3": {"FMX6.so", "FMX6.scx"},
		}},
	})
	p := pkgNamed(t, ix, "fmx6")
	if p.Trust != index.Reviewed || len(p.Versions) != 2 || p.Versions[0].Version != "0.3.0" {
		t.Fatalf("package = %+v", p)
	}
	want := index.Build{Platform: "windows-x86_64-api3", URL: "https://example.org/fmx6-0.3.0/windows-x86_64-api3.zip", SHA256: "abc123", Size: 42}
	if got := p.Versions[0].Builds; len(got) != 1 || got[0] != want {
		t.Errorf("builds = %+v", got)
	}
	if len(p.Versions[1].Builds) != 0 {
		t.Errorf("0.2.0 has no release but builds = %+v", p.Versions[1].Builds)
	}
	if got := strings.Join(p.PluginFiles, ","); got != "FMX6.scx,FMX6.so" {
		t.Errorf("PluginFiles = %q", got)
	}
	if ix.Schema != index.SchemaVersion || ix.Generated.IsZero() {
		t.Errorf("header = %d %v", ix.Schema, ix.Generated)
	}
}

func TestBuildSkipsAssetWithoutSum(t *testing.T) {
	var log bytes.Buffer
	ix := build(t, &fakeScanner{}, Inputs{
		Recipes:  []recipe.Recipe{fmxRecipe()},
		Releases: []github.Release{releaseWithAsset("fmx6-0.3.0", "windows-x86_64-api3.zip", "linux-x86_64-api3.zip", "notes.zip")},
		Sums:     map[string]map[string]string{"fmx6-0.3.0": {"linux-x86_64-api3.zip": "def456"}},
		Log:      &log,
	})
	builds := pkgNamed(t, ix, "fmx6").Versions[0].Builds
	if len(builds) != 1 || builds[0].Platform != "linux-x86_64-api3" {
		t.Errorf("builds = %+v", builds)
	}
	if !strings.Contains(log.String(), "windows-x86_64-api3.zip") {
		t.Errorf("log does not mention the skipped asset:\n%s", log.String())
	}
}

func TestBuildIgnoresDraftReleases(t *testing.T) {
	draft := releaseWithAsset("fmx6-0.3.0", "linux-x86_64-api3.zip")
	draft.Draft = true
	ix := build(t, &fakeScanner{}, Inputs{
		Recipes:  []recipe.Recipe{fmxRecipe()},
		Releases: []github.Release{draft},
		Sums:     map[string]map[string]string{"fmx6-0.3.0": {"linux-x86_64-api3.zip": "aa"}},
	})
	if got := pkgNamed(t, ix, "fmx6").Versions[0].Builds; len(got) != 0 {
		t.Errorf("draft release produced builds: %+v", got)
	}
}

func TestBuildMalwareVersionHasNoBuilds(t *testing.T) {
	ix := build(t, &fakeScanner{}, Inputs{
		Recipes: []recipe.Recipe{fmxRecipe()},
		Releases: []github.Release{
			releaseWithAsset("fmx6-0.3.0", "linux-x86_64-api3.zip"), releaseWithAsset("fmx6-0.2.0", "linux-x86_64-api3.zip"),
		},
		Sums: map[string]map[string]string{
			"fmx6-0.3.0": {"linux-x86_64-api3.zip": "aa"}, "fmx6-0.2.0": {"linux-x86_64-api3.zip": "bb"},
		},
		Advisories: []advisory.Advisory{{ID: "SCD-2026-0001", Package: "fmx6", Versions: []string{"0.3.0"}, Severity: advisory.Malware}},
	})
	p := pkgNamed(t, ix, "fmx6")
	if len(p.Versions) != 2 || len(p.Versions[0].Builds) != 0 || len(p.Versions[1].Builds) != 1 {
		t.Errorf("versions = %+v", p.Versions)
	}
	if len(ix.Advisories) != 1 {
		t.Errorf("advisories not copied: %+v", ix.Advisories)
	}
}

func TestBuildDedupesTrust(t *testing.T) {
	s := &fakeScanner{refs: map[string]fakeRefs{fmxURL: {head: sha9}}}
	ix := build(t, s, Inputs{
		Recipes:   []recipe.Recipe{fmxRecipe()},
		Directory: []discover.Entry{{Name: "FMX6", URL: fmxURL + ".git"}},
		Topics:    []github.Repo{{HTMLURL: "https://github.com/Someone/FMX6/"}},
	})
	if len(ix.Packages) != 1 || ix.Packages[0].Trust != index.Reviewed {
		t.Fatalf("packages = %+v", ix.Packages)
	}
}

func TestBuildCommunityUntaggedIsUnstable(t *testing.T) {
	s := &fakeScanner{refs: map[string]fakeRefs{"https://github.com/a/q": {head: "abcdef0123456789abcdef0123456789abcdef01"}}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Q", URL: "https://github.com/a/q"}}})
	p := pkgNamed(t, ix, "Q")
	v := p.Versions[0]
	if v.Version != "0.0.0+abcdef0" || !v.Unstable || v.Commit != "abcdef0123456789abcdef0123456789abcdef01" {
		t.Errorf("version = %+v", v)
	}
	if p.Trust != index.Community || p.Kind != recipe.KindQuark {
		t.Errorf("package = %+v", p)
	}
}

func TestBuildTaggedVersionsNewestFirst(t *testing.T) {
	s := &fakeScanner{refs: map[string]fakeRefs{"https://github.com/a/q": {head: sha9, tags: map[string]string{
		"v1.2": sha1, "1.10": sha0, "latest": sha9,
	}}}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Q", URL: "https://github.com/a/q"}}})
	vs := pkgNamed(t, ix, "Q").Versions
	if len(vs) != 2 || vs[0].Version != "1.10" || vs[1].Version != "1.2" || vs[1].Commit != sha1 {
		t.Errorf("versions = %+v", vs)
	}
}

func TestBuildUnreviewedDraftBuild(t *testing.T) {
	url := "https://github.com/a/plug"
	s := &fakeScanner{
		refs:  map[string]fakeRefs{url: {head: sha9}},
		scans: map[string]discover.Scan{url: {Kind: recipe.KindPlugin, CMakeDir: "src", SCPathVar: "SC_PATH", Classes: []string{"Foo"}, UGens: []string{"Bar"}}},
	}
	ix := build(t, s, Inputs{Topics: []github.Repo{{HTMLURL: url, Description: "A plug", Stars: 7}}})
	p := pkgNamed(t, ix, "plug")
	if p.Trust != index.Unreviewed || p.Build == nil || p.Build.Subdir != "src" || p.Summary != "A plug" || p.Stars != 7 {
		t.Errorf("package = %+v", p)
	}
	if p.ScannedCommit != sha9 || len(p.Classes) != 1 || len(p.UGens) != 1 {
		t.Errorf("scan results missing: %+v", p)
	}
}

func TestBuildSkipsArchivedUnlessReviewed(t *testing.T) {
	arch := "https://github.com/a/old"
	s := &fakeScanner{refs: map[string]fakeRefs{arch: {head: sha9}, fmxURL: {head: sha9}}}
	ix := build(t, s, Inputs{
		Recipes:   []recipe.Recipe{fmxRecipe()},
		Directory: []discover.Entry{{Name: "Old", URL: arch}},
		Meta: map[string]github.Repo{
			arch: {Archived: true}, fmxURL: {Archived: true, Stars: 3},
		},
	})
	if len(ix.Packages) != 1 || ix.Packages[0].Name != "fmx6" || ix.Packages[0].Stars != 3 {
		t.Errorf("packages = %+v", ix.Packages)
	}
}

func previousWith(url string) *index.Index {
	return &index.Index{Schema: 1, Packages: []index.Package{{
		Name: "Q", Repo: url, Kind: recipe.KindPlugin, Trust: index.Community, ScannedCommit: sha9, ScanFormat: index.ScanFormat,
		Summary: "stale summary", License: "STALE", QuarkSummary: "quark summary", LicenseFile: "MIT", Classes: []string{"Cached"}, UGens: []string{"CachedUGen"},
		Build:    &recipe.Build{System: "cmake", SCPathVar: "SC_PATH"},
		Versions: []index.Release{{Version: "9.9.9", Commit: sha0, Builds: []index.Build{{Platform: "linux-x86_64-api3", SHA256: "evil"}}}},
	}}}
}

func TestBuildReusesPreviousScan(t *testing.T) {
	url := "https://github.com/a/q"
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha9, tags: map[string]string{"1.0": sha1}}}}
	ix := build(t, s, Inputs{
		Directory: []discover.Entry{{Name: "Q", URL: url}},
		Previous:  previousWith(url),
	})
	if len(s.scanned) != 0 {
		t.Errorf("scanned %v although the previous index knew this commit", s.scanned)
	}
	p := pkgNamed(t, ix, "Q")
	if len(p.Classes) != 1 || p.Classes[0] != "Cached" || p.Kind != recipe.KindPlugin || p.Build == nil {
		t.Errorf("cached scan not reused: %+v", p)
	}
	if p.Summary != "quark summary" || p.License != "MIT" {
		t.Errorf("summary and license must be worked out again from the scan findings: %q, %q", p.Summary, p.License)
	}
	if len(p.Versions) != 1 || p.Versions[0].Version != "1.0" || len(p.Versions[0].Builds) != 0 {
		t.Errorf("versions must come from upstream tags only: %+v", p.Versions)
	}
}

func TestBuildRescansWhenHeadMoved(t *testing.T) {
	url := "https://github.com/a/q"
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha1}}, scans: map[string]discover.Scan{url: {Classes: []string{"Fresh"}}}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Q", URL: url}}, Previous: previousWith(url)})
	if len(s.scanned) != 1 || pkgNamed(t, ix, "Q").Classes[0] != "Fresh" {
		t.Errorf("scanned %v, package %+v", s.scanned, ix.Packages)
	}
}

func TestBuildSurvivesScanFailure(t *testing.T) {
	good, bad := "https://github.com/a/good", "https://github.com/a/bad"
	s := &fakeScanner{
		refs:    map[string]fakeRefs{good: {head: sha1}, bad: {head: sha9}},
		scans:   map[string]discover.Scan{good: {Classes: []string{"Good"}, Kind: recipe.KindQuark}},
		scanErr: map[string]error{bad: errors.New("clone failed")},
	}
	var log bytes.Buffer
	ix := build(t, s, Inputs{
		Directory: []discover.Entry{{Name: "Bad", URL: bad}, {Name: "Good", URL: good}},
		Meta:      map[string]github.Repo{bad: {Description: "still listed"}},
		Log:       &log,
	})
	if len(ix.Packages) != 2 {
		t.Fatalf("packages = %+v", ix.Packages)
	}
	p := pkgNamed(t, ix, "Bad")
	if len(p.Classes) != 0 || p.ScannedCommit != "" || p.Summary != "still listed" || len(p.Versions) != 1 {
		t.Errorf("bad package = %+v", p)
	}
	if !strings.Contains(log.String(), "clone failed") {
		t.Errorf("failure not logged:\n%s", log.String())
	}
}

func TestBuildSkipsUnreachableRepository(t *testing.T) {
	ix := build(t, &fakeScanner{}, Inputs{Directory: []discover.Entry{{Name: "Gone", URL: "https://github.com/a/gone"}}})
	if len(ix.Packages) != 0 {
		t.Errorf("packages = %+v", ix.Packages)
	}
}

func TestSummaryPrecedence(t *testing.T) {
	rURL, qURL, tURL := fmxURL, "https://github.com/a/quarky", "https://github.com/a/topical"
	s := &fakeScanner{
		refs: map[string]fakeRefs{qURL: {head: sha1}, tURL: {head: sha1}},
		scans: map[string]discover.Scan{
			qURL: {Kind: recipe.KindQuark, Quark: discover.QuarkFile{Summary: "from quark file"}},
			tURL: {Kind: recipe.KindQuark},
		},
	}
	ix := build(t, s, Inputs{
		Recipes:   []recipe.Recipe{fmxRecipe()},
		Directory: []discover.Entry{{Name: "Quarky", URL: qURL}},
		Topics:    []github.Repo{{HTMLURL: tURL, Description: "from GitHub"}},
		Meta: map[string]github.Repo{
			rURL: {Description: "GitHub says otherwise"}, qURL: {Description: "GitHub too"},
		},
	})
	for name, want := range map[string]string{"fmx6": "Six operator FM", "Quarky": "from quark file", "topical": "from GitHub"} {
		if got := pkgNamed(t, ix, name).Summary; got != want {
			t.Errorf("%s summary = %q, want %q", name, got, want)
		}
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func recipeText(name string, versions ...string) string {
	text := "name = \"" + name + "\"\nsummary = \"s\"\nrepo = \"https://github.com/a/" + name + "\"\nkind = \"quark\"\n\n[versions]\ntag_prefix = \"\"\n\n[versions.approved]\n"
	for _, v := range versions {
		text += "\"" + v + "\" = \"" + sha1 + "\"\n"
	}
	return text
}

func TestCIMatrix(t *testing.T) {
	repo := t.TempDir()
	recipes := filepath.Join(repo, "recipes")
	if err := os.Mkdir(recipes, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(recipes, name+".toml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "--quiet")
	gitIn(t, repo, "config", "user.email", "t@example.org")
	gitIn(t, repo, "config", "user.name", "t")
	gitIn(t, repo, "config", "commit.gpgsign", "false")
	write("x", recipeText("x", "1.0"))
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "--quiet", "-m", "base")
	gitIn(t, repo, "tag", "base")
	write("x", recipeText("x", "1.0", "1.1"))
	write("y", recipeText("y", "2.0"))

	rows, err := Matrix(context.Background(), run.Exec{}, recipes, "base")
	if err != nil {
		t.Fatalf("Matrix: %v", err)
	}
	if len(rows) != 2 || rows[0].Package != "x" || rows[0].Version != "1.1" || rows[1].Package != "y" || rows[1].Version != "2.0" {
		t.Fatalf("rows = %+v", rows)
	}
	if !strings.HasSuffix(rows[0].Recipe, "recipes/x.toml") {
		t.Errorf("recipe path = %q", rows[0].Recipe)
	}
	if _, err := Matrix(context.Background(), run.Exec{}, recipes, "no-such-ref"); err == nil {
		t.Error("an unknown base revision was accepted")
	}
}

func TestBuildPreviousDoesNotFreezeMetadata(t *testing.T) {
	url := "https://github.com/a/q"
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha9}}}
	prev := previousWith(url)
	prev.Packages[0].QuarkSummary = ""
	ix := build(t, s, Inputs{
		Directory: []discover.Entry{{Name: "Q", URL: url}},
		Meta:      map[string]github.Repo{url: {Description: "new description", License: "Apache-2.0"}},
		Previous:  prev,
	})
	p := pkgNamed(t, ix, "Q")
	if len(s.scanned) != 0 || p.Summary != "new description" || p.License != "Apache-2.0" {
		t.Errorf("scanned %v, summary %q, license %q", s.scanned, p.Summary, p.License)
	}
}

func TestBuildReviewedKeepsRecipeValuesOverPrevious(t *testing.T) {
	r := fmxRecipe()
	prev := &index.Index{Schema: 1, Packages: []index.Package{{
		Name: "fmx6", Repo: fmxURL, Kind: recipe.KindQuark, Trust: index.Community, Summary: "old", ScannedCommit: sha1, ScanFormat: index.ScanFormat,
		Classes: []string{"Cached"}, Build: &recipe.Build{System: "cmake", Subdir: "evil"},
	}}}
	ix := build(t, &fakeScanner{}, Inputs{Recipes: []recipe.Recipe{r}, Previous: prev})
	p := pkgNamed(t, ix, "fmx6")
	if p.Kind != recipe.KindPlugin || p.Summary != "Six operator FM" || p.Build.Subdir != "" || p.Trust != index.Reviewed {
		t.Errorf("recipe values lost: %+v", p)
	}
	if len(p.Classes) != 1 || p.Classes[0] != "Cached" {
		t.Errorf("classes were not reused: %+v", p.Classes)
	}
}

func TestBuildTopicRepoKeepsKindFromTopics(t *testing.T) {
	both, quark, other := "https://github.com/a/both", "https://github.com/a/quarky", "https://github.com/a/other"
	s := &fakeScanner{
		refs:    map[string]fakeRefs{both: {head: sha1}, quark: {head: sha1}, other: {head: sha1}},
		scanErr: map[string]error{both: errors.New("clone failed")},
		scans:   map[string]discover.Scan{quark: {}}, // scanned fine but nothing recognisable
	}
	ix := build(t, s, Inputs{Topics: []github.Repo{
		{HTMLURL: both, Description: "both kinds", Topics: []string{"supercollider-ugens", "supercollider-quark"}},
		{HTMLURL: quark, Description: "a quark", Topics: []string{"supercollider-quark"}},
		{HTMLURL: other, Topics: []string{"audio"}},
	}})
	if len(ix.Packages) != 2 {
		t.Fatalf("packages = %+v", ix.Packages)
	}
	p := pkgNamed(t, ix, "both")
	if p.Kind != recipe.KindPluginQuark || p.Build != nil || p.Summary != "both kinds" || p.Trust != index.Unreviewed {
		t.Errorf("both = %+v", p)
	}
	if q := pkgNamed(t, ix, "quarky"); q.Kind != recipe.KindQuark {
		t.Errorf("quarky = %+v", q)
	}
}

// quarkScan is a scan of a quark with the given dependencies.
func quarkScan(deps ...discover.Dependency) discover.Scan {
	return discover.Scan{Kind: recipe.KindQuark, HasQuark: true, Quark: discover.QuarkFile{Dependencies: deps}}
}

func TestBuildResolvesDependencies(t *testing.T) {
	const (
		aURL = "https://github.com/a/alpha"
		bURL = "https://github.com/b/beta"
		cURL = "https://github.com/c/gamma"
	)
	s := &fakeScanner{
		refs: map[string]fakeRefs{aURL: {head: sha9}, bURL: {head: sha9}, cURL: {head: sha9}},
		scans: map[string]discover.Scan{
			aURL: quarkScan(
				discover.Dependency{Name: "BETA"},
				discover.Dependency{Name: "gamma", URL: "git://github.com/C/Gamma.git", Refspec: "tags/1.2"},
				discover.Dependency{Name: "elsewhere", URL: "https://example.org/x/elsewhere", Refspec: "tags/2.0"},
				discover.Dependency{Name: "Missing", Refspec: "main"},
			),
			bURL: quarkScan(),
			cURL: quarkScan(),
		},
	}
	r := fmxRecipe()
	r.Depends = map[string]string{"https://github.com/b/beta.git": ">=1.0", "fmx6x": "*"}
	ix := build(t, s, Inputs{
		Recipes: []recipe.Recipe{r},
		Directory: []discover.Entry{
			{Name: "Alpha", URL: aURL}, {Name: "Beta", URL: bURL}, {Name: "Gamma", URL: cURL},
		},
	})
	alpha := pkgNamed(t, ix, "Alpha")
	wantDepends := map[string]string{"Beta": "*", "Gamma": "=1.2", "Missing": "*"}
	if !reflect.DeepEqual(alpha.Depends, wantDepends) {
		t.Errorf("Alpha depends = %v, want %v", alpha.Depends, wantDepends)
	}
	wantUnresolved := map[string]string{"https://example.org/x/elsewhere": "=2.0"}
	if !reflect.DeepEqual(alpha.DependsUnresolved, wantUnresolved) {
		t.Errorf("Alpha unresolved = %v, want %v", alpha.DependsUnresolved, wantUnresolved)
	}
	fmx := pkgNamed(t, ix, "fmx6")
	if !reflect.DeepEqual(fmx.Depends, map[string]string{"Beta": ">=1.0", "fmx6x": "*"}) || fmx.DependsUnresolved != nil {
		t.Errorf("fmx6 depends = %v, unresolved = %v", fmx.Depends, fmx.DependsUnresolved)
	}
	if !reflect.DeepEqual(alpha.QuarkDeps, []string{"BETA", "git://github.com/C/Gamma.git@tags/1.2", "https://example.org/x/elsewhere@tags/2.0", "Missing@main"}) {
		t.Errorf("QuarkDeps = %v", alpha.QuarkDeps)
	}
}

func TestBuildCachedScanKeepsDependencies(t *testing.T) {
	url := "https://github.com/a/q"
	prev := previousWith(url)
	prev.Packages[0].QuarkDeps = []string{"Other@tags/1.0", "https://example.org/z/Zed"}
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha9}}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Q", URL: url}}, Previous: prev})
	if len(s.scanned) != 0 {
		t.Fatalf("scanned %v", s.scanned)
	}
	p := pkgNamed(t, ix, "Q")
	if !reflect.DeepEqual(p.Depends, map[string]string{"Other": "=1.0"}) ||
		!reflect.DeepEqual(p.DependsUnresolved, map[string]string{"https://example.org/z/Zed": "*"}) {
		t.Errorf("depends = %v, unresolved = %v", p.Depends, p.DependsUnresolved)
	}
}

func TestBuildRescansWhenPreviousScanFormatIsOlder(t *testing.T) {
	url := "https://github.com/a/q"
	prev := previousWith(url)
	prev.Packages[0].ScanFormat = 0 // written before dependencies were recorded
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha9}}, scans: map[string]discover.Scan{url: quarkScan(discover.Dependency{Name: "Other"})}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Q", URL: url}}, Previous: prev})
	if len(s.scanned) != 1 {
		t.Fatalf("scanned %v, want a rescan", s.scanned)
	}
	p := pkgNamed(t, ix, "Q")
	if p.ScanFormat != index.ScanFormat || p.Depends["Other"] != "*" {
		t.Errorf("package = %+v", p)
	}
}

func TestBuildDropsSelfDependency(t *testing.T) {
	const url = "https://github.com/a/alpha"
	s := &fakeScanner{refs: map[string]fakeRefs{url: {head: sha9}}, scans: map[string]discover.Scan{url: quarkScan(
		discover.Dependency{Name: "ALPHA"},
		discover.Dependency{Name: "alpha", URL: "https://github.com/A/Alpha.git"},
		discover.Dependency{Name: "Other"},
	)}}
	ix := build(t, s, Inputs{Directory: []discover.Entry{{Name: "Alpha", URL: url}}})
	alpha := pkgNamed(t, ix, "Alpha")
	if !reflect.DeepEqual(alpha.Depends, map[string]string{"Other": "*"}) || alpha.DependsUnresolved != nil {
		t.Errorf("depends = %v, unresolved = %v", alpha.Depends, alpha.DependsUnresolved)
	}
}
