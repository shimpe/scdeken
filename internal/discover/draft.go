package discover

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/version"
)

// untaggedVersion names the first approved version of a repository without tags.
const untaggedVersion = "0.1.0"

var plainName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// ParseGitHubURL returns the owner and repository name of a github.com URL.
func ParseGitHubURL(url string) (owner, repo string, err error) {
	rest, ok := strings.CutPrefix(url, "https://github.com/")
	if !ok {
		return "", "", errors.New("only GitHub repositories are supported for now")
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%s: expected https://github.com/<owner>/<repository>", url)
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

// Release is the version a draft approves.
type Release struct {
	TagPrefix string
	Version   string
	Commit    string
	Tagged    bool // false when no tag was usable and head stands in
}

// PickRelease chooses what to approve: the newest tagged version, or head as
// version 0.1.0 when the repository has no usable tags.
func PickRelease(head string, tags map[string]string) Release {
	prefix, ok := tagPrefix(tags)
	if !ok {
		return Release{Version: untaggedVersion, Commit: head}
	}
	var best version.Version
	var bestTag string
	for _, tag := range tieOrder(tags) {
		v, parsed := version.FromTag(tag, prefix)
		if !parsed || !betterRelease(v, best, bestTag != "") {
			continue
		}
		best, bestTag = v, tag
	}
	return Release{TagPrefix: prefix, Version: best.String(), Commit: tags[bestTag], Tagged: true}
}

// tieOrder lists the tag names so that, when two tags give the same version,
// the first one listed wins: tags without a "v" prefix first, then by name.
func tieOrder(tags map[string]string) []string {
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	sort.Slice(names, func(i, j int) bool {
		vi, vj := strings.HasPrefix(names[i], "v"), strings.HasPrefix(names[j], "v")
		if vi != vj {
			return vj
		}
		return names[i] < names[j]
	})
	return names
}

// betterRelease reports whether v should replace the current best: newer
// wins, but a prerelease never beats a final release.
func betterRelease(v, best version.Version, haveBest bool) bool {
	if !haveBest {
		return true
	}
	if v.IsPrerelease() != best.IsPrerelease() {
		return !v.IsPrerelease()
	}
	return v.Compare(best) > 0
}

// tagPrefix finds the text before the version in the tags. It tries "" (the
// leading "v" is stripped anyway), then the most common non-digit start of the
// tags, and accepts one only when most tags parse with it.
func tagPrefix(tags map[string]string) (string, bool) {
	names := make([]string, 0, len(tags))
	for tag := range tags {
		names = append(names, tag)
	}
	for _, prefix := range []string{"", commonWordPrefix(names)} {
		if mostParse(names, prefix) {
			return prefix, true
		}
	}
	return "", false
}

func mostParse(tags []string, prefix string) bool {
	n := 0
	for _, tag := range tags {
		if _, ok := version.FromTag(tag, prefix); ok {
			n++
		}
	}
	return n*2 > len(tags)
}

// commonWordPrefix is the non-digit start shared by most tags (cut before
// the first digit); the longest wins a tie.
func commonWordPrefix(names []string) string {
	counts := map[string]int{}
	best := ""
	for _, name := range names {
		i := strings.IndexAny(name, "0123456789")
		if i < 0 {
			continue
		}
		p := name[:i]
		counts[p]++
		if counts[p] > counts[best] || (counts[p] == counts[best] && len(p) > len(best)) {
			best = p
		}
	}
	return best
}

// DraftRecipe turns what is known about a repository into a recipe a
// maintainer can review, plus notes about what still needs a human look.
// It touches no network.
func DraftRecipe(url string, meta github.Repo, scan Scan, head string, tags map[string]string) (recipe.Recipe, []string, error) {
	if scan.Kind == "" {
		return recipe.Recipe{}, nil, fmt.Errorf("%s doesn't look like a SuperCollider plugin or quark", url)
	}
	owner, repo, err := ParseGitHubURL(url)
	if err != nil {
		return recipe.Recipe{}, nil, err
	}
	r := recipe.Recipe{
		Name:    draftName(repo, scan),
		Repo:    "https://github.com/" + owner + "/" + repo,
		Authors: []string{owner},
		Kind:    scan.Kind,
		Depends: draftDepends(scan),
	}
	var notes []string
	r.Summary, notes = draftSummary(meta, scan, notes)
	r.License, notes = draftLicense(meta, scan, notes)
	if r.HasPlugin() {
		r.Build = draftBuild(scan)
	}
	rel := PickRelease(head, tags)
	r.Versions = recipe.Versions{
		TagPrefix: rel.TagPrefix,
		Approved:  map[string]string{rel.Version: rel.Commit},
	}
	if !rel.Tagged {
		notes = append(notes, fmt.Sprintf("no tags upstream; version name %s chosen for commit %s", rel.Version, short(rel.Commit)))
	}
	return r, notes, nil
}

func draftName(repo string, scan Scan) string {
	if scan.HasQuark && scan.Quark.Name != "" {
		return scan.Quark.Name
	}
	return repo
}

func draftSummary(meta github.Repo, scan Scan, notes []string) (string, []string) {
	switch {
	case scan.Quark.Summary != "":
		return scan.Quark.Summary, notes
	case meta.Description != "":
		return meta.Description, notes
	}
	return "TODO: one-line summary", append(notes, "summary: nothing to take it from; write a one-line summary")
}

func draftLicense(meta github.Repo, scan Scan, notes []string) (string, []string) {
	switch {
	case meta.License != "":
		return meta.License, notes
	case scan.LicenseFile != "":
		return scan.LicenseFile, append(notes, "license: guessed from the license file; please check it")
	}
	return "TODO", append(notes, "license: could not be determined; enter its SPDX id")
}

func draftBuild(scan Scan) *recipe.Build {
	b := &recipe.Build{System: "cmake", SCPathVar: scan.SCPathVar, Submodules: scan.Submodules}
	if b.SCPathVar == "" {
		b.SCPathVar = "SC_PATH"
	}
	if scan.CMakeDir != "." {
		b.Subdir = scan.CMakeDir
	}
	if scan.MentionsSupernova {
		b.CMakeFlags = []string{"-DSUPERNOVA=ON"}
	}
	return b
}

// draftDepends keeps the quark dependencies that are plain package names.
func draftDepends(scan Scan) map[string]string {
	var depends map[string]string
	for _, dep := range scan.Quark.Dependencies {
		if !plainName.MatchString(dep) {
			continue
		}
		if depends == nil {
			depends = map[string]string{}
		}
		depends[dep] = "*"
	}
	return depends
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}
