package advisory

import "strings"

// NormalizeRepo turns the common spellings of a git repository address into
// one canonical form, so two spellings of the same repo compare equal:
// lowercase, https, no ".git" suffix, no trailing slash.
// "git@github.com:O/R.git" becomes "https://github.com/o/r".
func NormalizeRepo(url string) string {
	s := strings.ToLower(strings.TrimSpace(url))
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = "https://" + strings.Replace(rest, ":", "/", 1)
	}
	for _, scheme := range []string{"https://", "http://", "git://", "ssh://git@", "ssh://"} {
		if rest, ok := strings.CutPrefix(s, scheme); ok {
			s = "https://" + rest
			break
		}
	}
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, ".git")
	return strings.TrimRight(s, "/")
}
