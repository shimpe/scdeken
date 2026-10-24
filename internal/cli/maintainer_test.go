package cli

import (
	"bytes"
	"encoding/json"
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
