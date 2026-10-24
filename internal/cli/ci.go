package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shimpe/scdeken/internal/archive"
	"github.com/shimpe/scdeken/internal/build"
	"github.com/shimpe/scdeken/internal/indexer"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
)

// The commands the recipes repository's CI workflows call.
func init() {
	commands["ci-matrix"] = command{
		usage: "ci matrix --recipes <folder> --base <git-revision>",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Maint.Recipes, "recipes", "", "folder of recipe files")
			fs.StringVar(&req.Maint.Base, "base", "", "git revision to compare with")
		},
		run: runCIMatrix,
	}
	commands["ci-platforms"] = command{usage: "ci platforms", run: runCIPlatforms}
	commands["ci-pack"] = command{
		usage: "ci pack <folder> <zip-file>",
		args: func(pos []string, req *Request) error {
			if len(pos) != 2 {
				return usageError{"expected a folder and a zip file name"}
			}
			req.Path, req.Out = pos[0], pos[1]
			return nil
		},
		run: runCIPack,
	}
	commands["build"] = command{
		usage: "build --recipe <file> --version <version> --out <folder> [--arch <arch>] [--sc-version " + defaultSCVersion + "] [--vcpkg-root <folder>]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Path, "recipe", "", "recipe file")
			fs.StringVar(&req.Version, "version", "", "approved version to build")
			fs.StringVar(&req.Out, "out", "", "folder for the finished build; must not exist")
			fs.StringVar(&req.Maint.Arch, "arch", "", "x86_64, arm64 or universal (default: this machine)")
			fs.StringVar(&req.SCVersion, "sc-version", "", "SuperCollider version to build against")
			fs.StringVar(&req.Maint.VcpkgRoot, "vcpkg-root", "", "vcpkg checkout for libraries")
		},
		run: runBuild,
	}
}

// jsonText is an answer that prints as bare JSON on the command line, so CI
// can feed it straight into a workflow, and as the usual envelope in a
// request file.
type jsonText struct{ Value any }

// MarshalJSON writes just the value.
func (j jsonText) MarshalJSON() ([]byte, error) { return json.Marshal(j.Value) }

func (j jsonText) plainText() string {
	data, err := json.MarshalIndent(j.Value, "", "  ")
	if err != nil {
		return err.Error() + "\n"
	}
	return string(data) + "\n"
}

func runCIMatrix(ctx context.Context, req Request, s *session) (any, error) {
	if req.Maint.Recipes == "" || req.Maint.Base == "" {
		return nil, usageError{"ci matrix needs --recipes and --base"}
	}
	rows, err := indexer.Matrix(ctx, s.deps.Runner, req.Maint.Recipes, req.Maint.Base)
	if err != nil {
		return nil, err
	}
	return jsonText{rows}, nil
}

// ciPlatform is one machine type the publish workflow builds on.
type ciPlatform struct {
	KeyOS     string `json:"key_os"`
	Arch      string `json:"arch"`
	Runner    string `json:"runner"`
	BuildArch string `json:"build_arch"` // value for "scdeken build --arch"; "" = native
}

func runCIPlatforms(context.Context, Request, *session) (any, error) {
	return jsonText{[]ciPlatform{
		{"linux", "x86_64", "ubuntu-22.04", ""},
		{"linux", "arm64", "ubuntu-22.04-arm", ""},
		{"macos", "universal", "macos-latest", "universal"},
		{"windows", "x86_64", "windows-latest", ""},
	}}, nil
}

func runCIPack(_ context.Context, req Request, _ *session) (any, error) {
	if err := archive.Zip(req.Path, req.Out); err != nil {
		return nil, err
	}
	return map[string]string{"zip": req.Out}, nil
}

// buildResult is the answer to "build".
type buildResult struct {
	Out         string   `json:"out"`
	PluginFiles []string `json:"plugin_files"`
	FilesList   string   `json:"files_list"` // the <out>.files.txt file
}

func runBuild(ctx context.Context, req Request, s *session) (any, error) {
	if req.Path == "" || req.Version == "" || req.Out == "" {
		return nil, usageError{"build needs --recipe, --version and --out"}
	}
	r, err := recipe.Load(req.Path)
	if err != nil {
		return nil, err
	}
	commit, ok := r.Versions.Approved[req.Version]
	if !ok {
		return nil, fmt.Errorf("%s has no approved version %s; approved: %s", r.Name, req.Version, strings.Join(r.ApprovedVersions(), ", "))
	}
	if !r.HasPlugin() {
		return nil, fmt.Errorf("%s is a quark; there is nothing to build", r.Name)
	}
	h, err := ensureHeaders(ctx, s, req.SCVersion)
	if err != nil {
		return nil, err
	}
	res, err := buildInto(ctx, s, r, commit, h.Dir, req)
	if err != nil {
		return nil, err
	}
	list := req.Out + ".files.txt"
	if err := os.WriteFile(list, []byte(strings.Join(res.PluginFiles, "\n")+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", list, err)
	}
	return buildResult{Out: req.Out, PluginFiles: res.PluginFiles, FilesList: list}, nil
}

// buildInto compiles the recipe at commit into req.Out, working in a scratch
// folder that is removed afterwards.
func buildInto(ctx context.Context, s *session, r recipe.Recipe, commit, headersDir string, req Request) (build.Result, error) {
	tmp, err := os.MkdirTemp("", "scdeken-build-")
	if err != nil {
		return build.Result{}, fmt.Errorf("build %s: %w", r.Name, err)
	}
	defer os.RemoveAll(tmp)
	arch, err := buildArch(req.Maint.Arch)
	if err != nil {
		return build.Result{}, usageError{err.Error()}
	}
	return build.Run(ctx, s.deps.Runner, build.Options{
		RepoURL: r.Repo, Commit: commit, Build: *r.Build, Requires: r.Requires,
		HeadersDir: headersDir, WorkDir: filepath.Join(tmp, "work"), OutDir: req.Out,
		OS: platform.HostOS(), Arch: arch, VcpkgRoot: req.Maint.VcpkgRoot, Log: s.log,
	})
}

// buildArch reads the --arch flag: empty (this machine), "universal" or a
// processor name.
func buildArch(flagValue string) (string, error) {
	if flagValue == "" || flagValue == "universal" {
		return flagValue, nil
	}
	return platform.NormalizeArch(flagValue)
}
