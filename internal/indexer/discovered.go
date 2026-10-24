package indexer

import (
	"context"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/version"
)

// discovered builds a community or unreviewed package from upstream tags and
// a scan of its newest commit. It carries no archives and no checksums.
func (b *builder) discovered(ctx context.Context, c candidate) (index.Package, bool) {
	head, tags, err := b.s.Refs(ctx, c.url)
	if err != nil {
		b.logf("skipping %s: %v", c.url, err)
		return index.Package{}, false
	}
	versions := discoveredVersions(head, tags)
	if len(versions) == 0 {
		b.logf("skipping %s: the repository has no commits", c.url)
		return index.Package{}, false
	}
	info, scanned := b.inspect(ctx, c.url, head, false)
	kind, ok := b.discoveredKind(c, info)
	if !ok {
		return index.Package{}, false
	}
	pkg := index.Package{
		Name: firstNonEmpty(c.name, info.QuarkName, repoName(c.url)), Summary: info.QuarkSummary,
		Repo: c.url, Kind: kind, Trust: c.trust,
		QuarkName: info.QuarkName, QuarkSummary: info.QuarkSummary, LicenseFile: info.LicenseFile,
		Classes: info.Classes, UGens: info.UGens, Build: info.Build, Versions: versions,
	}
	if scanned {
		pkg.ScannedCommit = head
	}
	b.applyMeta(&pkg)
	return pkg, true
}

// discoveredKind decides the kind of a found package: what the scan saw, else
// for quarks from the directory "quark", else what the repository's GitHub
// topics say. A package whose kind comes from topics has no build
// description, so it is listed for information only.
func (b *builder) discoveredKind(c candidate, info scanInfo) (recipe.Kind, bool) {
	if info.Kind != "" {
		return info.Kind, true
	}
	if c.trust == index.Community {
		return recipe.KindQuark, true
	}
	if kind := kindFromTopics(b.meta[advisory.NormalizeRepo(c.url)].Topics); kind != "" {
		return kind, true
	}
	b.logf("skipping %s: it does not look like a SuperCollider plugin or quark", c.url)
	return "", false
}

// kindFromTopics maps the topics searched by the indexer to a kind: the quark
// topic means quark, the plugin topics mean plugin, both mean plugin+quark.
func kindFromTopics(topics []string) recipe.Kind {
	var quark, plugin bool
	for _, t := range topics {
		switch t {
		case QuarkTopic:
			quark = true
		case "supercollider-plugin", "supercollider-plugins", "supercollider-ugens":
			plugin = true
		}
	}
	switch {
	case quark && plugin:
		return recipe.KindPluginQuark
	case quark:
		return recipe.KindQuark
	case plugin:
		return recipe.KindPlugin
	}
	return ""
}

// discoveredVersions turns upstream tags into releases, newest first. A
// repository without usable tags gets one unstable release of its head.
func discoveredVersions(head string, tags map[string]string) []index.Release {
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	sort.Strings(names) // a fixed order decides between "1.0" and "v1.0"
	type tagged struct {
		v      version.Version
		commit string
	}
	var found []tagged
	seen := map[string]bool{}
	for _, tag := range names {
		v, ok := version.FromTag(tag, "")
		if ok && !seen[v.String()] {
			seen[v.String()] = true
			found = append(found, tagged{v, tags[tag]})
		}
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].v.Compare(found[j].v) > 0 })
	var out []index.Release
	for _, f := range found {
		out = append(out, index.Release{Version: f.v.String(), Commit: f.commit, Unstable: f.v.IsUnstable()})
	}
	if len(out) == 0 && head != "" {
		u := version.Unstable(head)
		out = append(out, index.Release{Version: u.String(), Commit: head, Unstable: true})
	}
	return out
}

// scanInfo is what the index keeps from looking inside a repository.
type scanInfo struct {
	QuarkName      string // name in the .quark file
	QuarkSummary   string // summary in the .quark file
	LicenseFile    string // SPDX guess from the license file
	Kind           recipe.Kind
	Build          *recipe.Build
	Classes, UGens []string
}

// inspect returns what a scan of url at commit finds: from the previous
// index when it scanned this very commit, else by scanning. ok is false when
// there was nothing to scan or the scan failed; the package is then kept
// without scan results.
//
// The previous index is only a cache of scan results: classes, UGens, kind,
// build description and the quark-file and license-file findings. Names,
// summaries and licenses are worked out again every run from those and the
// current GitHub data. A reviewed package's kind and build come from its
// recipe, so for those (reviewed=true) callers use only classes and UGens;
// and a cached reviewed entry never stands in for a non-reviewed one.
func (b *builder) inspect(ctx context.Context, url, commit string, reviewed bool) (scanInfo, bool) {
	if commit == "" {
		return scanInfo{}, false
	}
	if info, ok := b.cached(url, commit, reviewed); ok {
		return info, true
	}
	scan, err := b.s.Scan(ctx, url, commit)
	if err != nil {
		b.logf("scan of %s failed, keeping it without classes: %v", url, err)
		return scanInfo{}, false
	}
	return infoFromScan(scan), true
}

func (b *builder) cached(url, commit string, reviewed bool) (scanInfo, bool) {
	if b.in.Previous == nil {
		return scanInfo{}, false
	}
	prev, ok := b.in.Previous.FindRepo(url)
	if !ok || prev.ScannedCommit != commit || (!reviewed && prev.Trust == index.Reviewed) {
		return scanInfo{}, false
	}
	return scanInfo{
		QuarkName: prev.QuarkName, QuarkSummary: prev.QuarkSummary, LicenseFile: prev.LicenseFile,
		Kind: prev.Kind, Build: prev.Build, Classes: prev.Classes, UGens: prev.UGens,
	}, true
}

func infoFromScan(s discover.Scan) scanInfo {
	info := scanInfo{
		QuarkName: s.Quark.Name, QuarkSummary: s.Quark.Summary, LicenseFile: s.LicenseFile,
		Kind: s.Kind, Classes: s.Classes, UGens: s.UGens,
	}
	if s.CMakeDir != "" {
		info.Build = discover.DraftBuild(s)
	}
	return info
}

// repoName is the last path segment of a repository address.
func repoName(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	return trimmed[strings.LastIndex(trimmed, "/")+1:]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
