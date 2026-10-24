package indexer

import (
	"strings"

	"github.com/shimpe/scdeken/internal/advisory"
	"github.com/shimpe/scdeken/internal/discover"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/recipe"
)

// dependsFromQuark turns the raw dependency strings of a .quark file into
// [depends]-style entries: name or https address → constraint. Strings that
// are not dependencies, and addresses that are not git:// or https://, are
// left out.
func dependsFromQuark(raw []string) map[string]string {
	var depends map[string]string
	for _, s := range raw {
		dep, ok := discover.ParseDependency(s)
		if !ok || (dep.URL != "" && !recipe.IsDependencyURL(dep.Key())) {
			continue
		}
		constraint, _ := dep.Constraint()
		if depends == nil {
			depends = map[string]string{}
		}
		depends[dep.Key()] = constraint
	}
	return depends
}

// rawDependencies writes the dependencies of a scan the way they are kept in
// the scan cache.
func rawDependencies(deps []discover.Dependency) []string {
	var raw []string
	for _, d := range deps {
		raw = append(raw, d.String())
	}
	return raw
}

// resolveDepends is the second pass over the finished package list. Name
// keys are rewritten to the spelling of the package they match; address
// keys are replaced by the name of the package with that repository. A name
// nobody has stays as it is (planning reports it); an address nobody has
// moves to DependsUnresolved.
func resolveDepends(pkgs []index.Package) {
	byName, byRepo := map[string]string{}, map[string]string{}
	for _, p := range pkgs {
		byName[strings.ToLower(p.Name)] = p.Name
		byRepo[advisory.NormalizeRepo(p.Repo)] = p.Name
	}
	for i := range pkgs {
		p := &pkgs[i]
		declared := p.Depends
		p.Depends = nil
		for key, constraint := range declared {
			switch {
			case !recipe.IsDependencyURL(key):
				addDepend(p, canonical(byName, key), constraint)
			case byRepo[advisory.NormalizeRepo(key)] != "":
				addDepend(p, byRepo[advisory.NormalizeRepo(key)], constraint)
			default:
				addUnresolved(p, key, constraint)
			}
		}
	}
}

// canonical returns the package spelling of name, or name itself.
func canonical(byName map[string]string, name string) string {
	if spelled, ok := byName[strings.ToLower(name)]; ok {
		return spelled
	}
	return name
}

// addDepend records a constraint on dependency name; when the package also
// reached it by another key, both constraints must hold.
func addDepend(p *index.Package, name, constraint string) {
	if p.Depends == nil {
		p.Depends = map[string]string{}
	}
	p.Depends[name] = joinConstraints(p.Depends[name], constraint)
}

func addUnresolved(p *index.Package, url, constraint string) {
	if p.DependsUnresolved == nil {
		p.DependsUnresolved = map[string]string{}
	}
	p.DependsUnresolved[url] = joinConstraints(p.DependsUnresolved[url], constraint)
}

// joinConstraints combines two constraints on one dependency; "" and "*"
// add nothing.
func joinConstraints(a, b string) string {
	switch {
	case a == "" || a == "*":
		return b
	case b == "" || b == "*" || a == b:
		return a
	}
	return a + "," + b
}
