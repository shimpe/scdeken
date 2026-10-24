package install

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shimpe/scdeken/internal/scenv"
)

// writeFile creates path (and any parent folders) with content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindDuplicatesByPluginFile(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "Extensions")
	writeFile(t, filepath.Join(ext, "old-fmx", "deep", "fmx6.SCX"), "x")
	writeFile(t, filepath.Join(ext, "other", "Other.scx"), "x")

	got, err := FindDuplicates([]string{ext}, nil, []string{"FMX6.scx"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Duplicate{{Path: filepath.Join(ext, "old-fmx"), Matched: "FMX6.scx"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFindDuplicatesByClass(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "Extensions")
	writeFile(t, filepath.Join(ext, "x", "Foo.sc"), "FMX6 : MultiOutUGen {\n}\n")
	writeFile(t, filepath.Join(ext, "y", "Bar.sc"), "Unrelated { }\n")

	got, err := FindDuplicates([]string{ext}, nil, nil, []string{"FMX6"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Duplicate{{Path: filepath.Join(ext, "x"), Matched: "FMX6"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFindDuplicatesFileDirectlyInRoot(t *testing.T) {
	ext := t.TempDir()
	writeFile(t, filepath.Join(ext, "FMX6.scx"), "x")

	got, _ := FindDuplicates([]string{ext}, nil, []string{"FMX6.scx"}, nil)
	want := []Duplicate{{Path: filepath.Join(ext, "FMX6.scx"), Matched: "FMX6.scx"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFindDuplicatesSkipsManaged(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "Extensions")
	managed := filepath.Join(ext, "sc-deken")
	writeFile(t, filepath.Join(managed, "fmx6", "FMX6.scx"), "x")
	writeFile(t, filepath.Join(managed, "fmx6", "FMX6.sc"), "FMX6 : UGen { }")

	got, err := FindDuplicates([]string{ext}, []string{filepath.Join(managed, "fmx6")},
		[]string{"FMX6.scx"}, []string{"FMX6"})
	if err != nil || len(got) != 0 {
		t.Errorf("got %+v, %v; want nothing", got, err)
	}
}

func TestFindDuplicatesSortedDedupedAndMissingRootsIgnored(t *testing.T) {
	base := t.TempDir()
	ext := filepath.Join(base, "Extensions")
	writeFile(t, filepath.Join(ext, "b", "FMX6.scx"), "x")
	writeFile(t, filepath.Join(ext, "b", "FMX6_supernova.scx"), "x")
	writeFile(t, filepath.Join(ext, "a", "FMX6.scx"), "x")

	roots := []string{filepath.Join(base, "missing"), ext, ext}
	got, err := FindDuplicates(roots, nil, []string{"FMX6.scx", "FMX6_supernova.scx"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != filepath.Join(ext, "a") || got[1].Path != filepath.Join(ext, "b") {
		t.Errorf("got %+v", got)
	}
}

func TestPlanAddsDuplicatesConfirm(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "Extensions")
	writeFile(t, filepath.Join(ext, "old-fmx", "FMX6.scx"), "x")
	s := newService(t)
	s.Ctx.ExtensionDir = ext
	s.Paths = scenv.Paths{Managed: filepath.Join(ext, "sc-deken")}
	s.Index.Packages[0].PluginFiles = []string{"FMX6.scx"}

	p, err := s.Plan(context.Background(), "fmx6", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Confirm, []string{ConfirmDuplicates}) || len(p.Duplicates) != 1 {
		t.Errorf("Confirm = %v, Duplicates = %+v", p.Confirm, p.Duplicates)
	}
}

func TestPlanReportsOtherManagedPackageFolder(t *testing.T) {
	ext := filepath.Join(t.TempDir(), "Extensions")
	managed := filepath.Join(ext, "sc-deken")
	writeFile(t, filepath.Join(managed, "other", "classes", "Foo.sc"), "FMX6 : UGen { }")
	writeFile(t, filepath.Join(managed, "fmx6", "FMX6.scx"), "x")
	s := newService(t)
	s.Ctx.ExtensionDir = ext
	s.Paths = scenv.Paths{Managed: managed}
	s.Index.Packages[0].Classes = []string{"FMX6"}

	p, err := s.Plan(context.Background(), "fmx6", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []Duplicate{{Path: filepath.Join(managed, "other"), Matched: "FMX6"}}
	if !reflect.DeepEqual(p.Duplicates, want) {
		t.Errorf("Duplicates = %+v, want %+v", p.Duplicates, want)
	}
}
