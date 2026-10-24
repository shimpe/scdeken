package index

import (
	"sort"
	"strings"

	"github.com/shimpe/scdeken/internal/recipe"
)

// SearchOptions narrows a search.
type SearchOptions struct {
	IncludeUnreviewed bool        // list unreviewed packages too
	Kind              recipe.Kind // when non-empty, only this kind
}

// Match says which part of a package matched the query.
type Match struct {
	Field string `json:"field"` // "name", "summary", "ugen" or "class"
	Value string `json:"value"`
}

// Result is one search hit, shaped for the SuperCollider GUI.
type Result struct {
	Name      string      `json:"name"`
	Summary   string      `json:"summary"`
	Kind      recipe.Kind `json:"kind"`
	Trust     Trust       `json:"trust"`
	Latest    string      `json:"latest"`
	Retracted bool        `json:"retracted"`
	Match     *Match      `json:"match,omitempty"` // nil for an empty query
}

// Fields a term can match, strongest first.
const (
	fieldName    = "name"
	fieldSummary = "summary"
	fieldUGen    = "ugen"
	fieldClass   = "class"
)

var fieldStrength = map[string]int{fieldName: 0, fieldSummary: 1, fieldUGen: 2, fieldClass: 3}

// Search finds packages in which every term of query matches the name,
// summary, a UGen name or a class name, case-insensitively. Results are
// ordered by match strength, trust, stars (most first) and name.
func Search(ix *Index, query string, opts SearchOptions) []Result {
	terms := strings.Fields(strings.ToLower(query))
	var hits []hit
	for i := range ix.Packages {
		p := &ix.Packages[i]
		if !wanted(p, opts) {
			continue
		}
		m, ok := matchAll(p, terms)
		if ok {
			hits = append(hits, hit{p, m})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hitLess(hits[i], hits[j]) })
	return results(ix, hits)
}

// hit is a package that matched, with its strongest match (nil if no terms).
type hit struct {
	pkg   *Package
	match *Match
}

// wanted applies the trust and kind filters.
func wanted(p *Package, opts SearchOptions) bool {
	if p.Trust == Unreviewed && !opts.IncludeUnreviewed {
		return false
	}
	return opts.Kind == "" || p.Kind == opts.Kind
}

// matchAll returns the strongest match among terms, and false unless every
// term matches p.
func matchAll(p *Package, terms []string) (*Match, bool) {
	var best *Match
	for _, term := range terms {
		m, ok := matchTerm(p, term)
		if !ok {
			return nil, false
		}
		if best == nil || fieldStrength[m.Field] < fieldStrength[best.Field] {
			best = m
		}
	}
	return best, true
}

// matchTerm finds the strongest field of p containing term.
func matchTerm(p *Package, term string) (*Match, bool) {
	if contains(p.Name, term) {
		return &Match{fieldName, p.Name}, true
	}
	if contains(p.Summary, term) {
		return &Match{fieldSummary, p.Summary}, true
	}
	if v, ok := firstContaining(p.UGens, term); ok {
		return &Match{fieldUGen, v}, true
	}
	if v, ok := firstContaining(p.Classes, term); ok {
		return &Match{fieldClass, v}, true
	}
	return nil, false
}

func contains(s, term string) bool {
	return strings.Contains(strings.ToLower(s), term)
}

func firstContaining(list []string, term string) (string, bool) {
	for _, s := range list {
		if contains(s, term) {
			return s, true
		}
	}
	return "", false
}

// hitLess orders hits by match strength, trust, stars descending and name.
func hitLess(a, b hit) bool {
	if sa, sb := strength(a), strength(b); sa != sb {
		return sa < sb
	}
	if ta, tb := trustRank(a.pkg.Trust), trustRank(b.pkg.Trust); ta != tb {
		return ta < tb
	}
	if a.match != nil && a.pkg.Stars != b.pkg.Stars { // an empty query ignores stars
		return a.pkg.Stars > b.pkg.Stars
	}
	return strings.ToLower(a.pkg.Name) < strings.ToLower(b.pkg.Name)
}

// strength is the match rank of a hit; hits without a match (empty query)
// all tie.
func strength(h hit) int {
	if h.match == nil {
		return 0
	}
	return fieldStrength[h.match.Field]
}

// trustRank orders trust levels: reviewed first.
func trustRank(t Trust) int {
	switch t {
	case Reviewed:
		return 0
	case Community:
		return 1
	}
	return 2
}

// results turns sorted hits into GUI results.
func results(ix *Index, hits []hit) []Result {
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		out = append(out, toResult(ix, h))
	}
	return out
}

func toResult(ix *Index, h hit) Result {
	p := h.pkg
	res := Result{Name: p.Name, Summary: p.Summary, Kind: p.Kind, Trust: p.Trust, Match: h.match}
	if r, ok := ix.Latest(p); ok {
		res.Latest = r.Version
	}
	if len(p.Versions) > 0 {
		_, res.Retracted = ix.Retraction(p, &p.Versions[0])
	}
	return res
}
