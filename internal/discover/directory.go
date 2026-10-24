package discover

import "strings"

// Entry is one line of the official quarks directory.
type Entry struct{ Name, URL string }

// ParseDirectory reads "name=url" lines, skipping blanks and "#" comments.
// A trailing "@ref" after the repository path is dropped from the URL.
func ParseDirectory(txt string) []Entry {
	var entries []Entry
	for _, line := range strings.Split(txt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, url, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		entries = append(entries, Entry{Name: strings.TrimSpace(name), URL: stripRef(strings.TrimSpace(url))})
	}
	return entries
}

// stripRef removes an "@ref" suffix that follows the repository path. An "@"
// in the user part ("git@host:path") is left alone.
func stripRef(url string) string {
	pathStart := 0
	if _, rest, ok := strings.Cut(url, "://"); ok {
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return url
		}
		pathStart = len(url) - len(rest) + slash
	} else if i := strings.Index(url, ":"); i >= 0 {
		pathStart = i
	}
	if i := strings.Index(url[pathStart:], "@"); i >= 0 {
		return url[:pathStart+i]
	}
	return url
}
