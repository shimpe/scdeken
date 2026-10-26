package index

import (
	"reflect"
	"testing"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/recipe"
)

func names(rs []Result) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

func TestSearchRanking(t *testing.T) {
	got := names(Search(fixture(), "fm", SearchOptions{}))
	want := []string{"fmx6", "presetter", "sc3-plugins"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSearchIncludeUnreviewed(t *testing.T) {
	got := names(Search(fixture(), "fm", SearchOptions{IncludeUnreviewed: true}))
	want := []string{"fmx6", "fm-thing", "presetter", "sc3-plugins"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSearchAllTermsMustMatch(t *testing.T) {
	got := names(Search(fixture(), "fm presets", SearchOptions{}))
	if !reflect.DeepEqual(got, []string{"presetter"}) {
		t.Errorf("got %v, want only presetter", got)
	}
}

func TestSearchKindFilter(t *testing.T) {
	got := names(Search(fixture(), "", SearchOptions{Kind: recipe.KindQuark}))
	want := []string{"Panola", "presetter"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSearchEmptyQuerySortsByTrustThenName(t *testing.T) {
	rs := Search(fixture(), "  ", SearchOptions{IncludeUnreviewed: true})
	want := []string{"fmx6", "Panola", "sc3-plugins", "presetter", "fm-thing"}
	if !reflect.DeepEqual(names(rs), want) {
		t.Errorf("got %v, want %v", names(rs), want)
	}
	if rs[0].Match != nil {
		t.Error("empty query should carry no match")
	}
}

func TestSearchMatchReportsField(t *testing.T) {
	for _, r := range Search(fixture(), "fm", SearchOptions{}) {
		if r.Name == "sc3-plugins" {
			if r.Match == nil || *r.Match != (Match{Field: "ugen", Value: "FM7"}) {
				t.Errorf("Match = %+v", r.Match)
			}
			return
		}
	}
	t.Fatal("sc3-plugins not found")
}

func TestSearchLatestRetractedAndClassMatch(t *testing.T) {
	ix := fixture()
	ix.Advisories = []advisory.Advisory{
		{ID: "SCD-2026-0003", Package: "Panola", Versions: []string{"*"}, Severity: advisory.Broken},
	}
	rs := Search(ix, "Panola", SearchOptions{})
	if len(rs) != 1 || rs[0].Match.Field != "name" {
		t.Fatalf("got %+v", rs)
	}
	if rs[0].Latest != "0.5.0" || !rs[0].Retracted {
		t.Errorf("Latest=%q Retracted=%v", rs[0].Latest, rs[0].Retracted)
	}
	ix.Packages[2].Name = "Other"
	rs = Search(ix, "panola", SearchOptions{})
	if len(rs) != 1 || *rs[0].Match != (Match{Field: "class", Value: "Panola"}) {
		t.Errorf("got %+v", rs)
	}
}

func TestSearchByRepositoryAddress(t *testing.T) {
	for _, q := range []string{
		"https://github.com/c/panola",
		"https://github.com/C/Panola.git",
		" github.com/c/panola/ ",
		"git@github.com:c/panola.git",
	} {
		rs := Search(fixture(), q, SearchOptions{})
		if !reflect.DeepEqual(names(rs), []string{"Panola"}) {
			t.Errorf("%q: got %v, want Panola", q, names(rs))
			continue
		}
		if rs[0].Match == nil || rs[0].Match.Field != "repo" || rs[0].Repo != "https://github.com/c/panola" {
			t.Errorf("%q: match %+v, repo %q; want a repo match on https://github.com/c/panola", q, rs[0].Match, rs[0].Repo)
		}
	}
	if rs := Search(fixture(), "https://github.com/c/panola-other", SearchOptions{}); len(rs) != 0 {
		t.Errorf("another repository: got %v, want nothing", names(rs))
	}
	if rs := Search(fixture(), "https://github.com/d/fm-thing", SearchOptions{}); len(rs) != 0 {
		t.Errorf("unreviewed repository without IncludeUnreviewed: got %v, want nothing", names(rs))
	}
}
