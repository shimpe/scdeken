// Package indexer builds the signed package index from reviewed recipes, the
// official quarks directory and GitHub topics, and finds new upstream tags
// for the recipes repository's bot. It runs in CI, never on a musician's
// machine.
package indexer

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
)

// Scanner looks at upstream repositories. The real one talks to git; tests
// supply a fake.
type Scanner interface {
	// Refs lists the head commit and the tag → commit map of a repository.
	Refs(ctx context.Context, url string) (head string, tags map[string]string, err error)
	// Scan inspects the repository's files at one commit.
	Scan(ctx context.Context, url, commit string) (discover.Scan, error)
}

// Inputs is everything Build needs, already fetched.
type Inputs struct {
	Recipes    []recipe.Recipe
	Advisories []advisory.Advisory
	SCVersions []platform.SCVersion
	Directory  []discover.Entry
	Topics     []github.Repo                  // union of topic searches
	Meta       map[string]github.Repo         // NormalizeRepo(url) → metadata (stars, description, license, pushed)
	Releases   []github.Release               // releases of the recipes repo
	Sums       map[string]map[string]string   // release tag → asset name → sha256 (from SHA256SUMS assets)
	Files      map[string]map[string][]string // release tag → platform key → plugin file names (from <key>.files.txt assets)

	// Previous is the last published index. It is a scan cache and nothing
	// else: Build copies only what a scan of the same commit would give
	// again (classes, UGens, and for non-reviewed packages kind, build
	// description, summary and license). Versions, builds and checksums are
	// never taken from it, so a bad old index cannot leak into a new one.
	Previous *index.Index

	Now time.Time
	Log io.Writer // progress and skipped-package lines; nil discards
}

// candidate is one repository that may become a package.
type candidate struct {
	name  string // directory name; "" when the scan decides
	url   string
	trust index.Trust
	r     *recipe.Recipe
}

// builder holds the state of one Build.
type builder struct {
	s     Scanner
	in    Inputs
	log   io.Writer
	meta  map[string]github.Repo
	names map[string]bool // lowercase package names already taken
}

// Build assembles the index. A repository that cannot be read is logged and
// skipped or kept bare; it never fails the build.
func Build(ctx context.Context, s Scanner, in Inputs) (*index.Index, error) {
	b := newBuilder(s, in)
	ix := &index.Index{
		Schema: index.SchemaVersion, Generated: in.Now.UTC(),
		SCVersions: nonNil(in.SCVersions), Advisories: nonNil(in.Advisories),
		Packages: []index.Package{},
	}
	for _, c := range b.candidates() {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("build index: %w", err)
		}
		pkg, ok := b.assemble(ctx, c)
		if !ok {
			continue
		}
		key := strings.ToLower(pkg.Name)
		if b.names[key] {
			b.logf("skipping %s: the name %q is already used by another package", c.url, pkg.Name)
			continue
		}
		b.names[key] = true
		ix.Packages = append(ix.Packages, pkg)
	}
	return ix, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newBuilder(s Scanner, in Inputs) *builder {
	log := in.Log
	if log == nil {
		log = io.Discard
	}
	b := &builder{s: s, in: in, log: log, meta: map[string]github.Repo{}, names: map[string]bool{}}
	for _, t := range in.Topics {
		b.meta[advisory.NormalizeRepo(t.HTMLURL)] = t
	}
	for url, m := range in.Meta {
		b.meta[url] = m
	}
	return b
}

func (b *builder) logf(format string, args ...any) { fmt.Fprintf(b.log, format+"\n", args...) }

// candidates lists the repositories to index: reviewed first, then community,
// then unreviewed. A repository appears once, under its highest trust, and
// archived repositories only when reviewed.
func (b *builder) candidates() []candidate {
	seen := map[string]bool{}
	var out []candidate
	add := func(c candidate) {
		key := advisory.NormalizeRepo(c.url)
		if seen[key] {
			return
		}
		if c.trust != index.Reviewed && b.meta[key].Archived {
			b.logf("skipping %s: the repository is archived", c.url)
			return
		}
		seen[key] = true
		out = append(out, c)
	}
	for i := range b.in.Recipes {
		r := &b.in.Recipes[i]
		add(candidate{name: r.Name, url: r.Repo, trust: index.Reviewed, r: r})
	}
	for _, e := range b.in.Directory {
		add(candidate{name: e.Name, url: e.URL, trust: index.Community})
	}
	for _, t := range b.in.Topics {
		add(candidate{url: t.HTMLURL, trust: index.Unreviewed})
	}
	return out
}

// assemble builds the package for one candidate; ok is false when it should
// not be listed.
func (b *builder) assemble(ctx context.Context, c candidate) (index.Package, bool) {
	if c.r != nil {
		return b.reviewed(ctx, *c.r), true
	}
	return b.discovered(ctx, c)
}

// applyMeta fills the fields that come from GitHub's metadata.
func (b *builder) applyMeta(pkg *index.Package) {
	m := b.meta[advisory.NormalizeRepo(pkg.Repo)]
	pkg.Stars, pkg.Pushed = m.Stars, m.PushedAt
	if pkg.License == "" {
		pkg.License = m.License
	}
	if pkg.Summary == "" {
		pkg.Summary = m.Description
	}
}
