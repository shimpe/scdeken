package build

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
)

// writeFile creates a file and its folders.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// installFake answers git, cmake, lipo and vcpkg; every cmake --build writes
// a plugin into the install prefix that the earlier configure named.
func installFake(t *testing.T, plugins ...string) *run.Fake {
	t.Helper()
	prefix := ""
	ok := run.FakeResult{}
	return &run.Fake{
		Script: map[string]run.FakeResult{"git *": ok, "cmake *": ok, "lipo *": ok, "*": ok},
		OnRun: func(c run.Cmd) error {
			if c.Name != "cmake" {
				return nil
			}
			for _, a := range c.Args {
				if p, found := strings.CutPrefix(a, "-DCMAKE_INSTALL_PREFIX="); found {
					prefix = p
				}
			}
			if c.Args[0] == "--build" {
				for _, name := range plugins {
					writeFile(t, filepath.Join(prefix, name), "binary")
				}
			}
			return nil
		},
	}
}

func baseOptions(t *testing.T) Options {
	t.Helper()
	root := t.TempDir()
	return Options{
		RepoURL:    "https://example.com/pkg.git",
		Commit:     "abc123",
		Build:      recipe.Build{System: "cmake", SCPathVar: "SC_PATH"},
		HeadersDir: filepath.Join(root, "headers"),
		WorkDir:    filepath.Join(root, "work"),
		OutDir:     filepath.Join(root, "out"),
		OS:         "linux",
		Arch:       "x86_64",
	}
}

// calls renders recorded commands as "name arg arg ...".
func calls(f *run.Fake) [][]string {
	var out [][]string
	for _, c := range f.Calls {
		out = append(out, append([]string{c.Name}, c.Args...))
	}
	return out
}

func TestBuildCommandSequence(t *testing.T) {
	o := baseOptions(t)
	o.Build.CMakeFlags = []string{"-DFOO=1"}
	o.ExtraCMakeArgs = []string{"-DBAR=2"}
	f := installFake(t, "Pkg/Pkg.scx")
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(o.WorkDir, "src")
	bd := filepath.Join(o.WorkDir, "build-x86_64")
	want := [][]string{
		{"git", "init", src},
		{"git", "-C", src, "remote", "add", "origin", o.RepoURL},
		{"git", "-C", src, "fetch", "--depth", "1", "origin", "abc123"},
		{"git", "-C", src, "checkout", "--quiet", "FETCH_HEAD"},
		{"cmake", "-S", src, "-B", bd, "-DCMAKE_BUILD_TYPE=Release", "-DSC_PATH=" + o.HeadersDir,
			"-DCMAKE_INSTALL_PREFIX=" + o.OutDir, "-DFOO=1", "-DBAR=2"},
		{"cmake", "--build", bd, "--config", "Release", "--target", "install", "--parallel"},
	}
	if got := calls(f); !reflect.DeepEqual(got, want) {
		t.Errorf("commands:\n got %q\nwant %q", got, want)
	}
}

func TestBuildNativeArchDirName(t *testing.T) {
	o := baseOptions(t)
	o.Arch = ""
	f := installFake(t, "Pkg.scx")
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(o.WorkDir, "build-native")
	if got := f.Calls[4].Args[3]; got != want {
		t.Errorf("build dir = %q, want %q", got, want)
	}
}

func TestBuildSubmodulesAndSubdir(t *testing.T) {
	o := baseOptions(t)
	o.Build.Submodules = true
	o.Build.Subdir = "supercollider"
	f := installFake(t, "Pkg.scx")
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(o.WorkDir, "src")
	sub := []string{"git", "-C", src, "submodule", "update", "--init", "--recursive", "--depth", "1"}
	if got := calls(f)[4]; !reflect.DeepEqual(got, sub) {
		t.Errorf("fifth call = %q, want %q", got, sub)
	}
	if got, want := f.Calls[5].Args[1], filepath.Join(src, "supercollider"); got != want {
		t.Errorf("cmake -S = %q, want %q", got, want)
	}
}

func TestBuildCommandsWithSpacedPaths(t *testing.T) {
	o := baseOptions(t)
	o.WorkDir = filepath.Join(t.TempDir(), "Stefaan Himpe", "work dir")
	o.HeadersDir = filepath.Join("C:\\Users", "Stefaan Himpe", "h")
	f := installFake(t, "Pkg.scx")
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	cfg := f.Calls[4].Args
	if want := "-DSC_PATH=" + o.HeadersDir; !contains(cfg, want) {
		t.Errorf("configure args %q lack %q", cfg, want)
	}
	if want := filepath.Join(o.WorkDir, "src"); cfg[1] != want {
		t.Errorf("-S = %q, want %q", cfg[1], want)
	}
	if want := filepath.Join(o.WorkDir, "src"); f.Calls[0].Args[1] != want {
		t.Errorf("git init path = %q, want %q", f.Calls[0].Args[1], want)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestBuildUniversalLipo(t *testing.T) {
	o := baseOptions(t)
	o.OS, o.Arch = "macos", "universal"
	f := installFake(t, "Pkg/Pkg.scx", "Pkg/Pkg_supernova.scx")
	res, err := Run(context.Background(), f, o)
	if err != nil {
		t.Fatal(err)
	}
	var configures, lipos [][]string
	for _, c := range calls(f) {
		switch {
		case c[0] == "cmake" && c[1] == "-S":
			configures = append(configures, c)
		case c[0] == "lipo":
			lipos = append(lipos, c)
		}
	}
	if len(configures) != 2 || !contains(configures[0], "-DCMAKE_OSX_ARCHITECTURES=arm64") ||
		!contains(configures[1], "-DCMAKE_OSX_ARCHITECTURES=x86_64") {
		t.Errorf("configures = %q", configures)
	}
	if !contains(configures[0], "-DCMAKE_INSTALL_PREFIX="+filepath.Join(o.WorkDir, "out-arm64")) {
		t.Errorf("arm64 prefix wrong: %q", configures[0])
	}
	if len(lipos) != 2 {
		t.Fatalf("lipo calls = %q, want 2", lipos)
	}
	rel := filepath.Join("Pkg", "Pkg.scx")
	want := []string{"lipo", "-create", filepath.Join(o.WorkDir, "out-arm64", rel),
		filepath.Join(o.WorkDir, "out-x86_64", rel), "-output", filepath.Join(o.OutDir, rel)}
	if !reflect.DeepEqual(lipos[0], want) {
		t.Errorf("lipo = %q, want %q", lipos[0], want)
	}
	if !reflect.DeepEqual(res.PluginFiles, []string{"Pkg.scx", "Pkg_supernova.scx"}) {
		t.Errorf("plugins = %q", res.PluginFiles)
	}
}

func TestBuildUniversalMismatch(t *testing.T) {
	o := baseOptions(t)
	o.OS, o.Arch = "macos", "universal"
	f := installFake(t, "Pkg/Pkg.scx")
	inner := f.OnRun
	f.OnRun = func(c run.Cmd) error {
		if err := inner(c); err != nil {
			return err
		}
		if c.Args[0] == "--build" && strings.Contains(c.Args[1], "build-arm64") {
			writeFile(t, filepath.Join(o.WorkDir, "out-arm64", "Pkg", "Only.scx"), "x")
		}
		return nil
	}
	_, err := Run(context.Background(), f, o)
	if err == nil || !strings.Contains(err.Error(), "Only.scx") {
		t.Fatalf("err = %v, want one naming Only.scx", err)
	}
}

func TestBuildVcpkg(t *testing.T) {
	o := baseOptions(t)
	o.OS = "windows"
	o.VcpkgRoot = filepath.Join(t.TempDir(), "vcpkg")
	o.Requires = map[string]recipe.Requirement{
		"fftw": {Vcpkg: "fftw3"},
		"none": {Apt: "libnone-dev"},
	}
	f := installFake(t, "Pkg.dll")
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(o.VcpkgRoot, "vcpkg.exe"), "install", "fftw3", "--triplet", "x64-windows-static"}
	if got := calls(f)[4]; !reflect.DeepEqual(got, want) {
		t.Errorf("vcpkg call = %q, want %q", got, want)
	}
	cfg := f.Calls[5].Args
	toolchain := filepath.Join(o.VcpkgRoot, "scripts", "buildsystems", "vcpkg.cmake")
	for _, w := range []string{"-DCMAKE_TOOLCHAIN_FILE=" + toolchain, "-DVCPKG_TARGET_TRIPLET=x64-windows-static",
		"-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded"} {
		if !contains(cfg, w) {
			t.Errorf("configure args %q lack %q", cfg, w)
		}
	}
}

func TestBuildNoPlugins(t *testing.T) {
	o := baseOptions(t)
	_, err := Run(context.Background(), installFake(t), o)
	if err == nil || !strings.Contains(err.Error(), "produced no plugin files") {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildOutDirMustNotExist(t *testing.T) {
	o := baseOptions(t)
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), installFake(t, "Pkg.scx"), o); err == nil {
		t.Fatal("want an error when OutDir exists")
	}
}

func TestFindPluginFiles(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"x/a.scx", "x/y/b_supernova.scx", "c.so", "d.dll", "e.txt"} {
		writeFile(t, filepath.Join(dir, p), "x")
	}
	got, err := FindPluginFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.scx", "b_supernova.scx", "c.so", "d.dll"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTriplet(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "x86_64"}: "x64-linux", {"linux", "arm64"}: "arm64-linux",
		{"macos", "arm64"}: "arm64-osx", {"macos", "x86_64"}: "x64-osx",
		{"windows", "x86_64"}: "x64-windows-static",
	}
	for in, want := range cases {
		if got := Triplet(in[0], in[1]); got != want {
			t.Errorf("Triplet(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildCopiesLicense(t *testing.T) {
	o := baseOptions(t)
	f := installFake(t, "Pkg.scx")
	inner := f.OnRun
	f.OnRun = func(c run.Cmd) error {
		if c.Name == "git" && c.Args[0] == "init" {
			writeFile(t, filepath.Join(c.Args[1], "LICENSE"), "MIT")
		}
		return inner(c)
	}
	if _, err := Run(context.Background(), f, o); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(o.OutDir, "LICENSE")); err != nil {
		t.Errorf("license not copied: %v", err)
	}
}
