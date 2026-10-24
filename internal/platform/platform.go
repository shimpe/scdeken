// Package platform names the machines prebuilt plugins run on and maps a
// SuperCollider release to the plugin API its binaries must be built for.
package platform

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/sc-deken/sc-deken/internal/version"
)

// Key identifies one kind of prebuilt binary: operating system, CPU
// architecture and SuperCollider plugin API.
type Key struct {
	OS   string
	Arch string
	API  int
}

// String renders the key as "<os>-<arch>-api<N>", e.g. "linux-x86_64-api3".
func (k Key) String() string {
	return fmt.Sprintf("%s-%s-api%d", k.OS, k.Arch, k.API)
}

// ParseKey reads a string produced by Key.String.
func ParseKey(s string) (Key, error) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return Key{}, fmt.Errorf("parse platform key %q: want <os>-<arch>-api<N>", s)
	}
	api, ok := strings.CutPrefix(parts[2], "api")
	n, err := strconv.Atoi(api)
	if !ok || err != nil || n < 0 {
		return Key{}, fmt.Errorf("parse platform key %q: %q is not api<N>", s, parts[2])
	}
	k := Key{OS: parts[0], Arch: parts[1], API: n}
	if err := k.validate(); err != nil {
		return Key{}, fmt.Errorf("parse platform key %q: %w", s, err)
	}
	return k, nil
}

// validate checks the OS and architecture against the supported values.
func (k Key) validate() error {
	switch k.OS {
	case "linux", "macos", "windows":
	default:
		return fmt.Errorf("unknown operating system %q", k.OS)
	}
	switch k.Arch {
	case "x86_64", "arm64":
	case "universal":
		if k.OS != "macos" {
			return fmt.Errorf("architecture universal exists only on macos")
		}
	default:
		return fmt.Errorf("unknown architecture %q", k.Arch)
	}
	return nil
}

// Accepts reports whether a binary with key build runs on the machine k:
// same OS and API, and the same architecture or a universal macOS build.
func (k Key) Accepts(build Key) bool {
	if k.OS != build.OS || k.API != build.API {
		return false
	}
	return k.Arch == build.Arch || (k.OS == "macos" && build.Arch == "universal")
}

// NormalizeArch maps the many spellings of a CPU architecture to "x86_64"
// or "arm64".
func NormalizeArch(s string) (string, error) {
	switch strings.ToLower(s) {
	case "x86_64", "amd64", "x64":
		return "x86_64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	}
	return "", fmt.Errorf("unsupported architecture %q", s)
}

// HostOS returns the running operating system as "linux", "macos" or
// "windows" (other systems are returned as Go names them).
func HostOS() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

// SCVersion is one row of sc-versions.toml / the index "sc_versions".
type SCVersion struct {
	// Version is the SuperCollider release CI builds against, e.g. "3.14.1".
	Version string `toml:"version" json:"version"`
	// API is the plugin API of that release.
	API int `toml:"api" json:"api"`
	// Min is the oldest SuperCollider release with this API that is supported.
	Min string `toml:"min" json:"min"`
}

// APIFor returns the plugin API for SuperCollider release sc. It picks the
// row with the greatest Min not above sc. It reports false when no row fits,
// or when sc is a newer major.minor than the row's Version, because such a
// release might have a new API. Dev versions count by their core: 3.15.0-dev
// is 3.15.
func APIFor(sc version.Version, table []SCVersion) (int, bool) {
	var best SCVersion
	var bestMin version.Version
	found := false
	for _, row := range table {
		oldest, err := version.Parse(row.Min)
		if err != nil || oldest.Compare(sc) > 0 {
			continue
		}
		if !found || oldest.Compare(bestMin) > 0 {
			best, bestMin, found = row, oldest, true
		}
	}
	if !found || newerThan(sc, best.Version) {
		return 0, false
	}
	return best.API, true
}

// newerThan reports whether sc's major.minor is above that of ref. An
// unparsable ref counts as newer so that bad table rows never match.
func newerThan(sc version.Version, ref string) bool {
	r, err := version.Parse(ref)
	if err != nil {
		return true
	}
	if sc.Major() != r.Major() {
		return sc.Major() > r.Major()
	}
	return sc.Minor() > r.Minor()
}
