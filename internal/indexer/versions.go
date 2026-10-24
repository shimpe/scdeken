package indexer

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/version"
)

// Proposal is a new upstream version of a reviewed package that nobody has
// approved yet. The tag bot opens one pull request for each.
type Proposal struct {
	Package     string `json:"package"`
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Commit      string `json:"commit"`
	PrevVersion string `json:"prev_version"`
	PrevCommit  string `json:"prev_commit"`
	CompareURL  string `json:"compare_url,omitempty"`
}

// MovedTag is an approved version whose upstream tag now points at another
// commit than the one a reviewer approved.
type MovedTag struct {
	Package  string `json:"package"`
	Version  string `json:"version"`
	Tag      string `json:"tag"`
	Recorded string `json:"recorded"`
	Now      string `json:"now"`
}

// NewVersions compares every recipe's upstream tags with its approved
// versions. Pre-release tags are not proposed. A repository that cannot be
// reached does not stop the others; their errors come back joined, together
// with the results found.
func NewVersions(ctx context.Context, s Scanner, recipes []recipe.Recipe) ([]Proposal, []MovedTag, error) {
	var proposals []Proposal
	var moved []MovedTag
	var errs []error
	for _, r := range recipes {
		_, tags, err := s.Refs(ctx, r.Repo)
		if err != nil {
			errs = append(errs, fmt.Errorf("check %s: %w", r.Name, err))
			continue
		}
		proposals = append(proposals, proposalsFor(r, tags)...)
		moved = append(moved, movedTags(r, tags)...)
	}
	return proposals, moved, errors.Join(errs...)
}

// upstream is one tag that parses as a version.
type upstream struct {
	tag    string
	v      version.Version
	commit string
}

// upstreamVersions lists the tags that are versions, oldest first.
func upstreamVersions(r recipe.Recipe, tags map[string]string) []upstream {
	var out []upstream
	for tag, commit := range tags {
		if v, ok := version.FromTag(tag, r.Versions.TagPrefix); ok {
			out = append(out, upstream{tag, v, commit})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if c := out[i].v.Compare(out[j].v); c != 0 {
			return c < 0
		}
		return out[i].tag < out[j].tag
	})
	return out
}

// proposalsFor lists the stable upstream versions newer than the newest
// approved one.
func proposalsFor(r recipe.Recipe, tags map[string]string) []Proposal {
	newest := r.ApprovedVersions()[0]
	newestV, _ := version.Parse(newest)
	prevCommit := r.Versions.Approved[newest]
	var out []Proposal
	for _, u := range upstreamVersions(r, tags) {
		if u.v.Compare(newestV) <= 0 || u.v.IsPrerelease() || u.v.IsUnstable() {
			continue
		}
		out = append(out, Proposal{
			Package: r.Name, Version: u.v.String(), Tag: u.tag, Commit: u.commit,
			PrevVersion: newest, PrevCommit: prevCommit, CompareURL: compareURL(r.Repo, prevCommit, u.commit),
		})
	}
	return out
}

// compareURL is GitHub's page comparing two commits; other hosts give "".
func compareURL(repo, from, to string) string {
	owner, name, err := discover.ParseGitHubURL(repo)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", owner, name, from, to)
}

// movedTags lists approved versions whose tag points somewhere else now.
func movedTags(r recipe.Recipe, tags map[string]string) []MovedTag {
	var out []MovedTag
	for _, u := range upstreamVersions(r, tags) {
		recorded, approved := approvedCommit(r, u.v)
		if approved && recorded != u.commit {
			out = append(out, MovedTag{
				Package: r.Name, Version: u.v.String(), Tag: u.tag, Recorded: recorded, Now: u.commit,
			})
		}
	}
	return out
}

// approvedCommit returns the commit approved for a version equal to v.
func approvedCommit(r recipe.Recipe, v version.Version) (string, bool) {
	for text, commit := range r.Versions.Approved {
		if a, err := version.Parse(text); err == nil && a.Compare(v) == 0 {
			return commit, true
		}
	}
	return "", false
}
