package advisory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRepo(t *testing.T) {
	want := "https://github.com/shimpe/panola"
	for _, in := range []string{
		"git@github.com:Shimpe/Panola.git",
		"https://github.com/shimpe/panola/",
		"HTTPS://GitHub.com/Shimpe/Panola.git",
		"http://github.com/shimpe/panola",
	} {
		if got := NormalizeRepo(in); got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAffectsByVersion(t *testing.T) {
	a := Advisory{Package: "pkg", Versions: []string{"1.2.0"}}
	if !a.Affects("pkg", "", "1.2.0", "") {
		t.Error("1.2.0 should be affected")
	}
	if a.Affects("pkg", "", "1.2.1", "") {
		t.Error("1.2.1 should not be affected")
	}
	if a.Affects("other", "", "1.2.0", "") {
		t.Error("other package should not be affected")
	}
}

func TestAffectsByCommitPrefix(t *testing.T) {
	a := Advisory{Package: "pkg", Commits: []string{"abc1234"}}
	if !a.Affects("pkg", "", "", "abc1234def5678") {
		t.Error("commit with matching prefix should be affected")
	}
	if a.Affects("pkg", "", "", "abc1235def5678") {
		t.Error("different commit should not be affected")
	}
	short := Advisory{Package: "pkg", Commits: []string{"abc12"}}
	if short.Affects("pkg", "", "", "abc1234def5678") {
		t.Error("prefix shorter than 7 chars must not match")
	}
}

func TestAffectsByRepo(t *testing.T) {
	a := Advisory{Repo: "https://github.com/shimpe/panola", Versions: []string{"1.0.0"}}
	if !a.Affects("renamed", "git@github.com:Shimpe/Panola.git", "1.0.0", "") {
		t.Error("should match through the repo")
	}
	if a.Affects("renamed", "", "1.0.0", "") {
		t.Error("empty repo must not match")
	}
}

func TestAffectsWildcard(t *testing.T) {
	a := Advisory{Package: "pkg", Versions: []string{"*"}}
	if !a.Affects("PKG", "", "", "") {
		t.Error("wildcard should match an external copy with no version")
	}
	b := Advisory{Package: "pkg", Versions: []string{"1.0.0"}}
	if b.Affects("pkg", "", "", "") {
		t.Error("unknown version must match only the wildcard")
	}
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const validTOML = `id = "SCD-2026-0001"
package = "pkg"
versions = ["1.0.0"]
severity = "malware"
summary = "bad"
details = "very bad"
published = 2026-11-02
`

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.toml", validTOML)
	write(t, dir, "b.toml", strings.Replace(
		strings.Replace(validTOML, "0001", "0002", 1), `2026-11-02`, `"2026-11-03"`, 1))
	write(t, dir, "notes.txt", "ignored")
	advs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(advs) != 2 || advs[0].Published != "2026-11-02" || advs[1].Published != "2026-11-03" {
		t.Errorf("unexpected advisories: %+v", advs)
	}
}

func TestLoadDirRejects(t *testing.T) {
	cases := map[string]string{
		"unknown key":  validTOML + "colour = \"red\"\n",
		"bad id":       strings.Replace(validTOML, "SCD-2026-0001", "X-1", 1),
		"bad severity": strings.Replace(validTOML, "malware", "scary", 1),
		"bad date":     strings.Replace(validTOML, "2026-11-02", "\"02/11/2026\"", 1),
		"no identity":  strings.Replace(validTOML, `package = "pkg"`, "", 1),
		"no versions":  strings.Replace(validTOML, `versions = ["1.0.0"]`, "", 1),
	}
	for name, body := range cases {
		dir := t.TempDir()
		write(t, dir, "a.toml", body)
		if _, err := LoadDir(dir); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadDirRejectsDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.toml", validTOML)
	write(t, dir, "b.toml", validTOML)
	_, err := LoadDir(dir)
	if err == nil || !strings.Contains(err.Error(), "SCD-2026-0001") {
		t.Fatalf("expected duplicate id error, got %v", err)
	}
}

func TestMatch(t *testing.T) {
	advs := []Advisory{
		{ID: "SCD-2026-0001", Package: "evil", Versions: []string{"*"}},
		{ID: "SCD-2026-0002", Repo: "https://github.com/o/r", Versions: []string{"2.0.0"}},
	}
	inst := []Installed{
		{Name: "evil", Version: "9.9.9", Source: "managed"},
		{Name: "r-quark", Repo: "git@github.com:O/R.git", Version: "2.0.0", Source: "quark"},
		{Name: "fine", Repo: "https://github.com/o/r", Version: "3.0.0", Source: "managed"},
	}
	hits := Match(advs, inst)
	if len(hits) != 2 {
		t.Fatalf("got %d hits: %+v", len(hits), hits)
	}
	if hits[0].Advisory.ID != "SCD-2026-0001" || hits[0].Installed.Name != "evil" {
		t.Errorf("hit 0 = %+v", hits[0])
	}
	if hits[1].Advisory.ID != "SCD-2026-0002" || hits[1].Installed.Name != "r-quark" {
		t.Errorf("hit 1 = %+v", hits[1])
	}
}
