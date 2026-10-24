package install

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/version"
)

// headVersion names the newest commit of a repository that has no releases.
const headVersion = "0.0.0+head"

// depNode is one package (or one repository address nobody lists) in the
// dependency graph.
type depNode struct {
	pkg   *index.Package // nil for an address that matches no package
	url   string         // the address, when pkg is nil
	edges []depEdge
}

// depEdge says that a package needs another one, within a version range.
type depEdge struct {
	to      *depNode
	text    string // the constraint as written
	con     version.Constraint
	missing string // set when the name is not in the package list
}

// need is one dependent's requirement on a node.
type need struct {
	by   string
	text string
	con  version.Constraint
}

// planDependencies plans everything pkg needs, in install order. A needed
// package is left out when it is installed in a version that fits.
//
// It first maps the whole dependency graph, then decides which nodes have to
// be installed. Installing a node adds its own requirements, which can make
// an installed package too old, so the decision repeats until it settles.
func (s *Service) planDependencies(ctx context.Context, pkg *index.Package) ([]Plan, error) {
	g := &depGraph{s: s, nodes: map[string]*depNode{}}
	root, err := g.build(pkg, []string{pkg.Name})
	if err != nil {
		return nil, err
	}
	live := map[*depNode]bool{root: true}
	for {
		needs, err := g.needs(live)
		if err != nil {
			return nil, err
		}
		grown := false
		for node, ns := range needs {
			if !live[node] && s.mustInstall(node, ns) {
				live[node], grown = true, true
			}
		}
		if !grown {
			return g.plans(ctx, root, live, needs)
		}
	}
}

// depGraph holds the nodes reached from the requested package.
type depGraph struct {
	s     *Service
	nodes map[string]*depNode // lowercase package name, or normalized address
}

// build creates the node for pkg and, once, everything below it. path holds
// the names leading here, for cycle errors.
func (g *depGraph) build(pkg *index.Package, path []string) (*depNode, error) {
	key := strings.ToLower(pkg.Name)
	if n, ok := g.nodes[key]; ok {
		return n, nil
	}
	n := &depNode{pkg: pkg}
	g.nodes[key] = n
	for _, name := range sortedKeys(pkg.Depends) {
		edge, err := g.edgeToName(pkg, name, path)
		if err != nil {
			return nil, err
		}
		n.edges = append(n.edges, edge)
	}
	for _, url := range sortedKeys(pkg.DependsUnresolved) {
		n.edges = append(n.edges, g.edgeToURL(pkg.DependsUnresolved[url], url))
	}
	return n, nil
}

func (g *depGraph) edgeToName(from *index.Package, name string, path []string) (depEdge, error) {
	text := from.Depends[name]
	con, err := version.ParseConstraint(text)
	if err != nil {
		return depEdge{}, fmt.Errorf("%s needs %s, but its version range is not understood: %w", from.Name, name, err)
	}
	dep, ok := g.s.Index.Find(name)
	if !ok {
		return depEdge{to: &depNode{}, text: text, con: con,
			missing: fmt.Sprintf("%s needs %q, which is not in the package list", from.Name, name)}, nil
	}
	if cycle := cycleAt(path, dep.Name); cycle != "" {
		return depEdge{}, fmt.Errorf("dependency cycle: %s", cycle)
	}
	child, err := g.build(dep, append(path[:len(path):len(path)], dep.Name))
	if err != nil {
		return depEdge{}, err
	}
	return depEdge{to: child, text: text, con: con}, nil
}

// edgeToURL links to a repository that is not in the package list. Its
// version range is ignored: with no releases listed there is nothing to compare.
func (g *depGraph) edgeToURL(text, url string) depEdge {
	key := advisory.NormalizeRepo(url)
	n, ok := g.nodes[key]
	if !ok {
		n = &depNode{url: url}
		g.nodes[key] = n
	}
	return depEdge{to: n, text: text}
}

// needs collects the requirements that the live nodes put on others, each
// list sorted by dependent so errors read the same every time.
func (g *depGraph) needs(live map[*depNode]bool) (map[*depNode][]need, error) {
	out := map[*depNode][]need{}
	for from := range live {
		for _, e := range from.edges {
			if e.missing != "" {
				return nil, fmt.Errorf("%s", e.missing)
			}
			out[e.to] = append(out[e.to], need{by: from.pkg.Name, text: e.text, con: e.con})
		}
	}
	for _, ns := range out {
		sort.Slice(ns, func(i, j int) bool {
			if ns[i].by != ns[j].by {
				return ns[i].by < ns[j].by
			}
			return ns[i].text < ns[j].text
		})
	}
	return out, nil
}

// mustInstall reports whether node has to be planned: it is not on the
// machine, or it is there in a version that breaks a requirement.
func (s *Service) mustInstall(n *depNode, ns []need) bool {
	if n.pkg == nil {
		return !s.repoInstalled(n.url)
	}
	if !s.installed(n.pkg) {
		return true
	}
	v, known := s.installedVersion(n.pkg)
	if !known {
		return false // cannot tell, so leave the user's copy alone
	}
	for _, need := range ns {
		if !need.con.Allows(v) {
			return true
		}
	}
	return false
}

// installedVersion is the version of pkg on this machine, taken from the
// install record or, for a quark SuperCollider installed, from its commit.
func (s *Service) installedVersion(pkg *index.Package) (version.Version, bool) {
	if s.State != nil {
		if e, ok := s.State.Packages[strings.ToLower(pkg.Name)]; ok && e.Version != "" {
			v, err := version.Parse(e.Version)
			return v, err == nil
		}
	}
	repo := advisory.NormalizeRepo(pkg.Repo)
	for _, q := range s.Ctx.Quarks {
		if advisory.NormalizeRepo(q.URL) != repo {
			continue
		}
		for _, rel := range pkg.Versions {
			if rel.Commit == q.Commit {
				v, err := version.Parse(rel.Version)
				return v, err == nil
			}
		}
	}
	return version.Version{}, false
}

// plans chooses a release for every live dependency and returns their plans,
// dependencies before the packages that need them.
func (g *depGraph) plans(ctx context.Context, root *depNode, live map[*depNode]bool, needs map[*depNode][]need) ([]Plan, error) {
	var out []Plan
	done := map[*depNode]bool{root: true}
	var visit func(n *depNode) error
	visit = func(n *depNode) error {
		for _, e := range n.edges {
			child := e.to
			if done[child] || !live[child] {
				continue
			}
			done[child] = true
			if err := visit(child); err != nil {
				return err
			}
			p, err := g.planNode(ctx, child, needs[child])
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	}
	return out, visit(root)
}

func (g *depGraph) planNode(ctx context.Context, n *depNode, ns []need) (Plan, error) {
	if n.pkg == nil {
		return g.s.headPlan(n.url)
	}
	rel, err := g.s.chooseRelease(n.pkg, ns)
	if err != nil {
		return Plan{}, err
	}
	p, err := g.s.planOne(ctx, n.pkg, rel)
	if err != nil {
		return Plan{}, err
	}
	p.IsUpdate = g.s.installed(n.pkg)
	return p, nil
}

// headPlan installs the newest commit of a quark that the package list does
// not know. It is unreviewed, so the user is asked before it is installed;
// advisories that name its repository still apply.
func (s *Service) headPlan(url string) (Plan, error) {
	p := Plan{
		Package: repoBaseName(url), Version: headVersion, Repo: url,
		Kind: recipe.KindQuark, Trust: index.Unreviewed, Route: RouteQuark,
		Quark:        &QuarkRef{URL: url},
		Confirm:      []string{ConfirmUnreviewed},
		AfterInstall: afterInstall(recipe.KindQuark),
	}
	stub := &index.Package{Name: p.Package, Repo: url, Kind: p.Kind, Trust: p.Trust}
	if err := s.applyRetraction(&p, stub, &index.Release{Version: headVersion, Unstable: true}); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// repoBaseName is the last part of a repository address, without ".git".
func repoBaseName(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	return trimmed[strings.LastIndex(trimmed, "/")+1:]
}

// repoInstalled reports whether a quark from this repository is installed.
func (s *Service) repoInstalled(url string) bool {
	repo := advisory.NormalizeRepo(url)
	for _, q := range s.Ctx.Quarks {
		if advisory.NormalizeRepo(q.URL) == repo {
			return true
		}
	}
	return false
}

// chooseRelease picks the newest release that is not withdrawn as malware
// and that every requirement allows, preferring stable releases.
func (s *Service) chooseRelease(pkg *index.Package, ns []need) (*index.Release, error) {
	if len(pkg.Versions) == 0 {
		return nil, fmt.Errorf("%s has no versions to install", pkg.Name)
	}
	candidates := s.usableReleases(pkg)
	for _, wantUnstable := range []bool{false, true} {
		for _, rel := range candidates {
			if rel.Unstable == wantUnstable && allAllow(ns, rel) {
				return rel, nil
			}
		}
	}
	return nil, s.noReleaseError(pkg, ns, candidates)
}

// usableReleases lists the releases of pkg, newest first, without those
// withdrawn as malware.
func (s *Service) usableReleases(pkg *index.Package) []*index.Release {
	var out []*index.Release
	for i := range pkg.Versions {
		rel := &pkg.Versions[i]
		if a, hit := s.Index.Retraction(pkg, rel); hit && a.Severity == advisory.Malware {
			continue
		}
		out = append(out, rel)
	}
	return out
}

func allAllow(ns []need, rel *index.Release) bool {
	v, err := version.Parse(rel.Version)
	if err != nil {
		return false
	}
	for _, n := range ns {
		if !n.con.Allows(v) {
			return false
		}
	}
	return true
}

// noReleaseError explains why no release fits: either one requirement has no
// release at all, or two requirements cannot be met by the same release.
func (s *Service) noReleaseError(pkg *index.Package, ns []need, rels []*index.Release) error {
	var constrained []need
	for _, n := range ns {
		if n.text != "" && n.text != "*" {
			constrained = append(constrained, n)
		}
	}
	for _, n := range constrained {
		if !anyAllow(rels, n) {
			return fmt.Errorf("%s needs %s %s, but the package list has no such version (latest is %s)",
				n.by, pkg.Name, n.text, s.latestText(pkg))
		}
	}
	if len(constrained) < 2 {
		return fmt.Errorf("%s has no version that can be installed", pkg.Name)
	}
	a, b := conflictingPair(constrained, rels)
	return fmt.Errorf("%s: %s needs %s but %s needs %s; no version satisfies both", pkg.Name, a.by, a.text, b.by, b.text)
}

func (s *Service) latestText(pkg *index.Package) string {
	if rel, ok := s.Index.Latest(pkg); ok {
		return rel.Version
	}
	return "none"
}

func anyAllow(rels []*index.Release, ns ...need) bool {
	for _, rel := range rels {
		if allAllow(ns, rel) {
			return true
		}
	}
	return false
}

// conflictingPair finds two requirements no release can meet together; when
// the clash only shows up among three or more, the first and last are named.
func conflictingPair(ns []need, rels []*index.Release) (need, need) {
	for i := range ns {
		for j := i + 1; j < len(ns); j++ {
			if ns[i].text != ns[j].text && !anyAllow(rels, ns[i], ns[j]) {
				return ns[i], ns[j]
			}
		}
	}
	return ns[0], ns[len(ns)-1]
}

// cycleAt returns "A → B → A" when name is already on path, else "".
func cycleAt(path []string, name string) string {
	for i, n := range path {
		if strings.EqualFold(n, name) {
			return strings.Join(append(path[i:len(path):len(path)], name), " → ")
		}
	}
	return ""
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
