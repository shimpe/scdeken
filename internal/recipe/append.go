package recipe

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var approvedHeader = regexp.MustCompile(`^\s*\[\s*versions\s*\.\s*approved\s*\]\s*(#.*)?$`)

// AppendApproved records a newly approved version in the recipe file. It edits
// the text, so the maintainers' comments and layout stay as they were.
func AppendApproved(path, version, commit string) error {
	current, err := Load(path)
	if err != nil {
		return err
	}
	if _, ok := current.Versions.Approved[version]; ok {
		return fmt.Errorf("%s: version %s is already approved", path, version)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("approve %s: %w", version, err)
	}
	updated, err := insertApproved(string(data), version, commit)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if _, err := parseLayout([]byte(updated)); err != nil {
		return fmt.Errorf("%s: new approval is not valid: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// insertApproved adds the version line after the last non-blank line of the
// [versions.approved] table, which is the last table of the file. The new
// line copies the line ending of the line before it.
func insertApproved(text, version, commit string) (string, error) {
	lines := strings.Split(text, "\n")
	header := slices.IndexFunc(lines, func(l string) bool {
		return approvedHeader.MatchString(strings.TrimRight(l, "\r"))
	})
	if header < 0 {
		return "", fmt.Errorf("no [versions.approved] table found")
	}
	last := header
	for i := header + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			last = i
		}
	}
	entry := strconv.Quote(version) + " = " + strconv.Quote(commit)
	if strings.HasSuffix(lines[last], "\r") {
		entry += "\r"
	}
	lines = slices.Insert(lines, last+1, entry)
	return strings.Join(lines, "\n"), nil
}
