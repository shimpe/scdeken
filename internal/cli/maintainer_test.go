package cli

import (
	"io"
	"net/http"
	"net/http/httptest"

	"bytes"
	"encoding/json"
	"github.com/shimpe/scdeken/internal/run"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	commitOld = "1111111111111111111111111111111111111111"
	commitNew = "2222222222222222222222222222222222222222"
)

const quarkRecipe = `name = "Thing"
summary = "A thing"
repo = "https://github.com/someone/thing"
kind = "quark"

[versions]
tag_prefix = ""

[versions.approved]
"0.4.5" = "` + commitOld + `"
`

func writeRecipe(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "thing.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecipeApproveCommand(t *testing.T) {
	path := writeRecipe(t, quarkRecipe)
	var out, errOut bytes.Buffer
	if code := Main([]string{"recipe", "approve", path, "0.5.0", commitNew}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"0.5.0" = "`+commitNew+`"`) {
		t.Errorf("recipe does not list the new version:\n%s", data)
	}
	if !strings.Contains(out.String(), `"approved": "0.5.0"`) {
		t.Errorf("answer = %s", out.String())
	}
}

func TestRecipeCheckCommand(t *testing.T) {
	path := writeRecipe(t, quarkRecipe)
	var out, errOut bytes.Buffer
	if code := Main([]string{"recipe", "check", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s%s", code, out.String(), errOut.String())
	}
	var resp struct {
		OK   bool
		Data struct{ OK bool }
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || !resp.OK || !resp.Data.OK {
		t.Errorf("answer = %s (%v)", out.String(), err)
	}
	bad := writeRecipe(t, strings.Replace(quarkRecipe, `kind = "quark"`, `kind = "banana"`, 1))
	if code := Main([]string{"recipe", "check", bad}, &out, &errOut); code != 1 {
		t.Errorf("invalid recipe: exit %d", code)
	}
}

func TestRecipeNewRejectsOtherHosts(t *testing.T) {
	var out, errOut bytes.Buffer
	code := Main([]string{"recipe", "new", "https://gitlab.com/a/b"}, &out, &errOut)
	if code == 0 || !strings.Contains(errOut.String(), "only GitHub repositories are supported for now") {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}

const (
	tagOld = "3333333333333333333333333333333333333333"
	tagNew = "4444444444444444444444444444444444444444"
	headID = "5555555555555555555555555555555555555555"
)

// fakeRepoRunner answers git: ls-remote lists two tags, and checking out a
// commit writes a plugin with a broken .quark file into the work folder.
func fakeRepoRunner() *run.Fake {
	lsRemote := headID + "\tHEAD\n" + tagOld + "\trefs/tags/v1.0\n" + tagNew + "\trefs/tags/v1.1"
	return &run.Fake{
		Script: map[string]run.FakeResult{
			"git ls-remote https://github.com/someone/foo HEAD refs/tags/*": {Out: lsRemote},
			"git *": {},
		},
		OnRun: func(c run.Cmd) error {
			if c.Args[0] != "-C" || c.Args[2] != "checkout" {
				return nil
			}
			files := map[string]string{
				"CMakeLists.txt": "include_directories(${SC_PATH}/include)\n",
				"Foo.sc":         "Foo : UGen {}\n",
				"Foo.quark":      "( name: \"Foo\", summary: \"unterminated",
			}
			if err := os.MkdirAll(c.Args[1], 0o755); err != nil {
				return err
			}
			for name, text := range files {
				if err := os.WriteFile(filepath.Join(c.Args[1], name), []byte(text), 0o644); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func TestRecipeNewCommand(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/someone/foo" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"full_name":"someone/foo","description":"A foo","license":{"spdx_id":"MIT"}}`)
	}))
	defer gh.Close()
	runner := fakeRepoRunner()
	out := filepath.Join(t.TempDir(), "foo.toml")
	var stdout, stderr bytes.Buffer
	code := mainWithDeps([]string{"recipe", "new", "https://github.com/someone/foo", "--out", out},
		&stdout, &stderr, Deps{Runner: runner, GitHubURL: gh.URL})
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`kind = "plugin+quark"`, `"1.1" = "` + tagNew + `"`, "# NOTE: the .quark file could not be read: "} {
		if !strings.Contains(string(data), want) {
			t.Errorf("recipe lacks %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), tagOld) {
		t.Errorf("recipe approves the older tag:\n%s", data)
	}
}
