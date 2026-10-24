// Package build compiles a plugin from source with CMake. The same code runs
// on a musician's machine (no prebuilt binary exists) and in CI.
package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/run"
)

// Options describes one build.
type Options struct {
	RepoURL, Commit string
	Build           recipe.Build
	Requires        map[string]recipe.Requirement
	HeadersDir      string
	WorkDir         string // scratch: source + build trees
	OutDir          string // install prefix; must not exist yet
	OS, Arch        string // platform.HostOS(); Arch "" = native, or "x86_64"|"arm64"|"universal" (universal only on macos)
	VcpkgRoot       string // "" = no vcpkg
	ExtraCMakeArgs  []string
	Log             io.Writer // nil discards
}

// Result lists what the build produced.
type Result struct{ PluginFiles []string } // base names, sorted

// noPluginsMsg is shown when the install step leaves nothing loadable behind.
const noPluginsMsg = "the build finished but produced no plugin files (.scx/.so/.dll); the recipe's build settings probably need fixing"

// Run fetches the source at the pinned commit, builds it and installs it into
// o.OutDir.
func Run(ctx context.Context, r run.Runner, o Options) (Result, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if _, err := os.Stat(o.OutDir); err == nil {
		return Result{}, fmt.Errorf("output folder %s already exists; remove it or choose another", o.OutDir)
	}
	if err := os.MkdirAll(o.WorkDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create work folder: %w", err)
	}
	if err := o.fetchSource(ctx, r); err != nil {
		return Result{}, err
	}
	if err := o.compile(ctx, r); err != nil {
		return Result{}, err
	}
	files, err := FindPluginFiles(o.OutDir)
	if err != nil {
		return Result{}, err
	}
	if len(files) == 0 {
		return Result{}, errors.New(noPluginsMsg)
	}
	if err := copyLicenses(o.srcDir(), o.OutDir); err != nil {
		return Result{}, err
	}
	return Result{PluginFiles: files}, nil
}

func (o Options) srcDir() string { return filepath.Join(o.WorkDir, "src") }

// fetchSource downloads exactly the pinned commit into <WorkDir>/src.
func (o Options) fetchSource(ctx context.Context, r run.Runner) error {
	src := o.srcDir()
	steps := [][]string{
		{"init", src},
		{"-C", src, "remote", "add", "origin", o.RepoURL},
		{"-C", src, "fetch", "--depth", "1", "origin", o.Commit},
		{"-C", src, "checkout", "--quiet", "FETCH_HEAD"},
	}
	if o.Build.Submodules {
		steps = append(steps, []string{"-C", src, "submodule", "update", "--init", "--recursive", "--depth", "1"})
	}
	for _, args := range steps {
		if err := r.Run(ctx, run.Cmd{Name: "git", Args: args, Log: o.Log}); err != nil {
			return fmt.Errorf("fetch source %s: %w", o.RepoURL, err)
		}
	}
	return nil
}

// compile builds for the requested architecture(s) and leaves the result in OutDir.
func (o Options) compile(ctx context.Context, r run.Runner) error {
	if o.Arch == "universal" {
		return o.compileUniversal(ctx, r)
	}
	return o.buildArch(ctx, r, o.Arch, o.OutDir)
}

// buildArch runs vcpkg (if used), configures, builds and installs into prefix.
// arch "" means the machine's own architecture.
func (o Options) buildArch(ctx context.Context, r run.Runner, arch, prefix string) error {
	triplet := ""
	if o.usesVcpkg() {
		actual := firstNonEmpty(arch, nativeArch())
		if triplet = Triplet(o.OS, actual); triplet == "" {
			return fmt.Errorf("vcpkg has no setup for %s on %s", actual, o.OS)
		}
		if err := o.vcpkgInstall(ctx, r, triplet); err != nil {
			return err
		}
	}
	buildDir := filepath.Join(o.WorkDir, "build-"+firstNonEmpty(arch, "native"))
	configure := run.Cmd{Name: "cmake", Args: o.configureArgs(arch, buildDir, prefix, triplet), Log: o.Log}
	if err := r.Run(ctx, configure); err != nil {
		return fmt.Errorf("configure with cmake: %w", err)
	}
	install := run.Cmd{
		Name: "cmake",
		Args: []string{"--build", buildDir, "--config", "Release", "--target", "install", "--parallel"},
		Log:  o.Log,
	}
	if err := r.Run(ctx, install); err != nil {
		return fmt.Errorf("build with cmake: %w", err)
	}
	return nil
}

// configureArgs assembles the cmake configure command line.
func (o Options) configureArgs(arch, buildDir, prefix, triplet string) []string {
	args := []string{
		"-S", filepath.Join(o.srcDir(), o.Build.Subdir),
		"-B", buildDir,
		"-DCMAKE_BUILD_TYPE=Release",
		"-D" + o.Build.SCPathVar + "=" + o.HeadersDir,
		"-DCMAKE_INSTALL_PREFIX=" + prefix,
	}
	if o.OS == "macos" && arch != "" {
		args = append(args, "-DCMAKE_OSX_ARCHITECTURES="+arch)
	}
	if triplet != "" {
		args = append(args, o.vcpkgCMakeArgs(triplet)...)
	}
	args = append(args, o.Build.CMakeFlags...)
	return append(args, o.ExtraCMakeArgs...)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// isPlugin reports whether a file name is a loadable SuperCollider plugin.
func isPlugin(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".scx", ".so", ".dll":
		return true
	}
	return false
}

// pluginPaths lists plugin files under dir as relative paths, sorted.
// A missing dir has none.
func pluginPaths(dir string) ([]string, error) {
	var rels []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !isPlugin(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("look for plugin files in %s: %w", dir, err)
	}
	sort.Strings(rels)
	return rels, nil
}

// FindPluginFiles lists the base names of every .scx, .so and .dll under dir
// (recursively), sorted.
func FindPluginFiles(dir string) ([]string, error) {
	rels, err := pluginPaths(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, rel := range rels {
		names = append(names, filepath.Base(rel))
	}
	sort.Strings(names)
	return names, nil
}

// copyLicenses copies LICENSE*, COPYING* and LICENCE* files from the source root.
func copyLicenses(src, out string) error {
	for _, pattern := range []string{"LICENSE*", "COPYING*", "LICENCE*"} {
		matches, err := filepath.Glob(filepath.Join(src, pattern))
		if err != nil {
			return fmt.Errorf("find license files: %w", err)
		}
		for _, m := range matches {
			if info, err := os.Stat(m); err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := copyFile(m, filepath.Join(out, filepath.Base(m))); err != nil {
				return fmt.Errorf("copy license: %w", err)
			}
		}
	}
	return nil
}
