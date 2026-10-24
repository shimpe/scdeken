package discover

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/recipe"
)

const (
	sha0 = "0000000000000000000000000000000000000000"
	sha1 = "1111111111111111111111111111111111111111"
	head = "2222222222222222222222222222222222222222"
	url  = "https://github.com/someone/fmx6"
)

var pluginMeta = github.Repo{FullName: "someone/fmx6", Description: "FM synth", License: "MIT"}

func pluginScan() Scan {
	return Scan{Kind: recipe.KindPlugin, CMakeDir: ".", SCPathVar: "SC_PATH"}
}

func TestDraftTaggedPlugin(t *testing.T) {
	tags := map[string]string{"v0.4.5": sha1, "v0.4.4": sha0}
	r, notes, err := DraftRecipe(url, pluginMeta, pluginScan(), head, tags)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Versions.Approved; len(got) != 1 || got["0.4.5"] != sha1 {
		t.Errorf("approved = %v", got)
	}
	if r.Versions.TagPrefix != "" || r.Kind != recipe.KindPlugin || r.Name != "fmx6" {
		t.Errorf("recipe = %+v", r)
	}
	if r.Build == nil || r.Build.Subdir != "" || len(notes) != 0 {
		t.Errorf("build = %+v, notes = %v", r.Build, notes)
	}
}

func TestDraftTagPrefixAndBuildOptions(t *testing.T) {
	scan := pluginScan()
	scan.CMakeDir, scan.Submodules, scan.MentionsSupernova = "src", true, true
	tags := map[string]string{"Version-1.2": sha1, "Version-1.10": sha0, "nightly": head}
	r, _, err := DraftRecipe(url, pluginMeta, scan, head, tags)
	if err != nil {
		t.Fatal(err)
	}
	if r.Versions.TagPrefix != "Version-" || r.Versions.Approved["1.10"] != sha0 {
		t.Errorf("versions = %+v", r.Versions)
	}
	b := r.Build
	if b.Subdir != "src" || !b.Submodules || len(b.CMakeFlags) != 1 || b.CMakeFlags[0] != "-DSUPERNOVA=ON" {
		t.Errorf("build = %+v", b)
	}
}

func TestDraftUntagged(t *testing.T) {
	r, notes, err := DraftRecipe(url, pluginMeta, pluginScan(), head, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Versions.Approved["0.1.0"] != head {
		t.Errorf("approved = %v", r.Versions.Approved)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "no tags upstream") {
		t.Errorf("notes = %v", notes)
	}
}

func TestDraftQuarkUsesQuarkFile(t *testing.T) {
	scan := Scan{Kind: recipe.KindQuark, HasQuark: true, Quark: QuarkFile{
		Name: "MyQuark", Summary: "From the quark", Dependencies: []string{"Other", "https://x.org/y.git", "Pinned@1.0"},
	}}
	r, _, err := DraftRecipe(url, pluginMeta, scan, head, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "MyQuark" || r.Summary != "From the quark" || r.Build != nil {
		t.Errorf("recipe = %+v", r)
	}
	if len(r.Depends) != 1 || r.Depends["Other"] == "" {
		t.Errorf("depends = %v", r.Depends)
	}
}

func TestDraftNotSuperCollider(t *testing.T) {
	_, _, err := DraftRecipe(url, pluginMeta, Scan{}, head, nil)
	if err == nil || !strings.Contains(err.Error(), "doesn't look like a SuperCollider plugin or quark") {
		t.Errorf("err = %v", err)
	}
}

func TestDraftTodoFields(t *testing.T) {
	_, notes, err := DraftRecipe(url, github.Repo{}, pluginScan(), head, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(notes, "\n")
	if !strings.Contains(text, "summary") || !strings.Contains(text, "license") {
		t.Errorf("notes = %v", notes)
	}
}

func TestDraftOutputValidates(t *testing.T) {
	tags := map[string]string{"v1.0": sha1}
	r, _, err := DraftRecipe(url, pluginMeta, pluginScan(), head, tags)
	if err != nil {
		t.Fatal(err)
	}
	data, err := recipe.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recipe.Parse(data); err != nil {
		t.Errorf("%v\n%s", err, data)
	}
}

func TestParseGitHubURL(t *testing.T) {
	owner, repo, err := ParseGitHubURL("https://github.com/a/b.git")
	if err != nil || owner != "a" || repo != "b" {
		t.Errorf("got %q %q %v", owner, repo, err)
	}
	if _, _, err := ParseGitHubURL("https://gitlab.com/a/b"); err == nil || !strings.Contains(err.Error(), "only GitHub") {
		t.Errorf("err = %v", err)
	}
}

func TestPickReleaseSameVersionIsDeterministic(t *testing.T) {
	tags := map[string]string{"v1.0": sha0, "1.0": sha1, "0.9": head}
	for i := 0; i < 50; i++ {
		if rel := PickRelease(head, tags); rel.Version != "1.0" || rel.Commit != sha1 {
			t.Fatalf("run %d: release = %+v", i, rel)
		}
	}
}

func TestDraftWithDependsAndBuildSurvivesRoundTrip(t *testing.T) {
	scan := Scan{
		Kind: recipe.KindPluginQuark, HasQuark: true, CMakeDir: "src", SCPathVar: "SC_SRC_PATH",
		Submodules: true, MentionsSupernova: true,
		Quark: QuarkFile{Name: "Both", Summary: "s", Dependencies: []string{"Other", "More"}},
	}
	r, _, err := DraftRecipe(url, pluginMeta, scan, head, map[string]string{"Version-2.1": sha1, "Version-2.0": sha0})
	if err != nil {
		t.Fatal(err)
	}
	data, err := recipe.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	back, err := recipe.Parse(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if !reflect.DeepEqual(r, back) {
		t.Errorf("round trip differs:\n%+v\n%+v\n%s", r, back, data)
	}
}
