package indexer

import (
	"context"
	"strings"
	"testing"

	"github.com/sc-deken/sc-deken/internal/recipe"
)

func tagRecipe(approved map[string]string) recipe.Recipe {
	r := fmxRecipe()
	r.Repo = "https://github.com/someone/thing"
	r.Versions = recipe.Versions{TagPrefix: "v", Approved: approved}
	return r
}

func TestNewVersionsProposal(t *testing.T) {
	r := tagRecipe(map[string]string{"0.4.4": sha0})
	s := &fakeScanner{refs: map[string]fakeRefs{r.Repo: {tags: map[string]string{
		"v0.4.4": sha0, "v0.4.5": sha1, "v0.5.0-rc1": sha9, "nightly": sha9,
	}}}}
	props, moved, err := NewVersions(context.Background(), s, []recipe.Recipe{r})
	if err != nil || len(moved) != 0 {
		t.Fatalf("err %v moved %v", err, moved)
	}
	want := Proposal{
		Package: "fmx6", Version: "0.4.5", Tag: "v0.4.5", Commit: sha1, PrevVersion: "0.4.4", PrevCommit: sha0,
		CompareURL: "https://github.com/someone/thing/compare/" + sha0 + "..." + sha1,
	}
	if len(props) != 1 || props[0] != want {
		t.Errorf("proposals = %+v", props)
	}
}

func TestNewVersionsMovedTag(t *testing.T) {
	r := tagRecipe(map[string]string{"0.4.4": sha0})
	s := &fakeScanner{refs: map[string]fakeRefs{r.Repo: {tags: map[string]string{"v0.4.4": sha9}}}}
	props, moved, err := NewVersions(context.Background(), s, []recipe.Recipe{r})
	if err != nil || len(props) != 0 {
		t.Fatalf("err %v proposals %v", err, props)
	}
	want := MovedTag{Package: "fmx6", Version: "0.4.4", Tag: "v0.4.4", Recorded: sha0, Now: sha9}
	if len(moved) != 1 || moved[0] != want {
		t.Errorf("moved = %+v", moved)
	}
}

func TestNewVersionsOneBadRepoKeepsTheRest(t *testing.T) {
	good := tagRecipe(map[string]string{"0.4.4": sha0})
	bad := tagRecipe(map[string]string{"1.0": sha0})
	bad.Name, bad.Repo = "gone", "https://github.com/someone/gone"
	s := &fakeScanner{refs: map[string]fakeRefs{good.Repo: {tags: map[string]string{"v0.4.5": sha1}}}}
	props, _, err := NewVersions(context.Background(), s, []recipe.Recipe{bad, good})
	if len(props) != 1 || err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("proposals %v, err %v", props, err)
	}
}
