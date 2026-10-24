package recipe

import (
	"os"
	"strings"
	"testing"
)

const commitB = "fedcba9876543210fedcba9876543210fedcba98"

const commented = "# maintainer note\r\n" +
	"name = \"mything\"\r\n" +
	"summary = \"A thing\"\r\n" +
	"repo = \"https://github.com/x/mything\"\r\n" +
	"kind = \"quark\"\r\n" +
	"\r\n" +
	"[versions]\r\n" +
	"tag_prefix = \"v\" # tags look like v1.0\r\n" +
	"\r\n" +
	"# approved after review\r\n" +
	"[versions.approved]\r\n" +
	"\"1.0\" = \"" + commitA + "\" # first\r\n" +
	"\r\n"

func TestAppendApproved(t *testing.T) {
	path := writeFile(t, t.TempDir(), "mything.toml", commented)
	if err := AppendApproved(path, "0.5.0", commitB); err != nil {
		t.Fatal(err)
	}
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Versions.Approved["0.5.0"] != commitB {
		t.Errorf("approved = %v", r.Versions.Approved)
	}
	got, _ := os.ReadFile(path)
	inserted := "\"0.5.0\" = \"" + commitB + "\"\r\n"
	want := strings.Replace(commented, "# first\r\n", "# first\r\n"+inserted, 1)
	if string(got) != want {
		t.Errorf("file changed beyond the new line:\n%q\nwant\n%q", got, want)
	}
}

func TestAppendApprovedNoTrailingNewline(t *testing.T) {
	src := strings.TrimRight(quarkRecipe(""), "\n")
	path := writeFile(t, t.TempDir(), "mything.toml", src)
	if err := AppendApproved(path, "2.0", commitB); err != nil {
		t.Fatal(err)
	}
	if r, err := Load(path); err != nil || len(r.Versions.Approved) != 2 {
		t.Fatalf("r = %+v, err = %v", r, err)
	}
}

func TestAppendApprovedDuplicate(t *testing.T) {
	path := writeFile(t, t.TempDir(), "mything.toml", commented)
	err := AppendApproved(path, "1.0", commitB)
	if err == nil || !strings.Contains(err.Error(), "already approved") {
		t.Fatalf("err = %v", err)
	}
}

func TestAppendApprovedRejectsBadInput(t *testing.T) {
	path := writeFile(t, t.TempDir(), "mything.toml", commented)
	if err := AppendApproved(path, "2.0", "abc"); err == nil {
		t.Fatal("expected error for bad commit")
	}
	got, _ := os.ReadFile(path)
	if string(got) != commented {
		t.Error("file was modified despite the error")
	}
}
