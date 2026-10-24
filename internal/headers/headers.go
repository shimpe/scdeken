// Package headers fetches and caches the SuperCollider source tree that
// matches the installed SuperCollider, so plugins can be built against it.
package headers

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shimpe/scdeken/internal/config"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/version"
)

const (
	// marker is written into a cached checkout once it is fully usable.
	marker     = ".sc-deken-complete"
	tagPrefix  = "Version-"
	headerPath = "include/plugin_interface/SC_InterfaceTable.h"
)

// submodules are the external libraries a plugin build needs.
var submodules = []string{"nova-tt", "nova-simd"}

var apiPattern = regexp.MustCompile(`sc_api_version\s*=\s*(\d+)`)

// Headers describes a cached SuperCollider source checkout.
type Headers struct {
	Dir   string // the checkout
	Tag   string // the release tag it was cloned from
	API   int    // the plugin API version it declares
	Exact bool   // false when a nearby older release stands in for the installed version
}

// Ensure makes sure the source for sc (or the nearest older release) is in
// cacheDir and returns where it is. log may be nil. git never asks for a
// password.
func Ensure(ctx context.Context, r run.Runner, cacheDir string, sc version.Version, log io.Writer) (Headers, error) {
	if log == nil {
		log = io.Discard
	}
	r = run.NoPrompt{Runner: r}
	tags, err := listTags(ctx, r)
	if err != nil {
		return Headers{}, err
	}
	tag, exact, err := PickTag(tags, sc)
	if err != nil {
		return Headers{}, err
	}
	if !exact {
		fmt.Fprintf(log, "No SuperCollider release tag for %s; using %s headers instead.\n", sc, tag)
	}
	dir := filepath.Join(cacheDir, tag)
	if err := ensureCheckout(ctx, r, dir, tag, log); err != nil {
		return Headers{}, err
	}
	api, err := ReadAPI(dir)
	if err != nil {
		return Headers{}, err
	}
	return Headers{Dir: dir, Tag: tag, API: api, Exact: exact}, nil
}

// ensureCheckout leaves a complete, marked checkout of tag in dir.
func ensureCheckout(ctx context.Context, r run.Runner, dir, tag string, log io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("clear incomplete SuperCollider source %s: %w", dir, err)
	}
	clone := run.Cmd{
		Name: "git",
		Args: []string{"clone", "--depth", "1", "--branch", tag, "--recurse-submodules", "--shallow-submodules", config.SCRepoURL, dir},
		Log:  log,
	}
	if err := r.Run(ctx, clone); err != nil {
		return fmt.Errorf("download SuperCollider source %s: %w", tag, err)
	}
	if !hasSubmodules(dir) {
		return fmt.Errorf("SuperCollider source for %s is missing its submodules (nova-tt / nova-simd); delete %s and try again", tag, dir)
	}
	if err := os.WriteFile(filepath.Join(dir, marker), nil, 0o644); err != nil {
		return fmt.Errorf("mark SuperCollider source complete: %w", err)
	}
	return nil
}

// hasSubmodules reports whether every required external library is non-empty.
func hasSubmodules(dir string) bool {
	for _, name := range submodules {
		entries, err := os.ReadDir(filepath.Join(dir, "external_libraries", name))
		if err != nil || len(entries) == 0 {
			return false
		}
	}
	return true
}

// listTags asks the SuperCollider repository for its tag names.
func listTags(ctx context.Context, r run.Runner) ([]string, error) {
	out, err := r.Output(ctx, run.Cmd{Name: "git", Args: []string{"ls-remote", "--tags", "--refs", config.SCRepoURL}})
	if err != nil {
		return nil, fmt.Errorf("list SuperCollider releases: %w", err)
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		if _, ref, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			tags = append(tags, strings.TrimPrefix(ref, "refs/tags/"))
		}
	}
	return tags, nil
}

// PickTag chooses the release tag (such as "Version-3.13.0") for want. A tag
// with the same version is exact, prerelease or not. Otherwise the greatest
// final release not newer than want's core version stands in.
func PickTag(tags []string, want version.Version) (tag string, exact bool, err error) {
	wantCore := coreOf(want)
	var best version.Version
	for _, t := range tags {
		v, ok := version.FromTag(t, tagPrefix)
		if !ok {
			continue
		}
		if v.Compare(want) == 0 {
			return t, true, nil
		}
		if v.IsPrerelease() || v.Compare(wantCore) > 0 {
			continue
		}
		if tag == "" || v.Compare(best) > 0 {
			tag, best = t, v
		}
	}
	if tag == "" {
		return "", false, fmt.Errorf("SuperCollider %s is older than any supported release", want)
	}
	return tag, false, nil
}

// coreOf drops any prerelease and build suffix from v.
func coreOf(v version.Version) version.Version {
	text, _, _ := strings.Cut(v.String(), "+")
	text, _, _ = strings.Cut(text, "-")
	core, _ := version.Parse(text)
	return core
}

// ReadAPI reads the plugin API version declared by the source tree in dir.
func ReadAPI(dir string) (int, error) {
	path := filepath.Join(dir, filepath.FromSlash(headerPath))
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read plugin API version: %w", err)
	}
	m := apiPattern.FindSubmatch(data)
	if m == nil {
		return 0, fmt.Errorf("no sc_api_version found in %s", path)
	}
	return strconv.Atoi(string(m[1]))
}
