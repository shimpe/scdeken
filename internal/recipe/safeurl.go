package recipe

import (
	"net/url"
	"regexp"
	"strings"
)

// safeURLChars is what the host and the path of a dependency address may
// contain. SuperCollider's Quarks passes the address to git through a shell
// string without quoting, so anything else is refused.
var safeURLChars = regexp.MustCompile(`^[A-Za-z0-9._~/-]*$`)

// SafeRepoURL reports whether s is an https repository address that is safe
// to hand to git: https only, a host and a path made of letters, digits and
// . _ ~ - (the path also /), no port, user name, query or fragment, and no
// "." or ".." path elements.
func SafeRepoURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if !safeURLChars.MatchString(u.Host) || !safeURLChars.MatchString(u.EscapedPath()) || strings.Contains(u.Host, "/") {
		return false
	}
	if u.EscapedPath() != u.Path || strings.Contains(s, "#") || strings.Contains(s, "?") {
		return false // percent escapes are not allowed
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}
