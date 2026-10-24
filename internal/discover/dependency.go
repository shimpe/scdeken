package discover

import (
	"regexp"
	"strings"

	"github.com/shimpe/scdeken/internal/version"
)

// Dependency is one entry of a .quark file's dependencies list.
type Dependency struct {
	Name    string // quark name, or the last part of the repository address
	URL     string // repository address; "" for a dependency given by name
	Refspec string // what follows "@", such as "tags/1.2"; "" when absent
}

// ParseDependency reads one dependency string: "Name", "Name@refspec", a
// repository address ("https://github.com/owner/Repo", "git://...",
// ".../Repo.git") or an address followed by "@refspec". ok is false for
// anything that is none of these.
func ParseDependency(raw string) (dep Dependency, ok bool) {
	raw = strings.TrimSpace(raw)
	spec, refspec := splitRefspec(raw)
	if strings.Contains(spec, "://") {
		dep = Dependency{Name: repoBase(spec), URL: spec, Refspec: refspec}
	} else {
		dep = Dependency{Name: spec, Refspec: refspec}
	}
	if !plainName.MatchString(dep.Name) {
		return Dependency{}, false
	}
	return dep, true
}

// splitRefspec cuts "@refspec" off the end. In an address only an "@" in the
// path counts, so "https://user@host/repo" keeps its user name.
func splitRefspec(raw string) (spec, refspec string) {
	from := 0
	if i := strings.Index(raw, "://"); i >= 0 {
		from = i + len("://")
		if slash := strings.IndexByte(raw[from:], '/'); slash >= 0 {
			from += slash
		}
	}
	if at := strings.IndexByte(raw[from:], '@'); at >= 0 {
		return raw[:from+at], raw[from+at+1:]
	}
	return raw, ""
}

// repoBase is the last path element of an address without ".git".
func repoBase(address string) string {
	trimmed := strings.TrimSuffix(strings.TrimRight(address, "/"), ".git")
	return trimmed[strings.LastIndex(trimmed, "/")+1:]
}

// String writes the dependency in the form ParseDependency reads, which is
// how it is kept in the scan cache.
func (d Dependency) String() string {
	s := d.Name
	if d.URL != "" {
		s = d.URL
	}
	if d.Refspec != "" {
		s += "@" + d.Refspec
	}
	return s
}

// Key is what a recipe's [depends] table calls this dependency: the
// repository address when there is one (git:// made https://), else the name.
func (d Dependency) Key() string {
	if d.URL != "" {
		if rest, ok := strings.CutPrefix(d.URL, "git://"); ok {
			return "https://" + rest
		}
		return d.URL
	}
	return d.Name
}

var versionRefspec = regexp.MustCompile(`^(?:tags/)?[vV]?(\d+(?:\.\d+){0,3}(?:-[0-9A-Za-z.]+)?)$`)

// Constraint turns the refspec into a version constraint. A refspec that
// names a version ("tags/1.2", "v1.2", "1.2") pins exactly that version; a
// branch or commit cannot be compared with released versions, so it allows
// any version and note says so. note is empty when nothing was lost.
func (d Dependency) Constraint() (constraint, note string) {
	if d.Refspec == "" {
		return "*", ""
	}
	if m := versionRefspec.FindStringSubmatch(d.Refspec); m != nil {
		if _, err := version.Parse(m[1]); err == nil {
			return "=" + m[1], ""
		}
	}
	return "*", "dependency " + d.Key() + ": \"" + d.Refspec + "\" is not a version, so any version is accepted"
}
