// Package version parses and compares package versions.
//
// Versions follow semantic versioning loosely: a dotted numeric core of one to
// four parts (missing parts count as zero), an optional "-prerelease" and an
// optional "+build" suffix.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

const maxCoreParts = 4

// Version is a parsed package version. The zero value is not meaningful; use
// Parse, FromTag or Unstable.
type Version struct {
	text  string
	core  [maxCoreParts]int
	pre   []string
	build bool
}

// Parse reads a version such as "1.2", "0.4.5-rc1" or "0.0.0+abc1234".
// One leading "v" or "V" is accepted and dropped.
func Parse(s string) (Version, error) {
	text := strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	rest, build, hasBuild := strings.Cut(text, "+")
	if hasBuild && build == "" {
		return Version{}, fmt.Errorf("parse version %q: empty build metadata", s)
	}
	coreText, preText, hasPre := strings.Cut(rest, "-")
	core, err := parseCore(coreText)
	if err != nil {
		return Version{}, fmt.Errorf("parse version %q: %w", s, err)
	}
	v := Version{text: text, core: core, build: hasBuild}
	if hasPre {
		if v.pre, err = parsePrerelease(preText); err != nil {
			return Version{}, fmt.Errorf("parse version %q: %w", s, err)
		}
	}
	return v, nil
}

// parseCore reads the dotted numeric part, padding missing parts with zero.
func parseCore(s string) ([maxCoreParts]int, error) {
	var core [maxCoreParts]int
	parts := strings.Split(s, ".")
	if len(parts) > maxCoreParts {
		return core, fmt.Errorf("more than %d numeric parts", maxCoreParts)
	}
	for i, p := range parts {
		n, err := parseNumber(p)
		if err != nil {
			return core, err
		}
		core[i] = n
	}
	return core, nil
}

// parsePrerelease splits the text after "-" into its dot-separated identifiers.
func parsePrerelease(s string) ([]string, error) {
	ids := strings.Split(s, ".")
	for _, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("empty prerelease identifier")
		}
	}
	return ids, nil
}

// parseNumber accepts only plain decimal digits.
func parseNumber(s string) (int, error) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	return strconv.Atoi(s)
}

// FromTag turns a release tag into a version: it strips prefix, then one
// optional leading "v" or "V", then parses. ok is false when the tag is not a
// version, such as "latest".
func FromTag(tag, prefix string) (Version, bool) {
	rest, found := strings.CutPrefix(tag, prefix)
	if !found {
		return Version{}, false
	}
	v, err := Parse(rest)
	return v, err == nil
}

// Unstable names an untagged build of the given commit: "0.0.0+" followed by
// the first seven characters of the commit.
func Unstable(commit string) Version {
	if len(commit) > 7 {
		commit = commit[:7]
	}
	v, _ := Parse("0.0.0+" + commit)
	return v
}

// IsUnstable reports whether the version carries build metadata, which is how
// untagged commits are marked.
func (v Version) IsUnstable() bool { return v.build }

// String returns the version as written, without a leading "v".
func (v Version) String() string { return v.text }

// Major returns the first numeric part of the version.
func (v Version) Major() int { return v.core[0] }

// Minor returns the second numeric part of the version.
func (v Version) Minor() int { return v.core[1] }

// Compare returns -1, 0 or 1 by semantic version precedence. Build metadata is
// ignored, and a prerelease sorts below its release.
func (v Version) Compare(o Version) int {
	for i := range v.core {
		if c := cmpInt(v.core[i], o.core[i]); c != 0 {
			return c
		}
	}
	return comparePrerelease(v.pre, o.pre)
}

// comparePrerelease orders prerelease identifier lists (semver section 11).
// No prerelease outranks any prerelease; a shorter list loses to a longer one
// that starts the same.
func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(a), len(b))
}

// compareIdentifier compares numerically when both are numbers, lexically
// otherwise; a number sorts below a non-number.
func compareIdentifier(a, b string) int {
	na, errA := parseNumber(a)
	nb, errB := parseNumber(b)
	switch {
	case errA == nil && errB == nil:
		return cmpInt(na, nb)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// Max returns the highest version in vs. ok is false when vs is empty.
func Max(vs []Version) (best Version, ok bool) {
	for i, v := range vs {
		if i == 0 || v.Compare(best) > 0 {
			best, ok = v, true
		}
	}
	return best, ok
}
