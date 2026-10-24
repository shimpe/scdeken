package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/sc-deken/sc-deken/internal/archive"
	"github.com/sc-deken/sc-deken/internal/fetch"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/run"
	"github.com/sc-deken/sc-deken/internal/state"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// applyService is newService with real folders under a temp dir.
func applyService(t *testing.T) *Service {
	t.Helper()
	s := newService(t)
	root := t.TempDir()
	s.Ctx.AppSupportDir = filepath.Join(root, "support")
	s.Ctx.ExtensionDir = filepath.Join(root, "ext")
	s.Paths = s.Ctx.Paths()
	s.Fetch = fetch.New(filepath.Join(root, "http-cache"))
	return s
}

// serveZip packs files (name -> content) into a zip, serves it, and returns
// the URL and SHA-256 of the zip.
func serveZip(t *testing.T, files map[string]string) (url, sum string) {
	t.Helper()
	src := t.TempDir()
	for name, content := range files {
		writeTestFile(t, filepath.Join(src, filepath.FromSlash(name)), content)
	}
	zip := filepath.Join(t.TempDir(), "pkg.zip")
	if err := archive.Zip(src, zip); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(data) }))
	t.Cleanup(srv.Close)
	h := sha256.Sum256(data)
	return srv.URL, hex.EncodeToString(h[:])
}

func prebuiltPlan(name string, kind recipe.Kind, url, sum string) Plan {
	return Plan{
		Package: name, Version: "1.0.0", Commit: fmx6Commit, Kind: kind,
		Route: RoutePrebuilt, Platform: hostKey(),
		Build: &index.Build{Platform: hostKey(), URL: url, SHA256: sum},
	}
}

func TestApplyPrebuilt(t *testing.T) {
	s := applyService(t)
	var fractions []float32
	s.Progress = func(f float32, _ string) { fractions = append(fractions, f) }
	url, sum := serveZip(t, map[string]string{"FMX6.scx": "binary"})

	res, err := s.Apply(context.Background(), prebuiltPlan("fmx6", recipe.KindPlugin, url, sum))
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(s.Paths.Managed, "fmx6")
	if !exists(filepath.Join(target, "FMX6.scx")) || res.InstalledTo != target {
		t.Errorf("not installed: %+v", res)
	}
	e := s.State.Packages["fmx6"]
	if e.Route != state.Prebuilt || e.Path != target || e.Version != "1.0.0" || e.InstalledAt.IsZero() {
		t.Errorf("state entry = %+v", e)
	}
	saved, err := state.Load(s.Paths.State)
	if err != nil || saved.Packages["fmx6"].Route != state.Prebuilt {
		t.Errorf("state not saved: %v %+v", err, saved)
	}
	if exists(filepath.Join(s.Paths.Managed, ".staging")) {
		t.Error("staging left behind")
	}
	if len(fractions) == 0 || fractions[len(fractions)-1] != 1 {
		t.Errorf("progress = %v", fractions)
	}
}

func TestApplyNeedsConfirmation(t *testing.T) {
	s := applyService(t)
	p := Plan{Package: "wild", Route: RoutePrebuilt, Confirm: []string{ConfirmUnreviewed}}

	_, err := s.Apply(context.Background(), p)
	var ce *ConfirmationError
	if !errors.As(err, &ce) || len(ce.Needed) != 1 || ce.Needed[0] != ConfirmUnreviewed {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "needs confirmation: unreviewed" {
		t.Errorf("message = %q", err.Error())
	}
	if exists(s.Paths.Managed) || exists(s.Paths.State) || len(s.State.Packages) != 0 {
		t.Error("something was written")
	}
	p.Confirm = []string{ConfirmDuplicates}
	if _, err := s.Apply(context.Background(), p); !errors.As(err, &ce) {
		t.Errorf("duplicates without a choice: %v", err)
	}
}

func TestApplyQuarkRouteChangesNothing(t *testing.T) {
	s := applyService(t)
	p := Plan{Package: "Panola", Route: RouteQuark, AfterInstall: []string{AfterRecompile}}
	res, err := s.Apply(context.Background(), p)
	if err != nil || res.InstalledTo != "" || res.Plan.AfterInstall[0] != AfterRecompile {
		t.Errorf("res = %+v err = %v", res, err)
	}
	if exists(s.Paths.Managed) || len(s.State.Packages) != 0 {
		t.Error("quark route touched the disk or state")
	}
}

// buildFake answers git and cmake; cmake --build writes plugins into the
// install prefix named at configure time.
func buildFake(t *testing.T, plugins ...string) *run.Fake {
	t.Helper()
	prefix := ""
	return &run.Fake{
		Script: map[string]run.FakeResult{
			"git ls-remote *": {Out: "aaa\trefs/tags/Version-3.14.1\n"},
			"*":               {},
		},
		OnRun: func(c run.Cmd) error {
			if c.Name != "cmake" {
				return nil
			}
			for _, a := range c.Args {
				if p, ok := strings.CutPrefix(a, "-DCMAKE_INSTALL_PREFIX="); ok {
					prefix = p
				}
			}
			if c.Args[0] == "--build" {
				for _, name := range plugins {
					writeTestFile(t, filepath.Join(prefix, name), "binary")
				}
			}
			return nil
		},
	}
}

// cachedHeaders pretends the SuperCollider 3.14.1 source is already cached.
func cachedHeaders(t *testing.T, s *Service) {
	t.Helper()
	dir := filepath.Join(s.Paths.Headers, "Version-3.14.1")
	writeTestFile(t, filepath.Join(dir, "include", "plugin_interface", "SC_InterfaceTable.h"), "sc_api_version = 3")
	writeTestFile(t, filepath.Join(dir, ".sc-deken-complete"), "")
}

func TestApplyLocalBuild(t *testing.T) {
	s := applyService(t)
	s.Runner = buildFake(t, "Raw.scx")
	cachedHeaders(t, s)
	// A crashed earlier run left a work folder behind.
	work := filepath.Join(s.Paths.Work, "Wild-1.0.0")
	writeTestFile(t, filepath.Join(work, "src", "stale"), "x")
	p := Plan{Package: "Wild", Version: "1.0.0", Commit: fmx6Commit, Kind: recipe.KindPlugin, Route: RouteLocalBuild}

	if _, err := s.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(s.Paths.Managed, "wild", "Raw.scx")) {
		t.Error("plugin not installed")
	}
	if exists(work) || exists(filepath.Join(s.Paths.Managed, ".staging")) {
		t.Error("work or staging folder left behind")
	}
	if e := s.State.Packages["wild"]; e.Route != state.LocalBuild || e.Repo == "" {
		t.Errorf("entry = %+v", e)
	}
}

func TestApplyFailureLeavesNoTrace(t *testing.T) {
	s := applyService(t)
	url, _ := serveZip(t, map[string]string{"FMX6.scx": "binary"})
	p := prebuiltPlan("fmx6", recipe.KindPlugin, url, strings.Repeat("0", 64))

	if _, err := s.Apply(context.Background(), p); err == nil {
		t.Fatal("want hash mismatch error")
	}
	if exists(filepath.Join(s.Paths.Managed, "fmx6")) || exists(filepath.Join(s.Paths.Managed, ".staging")) {
		t.Error("files left behind")
	}
	if len(s.State.Packages) != 0 || exists(s.Paths.State) {
		t.Error("state changed")
	}
}

func TestApplyFailedBuildCleansWorkAndStaging(t *testing.T) {
	s := applyService(t)
	cachedHeaders(t, s)
	fake := buildFake(t) // builds nothing, so build.Run fails
	s.Runner = fake
	p := Plan{Package: "Wild", Version: "1.0.0", Commit: fmx6Commit, Kind: recipe.KindPlugin, Route: RouteLocalBuild}

	if _, err := s.Apply(context.Background(), p); err == nil {
		t.Fatal("want error")
	}
	if exists(filepath.Join(s.Paths.Work, "Wild-1.0.0")) || exists(filepath.Join(s.Paths.Managed, ".staging")) {
		t.Error("work or staging left behind")
	}
}

func TestApplyPluginQuarkStripsClasses(t *testing.T) {
	s := applyService(t)
	url, sum := serveZip(t, map[string]string{
		"X.scx":                  "binary",
		"X.sc":                   "X { }",
		"extra/Y.SC":             "Y { }",
		"HelpSource/X.schelp":    "help",
		"HelpSource/sub/Z.txt":   "help",
		"data/notes.txt":         "keep",
		"data/HelpSource/Q.html": "x",
	})
	_, err := s.Apply(context.Background(), prebuiltPlan("Hybrid", recipe.KindPluginQuark, url, sum))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Paths.Managed, "hybrid")
	for _, gone := range []string{"X.sc", "extra/Y.SC", "HelpSource", "data/HelpSource"} {
		if exists(filepath.Join(dir, filepath.FromSlash(gone))) {
			t.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{"X.scx", "data/notes.txt"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(kept))) {
			t.Errorf("%s was removed", kept)
		}
	}
}

func duplicatePlan(t *testing.T, s *Service) (Plan, string) {
	t.Helper()
	url, sum := serveZip(t, map[string]string{"FMX6.scx": "new"})
	dup := filepath.Join(s.Ctx.ExtensionDir, "FMX6-by-hand")
	writeTestFile(t, filepath.Join(dup, "FMX6.scx"), "mine")
	p := prebuiltPlan("fmx6", recipe.KindPlugin, url, sum)
	p.Confirm = []string{ConfirmDuplicates}
	p.Duplicates = []Duplicate{{Path: dup, Matched: "FMX6.scx"}}
	return p, dup
}

func TestApplyReplaceBacksUpDuplicate(t *testing.T) {
	s := applyService(t)
	p, dup := duplicatePlan(t, s)
	res, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	if exists(dup) {
		t.Error("duplicate still in place")
	}
	if len(res.BackedUp) != 1 || !strings.HasPrefix(res.BackedUp[0], s.Paths.Backup) {
		t.Fatalf("BackedUp = %v", res.BackedUp)
	}
	got, err := os.ReadFile(filepath.Join(res.BackedUp[0], "FMX6.scx"))
	if err != nil || string(got) != "mine" {
		t.Errorf("backup content = %q, %v", got, err)
	}
	if !exists(filepath.Join(s.Paths.Managed, "fmx6", "FMX6.scx")) {
		t.Error("new copy missing")
	}
}

func TestFreePathAddsCounter(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "a")
	writeTestFile(t, base, "x")
	writeTestFile(t, base+"-2", "x")
	if got := freePath(base); got != base+"-3" {
		t.Errorf("freePath = %s", got)
	}
}

func TestApplyKeepRecordsExternal(t *testing.T) {
	s := applyService(t)
	p, dup := duplicatePlan(t, s)
	res, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "keep"})
	if err != nil || !res.Kept {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	e := s.State.Packages["fmx6"]
	if e.Route != state.External || e.Path != dup || e.Version != "" || e.Commit != "" {
		t.Errorf("entry = %+v", e)
	}
	if !exists(filepath.Join(dup, "FMX6.scx")) || exists(filepath.Join(s.Paths.Managed, "fmx6")) {
		t.Error("keep must not touch the disk")
	}
}

// lockedError is what the OS reports for a file another program holds open.
func lockedError() error {
	if runtime.GOOS == "windows" {
		return &os.LinkError{Op: "rename", Err: syscall.Errno(32)}
	}
	return fs.ErrPermission
}

func TestApplyUpdateRestoresOnSwapFailure(t *testing.T) {
	s := applyService(t)
	old := filepath.Join(s.Paths.Managed, "fmx6", "FMX6.scx")
	writeTestFile(t, old, "old version")
	url, sum := serveZip(t, map[string]string{"FMX6.scx": "new version"})
	real := rename
	rename = func(from, to string) error {
		if strings.Contains(from, ".staging") {
			return lockedError()
		}
		return real(from, to)
	}
	t.Cleanup(func() { rename = real })

	_, err := s.Apply(context.Background(), prebuiltPlan("fmx6", recipe.KindPlugin, url, sum))
	if !errors.Is(err, ErrInUse) || !strings.Contains(err.Error(), "s.quit") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(old); !bytes.Equal(got, []byte("old version")) {
		t.Errorf("old install not restored: %q", got)
	}
	if exists(filepath.Join(s.Paths.Managed, ".staging")) || exists(filepath.Join(s.Paths.Managed, ".old-fmx6")) {
		t.Error("staging or .old left behind")
	}
	if len(s.State.Packages) != 0 {
		t.Error("state changed")
	}
}

func TestApplyUpdateReplacesOldVersion(t *testing.T) {
	s := applyService(t)
	writeTestFile(t, filepath.Join(s.Paths.Managed, "fmx6", "Gone.scx"), "old")
	url, sum := serveZip(t, map[string]string{"FMX6.scx": "new"})
	if _, err := s.Apply(context.Background(), prebuiltPlan("fmx6", recipe.KindPlugin, url, sum)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Paths.Managed, "fmx6")
	if exists(filepath.Join(dir, "Gone.scx")) || !exists(filepath.Join(dir, "FMX6.scx")) || exists(dir+"/../.old-fmx6") {
		t.Error("update did not replace the old version cleanly")
	}
}

// crossDeviceError is what the OS reports for a rename between drives.
func crossDeviceError() error {
	if runtime.GOOS == "windows" {
		return &os.LinkError{Op: "rename", Err: syscall.Errno(17)}
	}
	return &os.LinkError{Op: "rename", Err: syscall.EXDEV}
}

// failRename makes rename fail with err for every move out of from.
func failRename(t *testing.T, err error, from ...string) {
	t.Helper()
	real := rename
	rename = func(src, dst string) error {
		for _, f := range from {
			if src == f {
				return err
			}
		}
		return real(src, dst)
	}
	t.Cleanup(func() { rename = real })
}

func TestApplyRejectsUnknownChoice(t *testing.T) {
	s := applyService(t)
	_, err := s.ApplyWith(context.Background(), Plan{Package: "fmx6", Route: RoutePrebuilt}, Choices{OnDuplicate: "yes"})
	want := `unknown choice for existing copies: "yes" (use replace or keep)`
	if err == nil || err.Error() != want {
		t.Errorf("err = %v", err)
	}
}

func TestApplyBackupFailureRestoresEverything(t *testing.T) {
	s := applyService(t)
	p, dup := duplicatePlan(t, s)
	second := filepath.Join(s.Ctx.ExtensionDir, "second")
	writeTestFile(t, filepath.Join(second, "FMX6.scx"), "other")
	p.Duplicates = append(p.Duplicates, Duplicate{Path: second, Matched: "FMX6.scx"})
	failRename(t, lockedError(), second)

	_, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "replace"})
	if err == nil || !strings.Contains(err.Error(), "Couldn't move "+second+" out of the way") {
		t.Fatalf("err = %v", err)
	}
	if errors.Is(err, ErrInUse) || strings.Contains(err.Error(), "s.quit") {
		t.Errorf("backup failure was reported as server in use: %v", err)
	}
	for _, d := range []string{dup, second} {
		if !exists(filepath.Join(d, "FMX6.scx")) {
			t.Errorf("%s not restored", d)
		}
	}
	if exists(filepath.Join(s.Paths.Managed, "fmx6")) || exists(filepath.Join(s.Paths.Managed, ".staging")) {
		t.Error("new install or staging left behind")
	}
	if len(s.State.Packages) != 0 || exists(s.Paths.State) {
		t.Error("state changed")
	}
}

func TestApplyBackupAcrossDrivesCopiesThenRemoves(t *testing.T) {
	s := applyService(t)
	p, dup := duplicatePlan(t, s)
	writeTestFile(t, filepath.Join(dup, "sub", "deep.txt"), "deep")
	failRename(t, crossDeviceError(), dup)

	res, err := s.ApplyWith(context.Background(), p, Choices{OnDuplicate: "replace"})
	if err != nil {
		t.Fatal(err)
	}
	if exists(dup) {
		t.Error("source not removed after copy")
	}
	if len(res.BackedUp) != 1 {
		t.Fatalf("BackedUp = %v", res.BackedUp)
	}
	for file, want := range map[string]string{"FMX6.scx": "mine", "sub/deep.txt": "deep"} {
		got, err := os.ReadFile(filepath.Join(res.BackedUp[0], filepath.FromSlash(file)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", file, got, err)
		}
	}
}

func TestSwapRestoreFailureNamesOldFolder(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "fmx6")
	staging := filepath.Join(root, ".staging", "fmx6")
	old := filepath.Join(root, ".old-fmx6")
	writeTestFile(t, filepath.Join(target, "a.scx"), "old")
	writeTestFile(t, filepath.Join(staging, "a.scx"), "new")
	failRename(t, lockedError(), staging, old)

	err := swapIn(staging, target)
	if err == nil || !strings.Contains(err.Error(), "your previous copy is in "+old) || !errors.Is(err, ErrInUse) {
		t.Fatalf("err = %v", err)
	}
	if !exists(filepath.Join(old, "a.scx")) {
		t.Error("previous copy lost")
	}
}

func TestSwapRestoresInterruptedLeftover(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "fmx6")
	staging := filepath.Join(root, ".staging", "fmx6")
	old := filepath.Join(root, ".old-fmx6")
	writeTestFile(t, filepath.Join(old, "a.scx"), "only copy")
	writeTestFile(t, filepath.Join(staging, "a.scx"), "new")
	failRename(t, lockedError(), staging)

	if err := swapIn(staging, target); err == nil {
		t.Fatal("want error")
	}
	got, err := os.ReadFile(filepath.Join(target, "a.scx"))
	if err != nil || string(got) != "only copy" {
		t.Errorf("leftover not restored: %q, %v", got, err)
	}
}
