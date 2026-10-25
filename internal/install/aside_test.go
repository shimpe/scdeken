package install

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuarantineDisablesLoadableFiles(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	for _, f := range []string{"FMX6_supernova.scx", "Classes/FMX6.sc", "linux/FMX6.SO", "README.txt", "help.schelp"} {
		writeTestFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x")
	}
	got, err := s.Quarantine(dir, "SCD-2026-0001")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"FMX6.scx.disabled", "FMX6_supernova.scx.disabled", "Classes/FMX6.sc.disabled", "linux/FMX6.SO.disabled", "README.txt", "help.schelp"} {
		if !exists(filepath.Join(got, filepath.FromSlash(f))) {
			t.Errorf("%s missing in quarantine", f)
		}
	}
	for _, f := range []string{"FMX6.scx", "Classes/FMX6.sc", "linux/FMX6.SO"} {
		if exists(filepath.Join(got, filepath.FromSlash(f))) {
			t.Errorf("%s can still be loaded", f)
		}
	}
}

func TestBackupDisablesLoadableFiles(t *testing.T) {
	s := applyService(t)
	p, _ := duplicatePlan(t, s)
	res, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.BackedUp) != 1 || !exists(filepath.Join(res.BackedUp[0], "FMX6.scx.disabled")) || exists(filepath.Join(res.BackedUp[0], "FMX6.scx")) {
		t.Errorf("backup not disabled: %+v", res.BackedUp)
	}
}

func TestQuarantineRefusesInsideIncludePath(t *testing.T) {
	s := applyService(t)
	dir := installManaged(t, s, "1.0.0")
	s.Ctx.IncludePaths = []string{filepath.Dir(s.Ctx.AppSupportDir)}
	_, err := s.Quarantine(dir, "SCD-2026-0001")
	if err == nil || !strings.Contains(err.Error(), "LanguageConfig") || !strings.Contains(err.Error(), "Nothing was moved") {
		t.Fatalf("err = %v", err)
	}
	if !exists(filepath.Join(dir, "FMX6.scx")) || exists(s.Paths.Quarantine) {
		t.Error("nothing may move")
	}
}

func TestBackupRefusesInsideExtensionDir(t *testing.T) {
	s := applyService(t)
	p, dup := duplicatePlan(t, s)
	s.Ctx.SystemExtensionDir = s.Ctx.AppSupportDir
	_, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "replace"})
	if err == nil || !strings.Contains(err.Error(), "Platform.systemExtensionDir") {
		t.Fatalf("err = %v", err)
	}
	if !exists(filepath.Join(dup, "FMX6.scx")) || exists(filepath.Join(s.Paths.Managed, "fmx6")) || exists(s.Paths.Backup) {
		t.Error("nothing may change")
	}
}

func TestWithin(t *testing.T) {
	root := t.TempDir()
	ext := filepath.Join(root, "ext")
	for _, c := range []struct {
		child, parent string
		want          bool
	}{
		{filepath.Join(ext, "a", "b"), ext, true},
		{ext, ext, true},
		{filepath.Join(root, "ext2"), ext, false},
		{root, ext, false},
		{filepath.Join(root, "x"), "", false},
	} {
		if got := within(c.child, c.parent); got != c.want {
			t.Errorf("within(%s, %s) = %v", c.child, c.parent, got)
		}
	}
}

func TestEnableTreeUndoesDisableTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pkg")
	writeTestFile(t, filepath.Join(dir, "A.scx"), "a")
	writeTestFile(t, filepath.Join(dir, "sub", "B.sc"), "b")
	writeTestFile(t, filepath.Join(dir, "notes.disabled"), "kept as is")
	if err := disableTree(dir); err != nil {
		t.Fatal(err)
	}
	if err := enableTree(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"A.scx", "sub/B.sc", "notes.disabled"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(f))) {
			t.Errorf("%s missing after enable", f)
		}
	}
}
