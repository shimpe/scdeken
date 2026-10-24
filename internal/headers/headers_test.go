package headers

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/config"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/version"
)

func mustVersion(t *testing.T, s string) version.Version {
	t.Helper()
	v, err := version.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPickTagExact(t *testing.T) {
	tag, exact, err := PickTag([]string{"Version-3.12.2", "Version-3.13.0"}, mustVersion(t, "3.13.0"))
	if err != nil || tag != "Version-3.13.0" || !exact {
		t.Fatalf("got %q exact=%v err=%v", tag, exact, err)
	}
}

func TestPickTagPrereleaseExact(t *testing.T) {
	tag, exact, err := PickTag([]string{"Version-3.13.0", "Version-3.14.0-rc1"}, mustVersion(t, "3.14.0-rc1"))
	if err != nil || tag != "Version-3.14.0-rc1" || !exact {
		t.Fatalf("got %q exact=%v err=%v", tag, exact, err)
	}
}

func TestPickTagDevFallsBack(t *testing.T) {
	tags := []string{"Version-3.13.0", "Version-3.14.1", "Version-3.15.0-rc1", "latest"}
	tag, exact, err := PickTag(tags, mustVersion(t, "3.15.0-dev"))
	if err != nil || tag != "Version-3.14.1" || exact {
		t.Fatalf("got %q exact=%v err=%v", tag, exact, err)
	}
}

func TestPickTagTooOld(t *testing.T) {
	_, _, err := PickTag([]string{"Version-3.12.2", "Version-3.13.0"}, mustVersion(t, "3.0.0"))
	if err == nil || !strings.Contains(err.Error(), "older than any supported release") {
		t.Fatalf("err = %v", err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHeader(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, headerPath), "static const int sc_api_version = 3;\n")
}

func TestReadAPI(t *testing.T) {
	dir := t.TempDir()
	writeHeader(t, dir)
	got, err := ReadAPI(dir)
	if err != nil || got != 3 {
		t.Fatalf("got %d, %v", got, err)
	}
}

func TestReadAPIMissing(t *testing.T) {
	if _, err := ReadAPI(t.TempDir()); err == nil {
		t.Fatal("want error")
	}
}

const lsRemote = "aaa\trefs/tags/Version-3.12.2\nbbb\trefs/tags/Version-3.13.0\n"

func newFake() *run.Fake {
	return &run.Fake{Script: map[string]run.FakeResult{
		"git ls-remote --tags --refs " + config.SCRepoURL: {Out: lsRemote},
		"git clone *": {},
	}}
}

func cloneCalls(f *run.Fake) int {
	n := 0
	for _, c := range f.Calls {
		if len(c.Args) > 0 && c.Args[0] == "clone" {
			n++
		}
	}
	return n
}

func TestEnsureUsesCache(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "Version-3.13.0")
	writeHeader(t, dir)
	writeFile(t, filepath.Join(dir, marker), "")
	f := newFake()
	h, err := Ensure(context.Background(), f, cache, mustVersion(t, "3.13.0"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cloneCalls(f) != 0 {
		t.Errorf("cloned despite cache: %v", f.Calls)
	}
	if h.Dir != dir || h.Tag != "Version-3.13.0" || h.API != 3 || !h.Exact {
		t.Errorf("got %+v", h)
	}
}

// fakeClone makes OnRun create a checkout at the clone destination.
func fakeClone(t *testing.T, withSubmodules bool) func(run.Cmd) error {
	return func(c run.Cmd) error {
		dest := c.Args[len(c.Args)-1]
		writeHeader(t, dest)
		if withSubmodules {
			writeFile(t, filepath.Join(dest, "external_libraries", "nova-tt", "x.hpp"), "x")
			writeFile(t, filepath.Join(dest, "external_libraries", "nova-simd", "y.hpp"), "y")
		}
		return nil
	}
}

func TestEnsureReclonesWithoutMarker(t *testing.T) {
	cache := t.TempDir()
	dir := filepath.Join(cache, "Version-3.13.0")
	writeFile(t, filepath.Join(dir, "stale.txt"), "half-done")
	f := newFake()
	f.OnRun = fakeClone(t, true)
	var log bytes.Buffer
	h, err := Ensure(context.Background(), f, cache, mustVersion(t, "3.13.0"), &log)
	if err != nil {
		t.Fatal(err)
	}
	if cloneCalls(f) != 1 {
		t.Errorf("clone calls = %d", cloneCalls(f))
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.txt")); !os.IsNotExist(err) {
		t.Errorf("stale file survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, marker)); err != nil {
		t.Errorf("marker missing: %v", err)
	}
	if h.API != 3 {
		t.Errorf("API = %d", h.API)
	}
}

func TestEnsureLogsFallback(t *testing.T) {
	f := newFake()
	f.OnRun = fakeClone(t, true)
	var log bytes.Buffer
	h, err := Ensure(context.Background(), f, t.TempDir(), mustVersion(t, "3.14.0-dev"), &log)
	if err != nil {
		t.Fatal(err)
	}
	want := "No SuperCollider release tag for 3.14.0-dev; using Version-3.13.0 headers instead."
	if h.Exact || !strings.Contains(log.String(), want) {
		t.Errorf("exact=%v log=%q", h.Exact, log.String())
	}
}

func TestEnsureMissingSubmodules(t *testing.T) {
	f := newFake()
	f.OnRun = fakeClone(t, false)
	cache := t.TempDir()
	_, err := Ensure(context.Background(), f, cache, mustVersion(t, "3.13.0"), nil)
	if err == nil || !strings.Contains(err.Error(), "missing its submodules") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cache, "Version-3.13.0", marker)); statErr == nil {
		t.Error("marker written despite missing submodules")
	}
}
