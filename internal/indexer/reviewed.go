package indexer

import (
	"context"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/version"
)

// reviewed builds the package for a recipe. Its versions are exactly the
// approved ones; upstream tags nobody approved yet are not listed.
func (b *builder) reviewed(ctx context.Context, r recipe.Recipe) index.Package {
	approved := r.ApprovedVersions()
	pkg := index.Package{
		Name: r.Name, Summary: r.Summary, Repo: r.Repo, License: r.License,
		Authors: r.Authors, Kind: r.Kind, Trust: index.Reviewed,
		Depends: r.Depends, Build: r.Build, Requires: r.Requires,
	}
	for _, v := range approved {
		pkg.Versions = append(pkg.Versions, b.release(r, v))
	}
	newest := approved[0]
	commit := r.Versions.Approved[newest]
	if info, ok := b.inspect(ctx, r.Repo, commit, true); ok {
		pkg.ScannedCommit, pkg.Classes, pkg.UGens = commit, info.Classes, info.UGens
	}
	if r.HasPlugin() {
		pkg.PluginFiles = b.pluginFiles(releaseTag(r, newest))
	}
	b.applyMeta(&pkg)
	return pkg
}

// releaseTag is the tag of the recipes repository's release for a version.
func releaseTag(r recipe.Recipe, v string) string { return r.Name + "-" + v }

// release builds one approved version with its prebuilt archives.
func (b *builder) release(r recipe.Recipe, v string) index.Release {
	commit := r.Versions.Approved[v]
	rel := index.Release{Version: v, Commit: commit}
	if parsed, err := version.Parse(v); err == nil {
		rel.Unstable = parsed.IsUnstable()
	}
	if !r.HasPlugin() || b.isMalware(r, v, commit) {
		return rel
	}
	rel.Builds = b.builds(releaseTag(r, v))
	return rel
}

// isMalware reports whether a malware advisory withdraws this version; such a
// version is listed but has no downloads.
func (b *builder) isMalware(r recipe.Recipe, v, commit string) bool {
	for _, a := range b.in.Advisories {
		if a.Severity == advisory.Malware && a.Affects(r.Name, r.Repo, v, commit) {
			return true
		}
	}
	return false
}

// builds lists the "<platform-key>.zip" assets of the release with this tag.
// An asset without a checksum is skipped: it cannot be verified.
func (b *builder) builds(tag string) []index.Build {
	var out []index.Build
	for _, rel := range b.in.Releases {
		if rel.Tag != tag || rel.Draft {
			continue
		}
		for _, a := range rel.Assets {
			if bd, ok := b.buildFromAsset(tag, a); ok {
				out = append(out, bd)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Platform < out[j].Platform })
	return out
}

func (b *builder) buildFromAsset(tag string, a github.Asset) (index.Build, bool) {
	key, isZip := strings.CutSuffix(a.Name, ".zip")
	if !isZip {
		return index.Build{}, false
	}
	if _, err := platform.ParseKey(key); err != nil {
		return index.Build{}, false
	}
	sum := b.in.Sums[tag][a.Name]
	if sum == "" {
		b.logf("skipping %s in release %s: no checksum listed in SHA256SUMS", a.Name, tag)
		return index.Build{}, false
	}
	return index.Build{Platform: key, URL: a.URL, SHA256: sum, Size: a.Size}, true
}

// pluginFiles is the union, over platforms, of the plugin files the publish
// workflow listed for this release.
func (b *builder) pluginFiles(tag string) []string {
	seen := map[string]bool{}
	var out []string
	for _, names := range b.in.Files[tag] {
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}
