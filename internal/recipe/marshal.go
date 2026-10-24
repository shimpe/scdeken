package recipe

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Marshal writes a recipe in the canonical layout, with the
// [versions.approved] table last. It does not validate: a drafted recipe
// with TODO fields is written as it is, and Parse or Load check it later.
func Marshal(r Recipe) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "name = %s\n", strconv.Quote(r.Name))
	fmt.Fprintf(&b, "summary = %s\n", strconv.Quote(r.Summary))
	fmt.Fprintf(&b, "repo = %s\n", strconv.Quote(r.Repo))
	if r.License != "" {
		fmt.Fprintf(&b, "license = %s\n", strconv.Quote(r.License))
	}
	if len(r.Authors) > 0 {
		fmt.Fprintf(&b, "authors = %s\n", quoteList(r.Authors))
	}
	fmt.Fprintf(&b, "kind = %s\n", strconv.Quote(string(r.Kind)))
	writeBuild(&b, r.Build)
	writeRequires(&b, r.Requires)
	writeDepends(&b, r.Depends)
	writeVersions(&b, r.Versions)
	return b.Bytes(), nil
}

func writeBuild(b *bytes.Buffer, build *Build) {
	if build == nil {
		return
	}
	fmt.Fprintf(b, "\n[build]\n")
	fmt.Fprintf(b, "system = %s\n", strconv.Quote(build.System))
	fmt.Fprintf(b, "subdir = %s\n", strconv.Quote(build.Subdir))
	fmt.Fprintf(b, "sc_path_var = %s\n", strconv.Quote(build.SCPathVar))
	if len(build.CMakeFlags) > 0 {
		fmt.Fprintf(b, "cmake_flags = %s\n", quoteList(build.CMakeFlags))
	}
	fmt.Fprintf(b, "submodules = %t\n", build.Submodules)
}

func writeRequires(b *bytes.Buffer, requires map[string]Requirement) {
	if len(requires) == 0 {
		return
	}
	fmt.Fprintf(b, "\n[requires]\n")
	for _, name := range sortedKeys(requires) {
		fmt.Fprintf(b, "%s = %s\n", strconv.Quote(name), inlineRequirement(requires[name]))
	}
}

// inlineRequirement renders a requirement as a TOML inline table, skipping empty fields.
func inlineRequirement(q Requirement) string {
	fields := []struct{ key, value string }{
		{"apt", q.Apt}, {"brew", q.Brew}, {"pacman", q.Pacman}, {"dnf", q.Dnf},
		{"winget", q.Winget}, {"vcpkg", q.Vcpkg}, {"pkgconfig", q.PkgConfig},
	}
	var parts []string
	for _, f := range fields {
		if f.value != "" {
			parts = append(parts, f.key+" = "+strconv.Quote(f.value))
		}
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func writeDepends(b *bytes.Buffer, depends map[string]string) {
	if len(depends) == 0 {
		return
	}
	fmt.Fprintf(b, "\n[depends]\n")
	for _, name := range sortedKeys(depends) {
		fmt.Fprintf(b, "%s = %s\n", strconv.Quote(name), strconv.Quote(depends[name]))
	}
}

func writeVersions(b *bytes.Buffer, v Versions) {
	fmt.Fprintf(b, "\n[versions]\ntag_prefix = %s\n", strconv.Quote(v.TagPrefix))
	fmt.Fprintf(b, "\n[versions.approved]\n")
	r := Recipe{Versions: v}
	for _, ver := range r.ApprovedVersions() {
		fmt.Fprintf(b, "%s = %s\n", strconv.Quote(ver), strconv.Quote(v.Approved[ver]))
	}
}

func quoteList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = strconv.Quote(s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
