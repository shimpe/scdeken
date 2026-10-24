package recipe

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const commitA = "0123456789abcdef0123456789abcdef01234567"

const portedPlugins = `name = "portedplugins"
summary = "Ported / remixed UGens from elsewhere"
repo = "https://github.com/madskjeldgaard/portedplugins"
license = "GPL-3.0"
authors = ["Mads Kjeldgaard"]
kind = "plugin"

[build]
system = "cmake"
subdir = ""
sc_path_var = "SC_PATH"
cmake_flags = ["-DSUPERNOVA=ON"]
submodules = true

[requires]
fftw = { apt = "libfftw3-dev", brew = "fftw", pacman = "fftw", vcpkg = "fftw3", pkgconfig = "fftw3" }

[versions]
tag_prefix = ""

[versions.approved]
"0.4.5" = "` + commitA + `"
`

// quarkRecipe returns a minimal valid quark recipe with the given extra lines
// inserted before the versions tables.
func quarkRecipe(extra string) string {
	return `name = "mything"
summary = "A thing"
repo = "https://github.com/x/mything"
kind = "quark"
` + extra + `
[versions.approved]
"1.0" = "` + commitA + `"
`
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParsePortedPlugins(t *testing.T) {
	r, err := Parse([]byte(portedPlugins))
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindPlugin || !r.HasPlugin() {
		t.Errorf("kind = %q", r.Kind)
	}
	if r.Build.SCPathVar != "SC_PATH" {
		t.Errorf("SCPathVar = %q", r.Build.SCPathVar)
	}
	if !reflect.DeepEqual(r.Build.CMakeFlags, []string{"-DSUPERNOVA=ON"}) {
		t.Errorf("CMakeFlags = %v", r.Build.CMakeFlags)
	}
	if r.Requires["fftw"].PkgConfig != "fftw3" || r.Requires["fftw"].Apt != "libfftw3-dev" {
		t.Errorf("requires = %+v", r.Requires)
	}
	if r.Versions.Approved["0.4.5"] != commitA {
		t.Errorf("approved = %v", r.Versions.Approved)
	}
}

func TestDefaultSCPathVar(t *testing.T) {
	src := strings.Replace(portedPlugins, `sc_path_var = "SC_PATH"`+"\n", "", 1)
	r, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if r.Build.SCPathVar != "SC_PATH" {
		t.Errorf("SCPathVar = %q", r.Build.SCPathVar)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	_, err := Parse([]byte(quarkRecipe(`sumary = "x"`)))
	if err == nil || !strings.Contains(err.Error(), "sumary") {
		t.Fatalf("err = %v", err)
	}
}

func TestQuarkNeedsNoBuild(t *testing.T) {
	if _, err := Parse([]byte(quarkRecipe(""))); err != nil {
		t.Fatalf("quark without build: %v", err)
	}
	src := strings.Replace(quarkRecipe(""), `"quark"`, `"plugin"`, 1)
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "build") {
		t.Fatalf("plugin without build: err = %v", err)
	}
}

func TestBadCommitRejected(t *testing.T) {
	src := strings.Replace(quarkRecipe(""), commitA, "abc", 1)
	_, err := Parse([]byte(src))
	if err == nil || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidationRules(t *testing.T) {
	cases := map[string]string{
		"bad name":    strings.Replace(quarkRecipe(""), `"mything"`, `"-bad"`, 1),
		"http repo":   strings.Replace(quarkRecipe(""), "https://", "http://", 1),
		"bad kind":    strings.Replace(quarkRecipe(""), `"quark"`, `"widget"`, 1),
		"no summary":  strings.Replace(quarkRecipe(""), `summary = "A thing"`, `summary = ""`, 1),
		"bad version": strings.Replace(quarkRecipe(""), `"1.0" =`, `"x.y" =`, 1),
		"upper hex":   strings.Replace(quarkRecipe(""), commitA, strings.ToUpper(commitA), 1),
		"bad system":  strings.Replace(portedPlugins, `"cmake"`, `"make"`, 1),
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	noApproved := "name = \"a\"\nsummary = \"s\"\nrepo = \"https://x\"\nkind = \"quark\"\n"
	if _, err := Parse([]byte(noApproved)); err == nil {
		t.Error("no approved versions: expected error")
	}
}

func TestApprovedVersionsSorted(t *testing.T) {
	r := Recipe{Versions: Versions{Approved: map[string]string{"0.9": commitA, "0.10": commitA}}}
	got := r.ApprovedVersions()
	if !reflect.DeepEqual(got, []string{"0.10", "0.9"}) {
		t.Errorf("got %v", got)
	}
}

func TestLoadRequiresApprovedLast(t *testing.T) {
	src := quarkRecipe("") + "\n[depends]\nfoo = \">=1.0\"\n"
	path := writeFile(t, t.TempDir(), "mything.toml", src)
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must be the last table") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mything.toml", quarkRecipe(""))
	writeFile(t, dir, "portedplugins.toml", portedPlugins)
	writeFile(t, dir, "notes.txt", "ignored")
	rs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Name != "mything" || rs[1].Name != "portedplugins" {
		t.Errorf("got %+v", rs)
	}
	writeFile(t, dir, "wrong.toml", quarkRecipe(""))
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "wrong.toml") {
		t.Errorf("name mismatch: err = %v", err)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	r, err := Parse([]byte(portedPlugins))
	if err != nil {
		t.Fatal(err)
	}
	r.Depends = map[string]string{"other": ">=1.2"}
	out, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !reflect.DeepEqual(r, back) {
		t.Errorf("round trip differs:\n%+v\n%+v\n%s", r, back, out)
	}
	if !strings.HasSuffix(string(out), "[versions.approved]\n\"0.4.5\" = \""+commitA+"\"\n") {
		t.Errorf("output does not end with approved table:\n%s", out)
	}
}

func TestDependsKeysAndConstraints(t *testing.T) {
	good := quarkRecipe("\n[depends]\nOther = \">=1.0,<2.0\"\n\"https://github.com/o/Repo.git\" = \"=1.2\"\nThird = \"*\"\n")
	r, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if r.Depends["https://github.com/o/Repo.git"] != "=1.2" || len(r.Depends) != 3 {
		t.Errorf("depends = %v", r.Depends)
	}
	bad := map[string]string{
		"http url":       "\"http://github.com/o/Repo\" = \"*\"",
		"git url":        "\"git://github.com/o/Repo\" = \"*\"",
		"bad name":       "\"-bad\" = \"*\"",
		"bad constraint": "Other = \"~1.0\"",
		"empty key":      "\"\" = \"*\"",
	}
	for name, line := range bad {
		if _, err := Parse([]byte(quarkRecipe("\n[depends]\n" + line + "\n"))); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestMarshalURLDependsRoundTrip(t *testing.T) {
	r, err := Parse([]byte(quarkRecipe("")))
	if err != nil {
		t.Fatal(err)
	}
	r.Depends = map[string]string{"Name": "*", "https://github.com/o/Repo": "=1.0"}
	out, err := Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(out)
	if err != nil || !reflect.DeepEqual(r.Depends, back.Depends) {
		t.Errorf("err = %v, depends = %v\n%s", err, back.Depends, out)
	}
}
