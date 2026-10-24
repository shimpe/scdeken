package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/sign"
)

// runMain runs scdeken with deps and returns its exit code and output.
func runMain(t *testing.T, deps Deps, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = mainWithDeps(args, &out, &errOut, deps)
	return code, out.String(), errOut.String()
}

func TestCIPlatforms(t *testing.T) {
	code, out, errOut := runMain(t, Deps{}, "ci", "platforms")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var rows []map[string]string
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != 4 {
		t.Fatalf("rows = %v (%v) from %s", rows, err, out)
	}
	if rows[2]["key_os"] != "macos" || rows[2]["build_arch"] != "universal" || rows[1]["runner"] != "ubuntu-22.04-arm" {
		t.Errorf("rows = %v", rows)
	}
}

func TestCIPack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "pkg")
	os.MkdirAll(src, 0o755)
	os.WriteFile(filepath.Join(src, "a.scx"), []byte("x"), 0o644)
	zip1, zip2 := filepath.Join(dir, "1.zip"), filepath.Join(dir, "2.zip")
	for _, z := range []string{zip1, zip2} {
		if code, _, errOut := runMain(t, Deps{}, "ci", "pack", src, z); code != 0 {
			t.Fatalf("exit %d: %s", code, errOut)
		}
	}
	a, _ := os.ReadFile(zip1)
	b, _ := os.ReadFile(zip2)
	if len(a) == 0 || !bytes.Equal(a, b) {
		t.Error("packing the same folder twice gave different zips")
	}
}

func TestAdvisoriesValidate(t *testing.T) {
	dir := t.TempDir()
	good := "id = \"SCD-2026-0001\"\npackage = \"x\"\nversions = [\"*\"]\nseverity = \"broken\"\nsummary = \"s\"\ndetails = \"d\"\npublished = 2026-11-02\n"
	os.WriteFile(filepath.Join(dir, "a.toml"), []byte(good), 0o644)
	if code, _, errOut := runMain(t, Deps{}, "advisories", "validate", "--dir", dir); code != 0 {
		t.Fatalf("valid dir: exit %d: %s", code, errOut)
	}
	os.WriteFile(filepath.Join(dir, "b.toml"), []byte("id = \"nope\"\n"), 0o644)
	code, _, errOut := runMain(t, Deps{}, "advisories", "validate", "--dir", dir)
	if code != 1 || !strings.Contains(errOut, "b.toml") {
		t.Errorf("invalid dir: exit %d: %s", code, errOut)
	}
	code, _, errOut = runMain(t, Deps{}, "advisories", "validate", "--dir", filepath.Join(dir, "missing"))
	if code != 1 || !strings.Contains(errOut, "advisories folder") {
		t.Errorf("missing dir: exit %d: %s", code, errOut)
	}
}

func TestKeygenSignRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_KEY_PASSWORD", "hunter2")
	if code, _, errOut := runMain(t, Deps{}, "index", "keygen", "--out-dir", dir, "--password-env", "TEST_KEY_PASSWORD"); code != 0 {
		t.Fatalf("keygen exit %d: %s", code, errOut)
	}
	if code, _, errOut := runMain(t, Deps{}, "index", "keygen", "--out-dir", dir, "--password-env", "TEST_KEY_PASSWORD"); code == 0 || !strings.Contains(errOut, "already exists") {
		t.Errorf("second keygen: exit %d: %s", code, errOut)
	}
	pub, _ := os.ReadFile(filepath.Join(dir, "scdeken.pub"))
	key, _ := os.ReadFile(filepath.Join(dir, "scdeken.key"))

	indexFile := filepath.Join(dir, "index.json")
	os.WriteFile(indexFile, []byte(`{"schema":1}`), 0o644)
	if code, _, errOut := runMain(t, Deps{}, "index", "sign", "--in", indexFile); code == 0 || !strings.Contains(errOut, signingKeyEnv) {
		t.Errorf("sign without keys: exit %d: %s", code, errOut)
	}
	t.Setenv(signingKeyEnv, string(key))
	t.Setenv(signingPasswordEnv, "hunter2")
	if code, _, errOut := runMain(t, Deps{}, "index", "sign", "--in", indexFile); code != 0 {
		t.Fatalf("sign exit %d: %s", code, errOut)
	}
	sig, err := os.ReadFile(indexFile + ".minisig")
	if err != nil {
		t.Fatal(err)
	}
	if err := sign.Verify([]byte(`{"schema":1}`), sig, []string{strings.TrimSpace(string(pub))}); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}
}

func TestKeygenPromptsOnStdin(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runMain(t, Deps{Stdin: strings.NewReader("secret\nsecret\n")}, "index", "keygen", "--out-dir", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "again") {
		t.Errorf("the password was not asked twice: %q", errOut)
	}
	key, _ := os.ReadFile(filepath.Join(dir, "scdeken.key"))
	if _, err := sign.Sign(key, "secret", []byte("m")); err != nil {
		t.Errorf("key does not open with the typed password: %v", err)
	}
}

func TestKeygenRefusesMismatchedPasswords(t *testing.T) {
	dir := t.TempDir()
	code, _, errOut := runMain(t, Deps{Stdin: strings.NewReader("secret\nsecert\n")}, "index", "keygen", "--out-dir", dir)
	if code != 1 || !strings.Contains(errOut, "do not match") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files written: %v", entries)
	}
}

// TestIndexBuildCommand runs index build against a file directory, a fake
// GitHub and a fake git, with --limit cutting the directory to one entry.
func TestIndexBuildCommand(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/repositories"):
			io.WriteString(w, `{"items":[]}`)
		case r.URL.Path == "/repos/a/one":
			io.WriteString(w, `{"full_name":"a/one","description":"First quark","stargazers_count":5}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer gh.Close()
	dir := t.TempDir()
	recipes := filepath.Join(dir, "recipes")
	os.Mkdir(recipes, 0o755)
	directory := filepath.Join(dir, "directory.txt")
	os.WriteFile(directory, []byte("One=https://github.com/a/one\nTwo=https://github.com/a/two\n"), 0o644)
	scVersions := filepath.Join(dir, "sc-versions.toml")
	os.WriteFile(scVersions, []byte("[[sc]]\nversion = \"3.14\"\napi = 3\nmin = \"3.13.0\"\n"), 0o644)
	runner := &run.Fake{Script: map[string]run.FakeResult{
		"git ls-remote https://github.com/a/one HEAD refs/tags/*": {Out: headID + "\tHEAD\n" + tagOld + "\trefs/tags/v1.0"},
		"git *": {},
	}}
	out := filepath.Join(dir, "index.json")
	code, _, errOut := runMain(t, Deps{Runner: runner, GitHubURL: gh.URL},
		"index", "build", "--recipes", recipes, "--directory", directory, "--sc-versions", scVersions,
		"--previous", filepath.Join(dir, "missing.json"), "--limit", "1", "--out", out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(ix.Packages) != 1 || ix.Packages[0].Name != "One" || ix.Packages[0].Summary != "First quark" || ix.Packages[0].Stars != 5 {
		t.Errorf("packages = %+v", ix.Packages)
	}
	if len(ix.SCVersions) != 1 || ix.SCVersions[0].API != 3 {
		t.Errorf("sc versions = %+v", ix.SCVersions)
	}
	if !strings.Contains(errOut, "No previous index") {
		t.Errorf("missing previous index not logged:\n%s", errOut)
	}
}

func TestParseSums(t *testing.T) {
	got := parseSums("ABC123  linux-x86_64-api3.zip\r\n\nnot a line\ndef456 *win.zip\n")
	if got["linux-x86_64-api3.zip"] != "abc123" || got["win.zip"] != "def456" || len(got) != 2 {
		t.Errorf("sums = %v", got)
	}
}

// sourceFailureSetup writes a recipes folder with one plugin-less recipe and
// returns the arguments of an index build whose sources the test can break.
func sourceFailureSetup(t *testing.T, gh *httptest.Server) (args []string, out string) {
	t.Helper()
	dir := t.TempDir()
	recipes := filepath.Join(dir, "recipes")
	os.Mkdir(recipes, 0o755)
	os.WriteFile(filepath.Join(recipes, "Thing.toml"), []byte(quarkRecipe), 0o644)
	directory := filepath.Join(dir, "directory.txt")
	os.WriteFile(directory, nil, 0o644)
	out = filepath.Join(dir, "index.json")
	return []string{"index", "build", "--recipes", recipes, "--directory", directory, "--releases-repo", "o/r", "--out", out}, out
}

func runFailingBuild(t *testing.T, handler http.HandlerFunc, wantInError string) {
	t.Helper()
	gh := httptest.NewServer(handler)
	defer gh.Close()
	args, out := sourceFailureSetup(t, gh)
	runner := &run.Fake{Script: map[string]run.FakeResult{"git *": {}}}
	code, _, errOut := runMain(t, Deps{Runner: runner, GitHubURL: gh.URL}, args...)
	if code != 1 || !strings.Contains(errOut, wantInError) {
		t.Errorf("exit %d, stderr: %s", code, errOut)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("an index was written although a source could not be read")
	}
}

func TestIndexBuildFailsWhenReleasesCannotBeListed(t *testing.T) {
	runFailingBuild(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			io.WriteString(w, `{"items":[]}`)
			return
		}
		http.Error(w, "boom", http.StatusInternalServerError)
	}, "list the prebuilt releases")
}

func TestIndexBuildFailsWhenChecksumsCannotBeRead(t *testing.T) {
	runFailingBuild(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/"):
			io.WriteString(w, `{"items":[]}`)
		case r.URL.Path == "/repos/o/r/releases":
			io.WriteString(w, `[{"tag_name":"Thing-0.4.5","assets":[{"name":"SHA256SUMS","browser_download_url":"http://`+r.Host+`/missing","size":1}]}]`)
		default:
			http.NotFound(w, r)
		}
	}, "SHA256SUMS of release Thing-0.4.5")
}

func TestIndexBuildFailsWhenATopicSearchFails(t *testing.T) {
	runFailingBuild(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}, "search topic")
}
