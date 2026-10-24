// Package doctor checks that the tools needed to build SuperCollider plugins
// are installed, and tells the musician the exact command to install any that
// are missing.
package doctor

import (
	"context"
	"regexp"
	"sort"

	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/version"
)

// MinCMake is the oldest CMake version that can build plugins.
const MinCMake = "3.12"

const neededFor = "building plugins"

// Item is the result of checking one tool or library.
type Item struct {
	Name      string `json:"name"`     // "git", "cmake", "compiler", "build-driver", or requirement key
	Label     string `json:"label"`    // musician-friendly, e.g. "CMake (build tool)"
	Found     bool   `json:"found"`    // present and good enough
	Verified  bool   `json:"verified"` // false = "could not check"
	Version   string `json:"version,omitempty"`
	NeededFor string `json:"needed_for"`     // "building plugins"
	Hint      string `json:"hint,omitempty"` // exact install command
}

// Report is the outcome of a doctor run.
type Report struct {
	OK    bool   `json:"ok"` // every verified item was found
	Items []Item `json:"items"`
}

// Check looks for the build tools on this machine, plus the libraries the
// given requirements name (keyed by requirement name).
func Check(ctx context.Context, r run.Runner, env Env, reqs map[string]recipe.Requirement) Report {
	items := []Item{checkGit(ctx, r, env), checkCMake(ctx, r, env)}
	items = append(items, checkToolchain(ctx, r, env)...)
	items = append(items, checkRequirements(ctx, r, env, reqs)...)

	report := Report{OK: true, Items: items}
	for _, it := range items {
		if it.Verified && !it.Found {
			report.OK = false
		}
	}
	return report
}

// newItem starts a verified item that is not yet found.
func newItem(name, label string) Item {
	return Item{Name: name, Label: label, Verified: true, NeededFor: neededFor}
}

// withHint adds the install hint to an item that was not found.
func withHint(it Item, hint string) Item {
	if !it.Found {
		it.Hint = hint
	}
	return it
}

func checkGit(ctx context.Context, r run.Runner, env Env) Item {
	it := newItem("git", "Git (downloads source code)")
	out, err := r.Output(ctx, run.Cmd{Name: "git", Args: []string{"--version"}})
	if err == nil {
		it.Found, it.Version = true, parseVersion(out)
	}
	return withHint(it, hintFor(gitHints, env, it.Label))
}

func checkCMake(ctx context.Context, r run.Runner, env Env) Item {
	it := newItem("cmake", "CMake (build tool)")
	out, err := r.Output(ctx, run.Cmd{Name: "cmake", Args: []string{"--version"}})
	if err == nil {
		it.Version = parseVersion(out)
		it.Found = atLeast(it.Version, MinCMake)
	}
	return withHint(it, hintFor(cmakeHints, env, it.Label))
}

// versionPattern finds the first dotted number, e.g. 2.47.1 in "git version 2.47.1.windows.1".
var versionPattern = regexp.MustCompile(`\d+(\.\d+){1,2}`)

// parseVersion extracts "major.minor[.patch]" from a tool's version output.
func parseVersion(out string) string {
	return versionPattern.FindString(out)
}

// atLeast reports whether have is a version >= min. An unreadable version
// counts as good enough: the tool runs, we just cannot judge its age.
func atLeast(have, min string) bool {
	if have == "" {
		return true
	}
	h, err := version.Parse(have)
	if err != nil {
		return true
	}
	m, err := version.Parse(min)
	if err != nil {
		return true
	}
	return h.Compare(m) >= 0
}

// checkToolchain checks the compiler and build driver for the OS.
func checkToolchain(ctx context.Context, r run.Runner, env Env) []Item {
	switch env.OS {
	case "darwin", "macos":
		return []Item{checkMacCompiler(ctx, r, env), checkBuildDriver(r, env)}
	case "windows":
		return []Item{checkWindowsCompiler(ctx, r, env)}
	case "linux":
		return []Item{checkLinuxCompiler(r, env), checkBuildDriver(r, env)}
	}
	return nil
}

func checkMacCompiler(ctx context.Context, r run.Runner, env Env) Item {
	it := newItem("compiler", "C++ compiler")
	_, err := r.Output(ctx, run.Cmd{Name: "xcode-select", Args: []string{"-p"}})
	it.Found = err == nil
	return withHint(it, hintFor(compilerHints, env, it.Label))
}

func checkWindowsCompiler(ctx context.Context, r run.Runner, env Env) Item {
	it := newItem("compiler", "C++ compiler")
	if env.VSWhere != "" {
		args := []string{"-latest", "-products", "*", "-requires",
			"Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath"}
		out, err := r.Output(ctx, run.Cmd{Name: env.VSWhere, Args: args})
		it.Found = err == nil && out != ""
	}
	return withHint(it, hintFor(compilerHints, env, it.Label))
}

func checkLinuxCompiler(r run.Runner, env Env) Item {
	it := newItem("compiler", "C++ compiler")
	it.Found = anyOnPath(r, "c++", "g++", "clang++")
	return withHint(it, hintFor(compilerHints, env, it.Label))
}

// checkBuildDriver looks for make or ninja. Their install hint is the compiler's.
func checkBuildDriver(r run.Runner, env Env) Item {
	it := newItem("build-driver", "make or ninja (build driver)")
	it.Found = anyOnPath(r, "make", "ninja")
	return withHint(it, hintFor(compilerHints, env, it.Label))
}

// anyOnPath reports whether at least one of the programs is on PATH.
func anyOnPath(r run.Runner, names ...string) bool {
	for _, name := range names {
		if _, err := r.LookPath(name); err == nil {
			return true
		}
	}
	return false
}

// checkRequirements checks each requirement, in key order.
func checkRequirements(ctx context.Context, r run.Runner, env Env, reqs map[string]recipe.Requirement) []Item {
	keys := make([]string, 0, len(reqs))
	for key := range reqs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]Item, 0, len(keys))
	for _, key := range keys {
		items = append(items, checkRequirement(ctx, r, env, key, reqs[key]))
	}
	return items
}

// checkRequirement asks pkg-config about one library. Without pkg-config or a
// pkg-config name the library cannot be checked, so only the hint is given.
func checkRequirement(ctx context.Context, r run.Runner, env Env, key string, req recipe.Requirement) Item {
	it := newItem(key, key+" library")
	hint := requirementHint(req, env, it.Label)
	if req.PkgConfig == "" || !anyOnPath(r, "pkg-config") {
		it.Verified = false
		it.Hint = hint
		return it
	}
	_, err := r.Output(ctx, run.Cmd{Name: "pkg-config", Args: []string{"--exists", req.PkgConfig}})
	it.Found = err == nil
	return withHint(it, hint)
}
