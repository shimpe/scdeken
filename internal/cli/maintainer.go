package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sc-deken/sc-deken/internal/build"
	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/headers"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/version"
)

// defaultSCVersion is the SuperCollider release "recipe check --build" builds against.
const defaultSCVersion = "3.14.1"

// The maintainer commands live in their own file and join the command table here.
func init() {
	commands["recipe-new"] = command{
		usage: "recipe new <github-url> [--out <file>]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Out, "out", "", "write the recipe to this file instead of printing it")
		},
		args: func(pos []string, req *Request) error { return oneWord(pos, &req.URL, "a GitHub repository address") },
		run:  runRecipeNew,
	}
	commands["recipe-check"] = command{
		usage: "recipe check <file> [--build] [--sc-version " + defaultSCVersion + "]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.BoolVar(&req.Build, "build", false, "also build the newest approved version here")
			fs.StringVar(&req.SCVersion, "sc-version", "", "SuperCollider version to build against")
		},
		args: func(pos []string, req *Request) error { return oneWord(pos, &req.Path, "a recipe file") },
		run:  runRecipeCheck,
	}
	commands["recipe-approve"] = command{
		usage: "recipe approve <file> <version> <commit>",
		args:  recipeApproveArgs,
		run:   runRecipeApprove,
	}
}

func recipeApproveArgs(pos []string, req *Request) error {
	if len(pos) != 3 {
		return usageError{"expected a recipe file, a version and a commit"}
	}
	req.Path, req.Version, req.Commit = pos[0], pos[1], pos[2]
	return nil
}

// draftResult is the answer to "recipe new". On the command line it prints as
// plain text, so the TOML can be redirected into a file.
type draftResult struct {
	TOML  string   `json:"toml"`
	Notes []string `json:"notes,omitempty"`
	Path  string   `json:"path,omitempty"` // where the recipe was written, if --out was given
}

// plainText is what the command line prints instead of JSON.
func (d draftResult) plainText() string {
	if d.Path == "" {
		return d.TOML + noteLines(d.Notes)
	}
	return "Wrote " + d.Path + "\n" + noteLines(d.Notes)
}

func noteLines(notes []string) string {
	var b strings.Builder
	for _, n := range notes {
		b.WriteString("# NOTE: " + n + "\n")
	}
	return b.String()
}

func runRecipeNew(ctx context.Context, req Request, s *session) (any, error) {
	owner, repo, err := discover.ParseGitHubURL(req.URL)
	if err != nil {
		return nil, usageError{err.Error()}
	}
	url := "https://github.com/" + owner + "/" + repo
	client := &github.Client{Token: os.Getenv("GITHUB_TOKEN"), BaseURL: s.deps.GitHubURL}
	meta, err := client.Repo(ctx, owner+"/"+repo)
	if err != nil {
		return nil, err
	}
	head, tags, err := discover.RemoteRefs(ctx, s.deps.Runner, url)
	if err != nil {
		return nil, err
	}
	scan, err := scanRelease(ctx, s, url, discover.PickRelease(head, tags).Commit)
	if err != nil {
		return nil, err
	}
	draft, notes, err := discover.DraftRecipe(url, meta, scan, head, tags)
	if err != nil {
		return nil, err
	}
	if scan.QuarkError != "" {
		notes = append(notes, "the .quark file could not be read: "+scan.QuarkError)
	}
	return writeDraft(draft, notes, req.Out)
}

// scanRelease inspects the repository at commit in a scratch folder.
func scanRelease(ctx context.Context, s *session, url, commit string) (discover.Scan, error) {
	tmp, err := os.MkdirTemp("", "scdeken-scan-")
	if err != nil {
		return discover.Scan{}, fmt.Errorf("scan %s: %w", url, err)
	}
	defer os.RemoveAll(tmp)
	return discover.ScanRepo(ctx, s.deps.Runner, url, commit, filepath.Join(tmp, "src"))
}

func writeDraft(draft recipe.Recipe, notes []string, out string) (any, error) {
	data, err := recipe.Marshal(draft)
	if err != nil {
		return nil, err
	}
	result := draftResult{TOML: string(data), Notes: notes}
	if out == "" {
		return result, nil
	}
	if err := os.WriteFile(out, []byte(result.TOML+noteLines(notes)), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", out, err)
	}
	result.Path = out
	return result, nil
}

// checkResult is the answer to "recipe check".
type checkResult struct {
	OK          bool     `json:"ok"`
	PluginFiles []string `json:"plugin_files,omitempty"`
}

func runRecipeCheck(ctx context.Context, req Request, s *session) (any, error) {
	r, err := recipe.Load(req.Path)
	if err != nil {
		return nil, err
	}
	if !req.Build {
		return checkResult{OK: true}, nil
	}
	files, err := buildNewest(ctx, r, req.SCVersion, s)
	if err != nil {
		return nil, err
	}
	return checkResult{OK: true, PluginFiles: files}, nil
}

// buildNewest builds the newest approved version into a scratch folder and
// returns the plugin files it produced.
func buildNewest(ctx context.Context, r recipe.Recipe, scVersion string, s *session) ([]string, error) {
	if !r.HasPlugin() {
		return nil, fmt.Errorf("%s is a quark; there is nothing to build", r.Name)
	}
	h, err := ensureHeaders(ctx, s, scVersion)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "scdeken-check-")
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", r.Name, err)
	}
	defer os.RemoveAll(tmp)
	newest := r.ApprovedVersions()[0]
	res, err := build.Run(ctx, s.deps.Runner, build.Options{
		RepoURL: r.Repo, Commit: r.Versions.Approved[newest], Build: *r.Build, Requires: r.Requires,
		HeadersDir: h.Dir, WorkDir: filepath.Join(tmp, "work"), OutDir: filepath.Join(tmp, "out"),
		OS: platform.HostOS(), Log: s.log,
	})
	if err != nil {
		return nil, err
	}
	return res.PluginFiles, nil
}

func runRecipeApprove(_ context.Context, req Request, _ *session) (any, error) {
	if req.Path == "" || req.Version == "" || req.Commit == "" {
		return nil, errors.New("recipe approve needs a recipe file, a version and a commit")
	}
	if err := recipe.AppendApproved(req.Path, req.Version, req.Commit); err != nil {
		return nil, err
	}
	return map[string]string{"approved": req.Version}, nil
}

// ensureHeaders makes sure the SuperCollider headers for scVersion (default
// defaultSCVersion) are in the shared cache folder.
func ensureHeaders(ctx context.Context, s *session, scVersion string) (headers.Headers, error) {
	if scVersion == "" {
		scVersion = defaultSCVersion
	}
	sc, err := version.Parse(scVersion)
	if err != nil {
		return headers.Headers{}, usageError{fmt.Sprintf("%q is not a SuperCollider version like %s", scVersion, defaultSCVersion)}
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return headers.Headers{}, fmt.Errorf("find the cache folder: %w", err)
	}
	return headers.Ensure(ctx, s.deps.Runner, filepath.Join(cache, "sc-deken", "headers"), sc, s.log)
}
