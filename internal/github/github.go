// Package github is a small GitHub REST v3 client for finding packages.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

// DefaultBaseURL is the public GitHub API.
const DefaultBaseURL = "https://api.github.com"

// Client talks to the GitHub API. Zero values give sensible defaults.
type Client struct {
	HTTP    *http.Client
	BaseURL string // default DefaultBaseURL
	Token   string // optional; raises rate limits
}

// Repo is the part of a GitHub repository the indexer cares about.
type Repo struct {
	FullName, HTMLURL, CloneURL, Description, DefaultBranch string
	License                                                 string // SPDX id or ""
	Stars                                                   int
	PushedAt                                                time.Time
	Archived                                                bool
	Topics                                                  []string // GitHub topics; SearchTopic adds the one it searched for
}

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name, URL string
	Size      int64
}

// Release is a tagged GitHub release with its files.
type Release struct {
	Tag    string
	Draft  bool // unpublished; the indexer ignores it
	Assets []Asset
}

// SearchTopic returns every repository carrying the given topic. GitHub's
// search API stops after 1000 results, which is plenty here.
func (c *Client) SearchTopic(ctx context.Context, topic string) ([]Repo, error) {
	q := url.Values{"q": {"topic:" + topic}, "per_page": {"100"}}
	var repos []Repo
	err := c.eachPage(ctx, "/search/repositories?"+q.Encode(), func(body json.RawMessage) error {
		var page struct {
			Items []repoJSON `json:"items"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		for _, it := range page.Items {
			repos = append(repos, withTopic(it.repo(), topic))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search topic %s: %w", topic, err)
	}
	return repos, nil
}

// Repo fetches one repository by "owner/name".
func (c *Client) Repo(ctx context.Context, fullName string) (Repo, error) {
	resp, err := c.get(ctx, c.base()+"/repos/"+fullName)
	if err != nil {
		return Repo{}, fmt.Errorf("get repo %s: %w", fullName, err)
	}
	defer resp.Body.Close()
	var r repoJSON
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return Repo{}, fmt.Errorf("decode repo %s: %w", fullName, err)
	}
	return r.repo(), nil
}

// Releases lists every release of a repository.
func (c *Client) Releases(ctx context.Context, fullName string) ([]Release, error) {
	var out []Release
	err := c.eachPage(ctx, "/repos/"+fullName+"/releases?per_page=100", func(body json.RawMessage) error {
		var page []releaseJSON
		if err := json.Unmarshal(body, &page); err != nil {
			return err
		}
		for _, r := range page {
			out = append(out, r.release())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list releases of %s: %w", fullName, err)
	}
	return out, nil
}

type repoJSON struct {
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	License       *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Topics   []string  `json:"topics"`
	Stars    int       `json:"stargazers_count"`
	PushedAt time.Time `json:"pushed_at"`
	Archived bool      `json:"archived"`
}

func (j repoJSON) repo() Repo {
	r := Repo{
		FullName: j.FullName, HTMLURL: j.HTMLURL, CloneURL: j.CloneURL,
		Description: j.Description, DefaultBranch: j.DefaultBranch,
		Stars: j.Stars, PushedAt: j.PushedAt, Archived: j.Archived, Topics: j.Topics,
	}
	if j.License != nil && j.License.SPDXID != "NOASSERTION" {
		r.License = j.License.SPDXID
	}
	return r
}

type releaseJSON struct {
	Tag    string `json:"tag_name"`
	Draft  bool   `json:"draft"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (j releaseJSON) release() Release {
	r := Release{Tag: j.Tag, Draft: j.Draft}
	for _, a := range j.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return r
}

func (c *Client) base() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return c.BaseURL
}

// eachPage fetches path and follows Link rel="next" headers, handing each
// response body to handle.
func (c *Client) eachPage(ctx context.Context, path string, handle func(json.RawMessage) error) error {
	next := c.base() + path
	for next != "" {
		resp, err := c.get(ctx, next)
		if err != nil {
			return err
		}
		var body json.RawMessage
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if err := handle(body); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		next = nextLink(resp.Header.Get("Link"))
	}
	return nil
}

// get performs one authenticated GET and turns non-2xx answers into errors.
func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "sc-deken")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	h := c.HTTP
	if h == nil {
		h = http.DefaultClient
	}
	resp, err := h.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", u, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, statusError(u, resp.StatusCode)
	}
	return resp, nil
}

// statusError describes a failed request; rate-limit answers get a hint.
func statusError(u string, code int) error {
	msg := fmt.Sprintf("GET %s: status %d %s", u, code, http.StatusText(code))
	if code == http.StatusForbidden || code == http.StatusTooManyRequests {
		msg += " (probably GitHub's rate limit: set the GITHUB_TOKEN environment variable, or try again later)"
	}
	return fmt.Errorf("%s", msg)
}

var nextRe = regexp.MustCompile(`<([^>]*)>\s*;\s*rel="next"`)

// nextLink extracts the rel="next" URL from a Link header, or "".
func nextLink(header string) string {
	if m := nextRe.FindStringSubmatch(header); m != nil {
		return m[1]
	}
	return ""
}

// withTopic makes sure r lists topic, which the search proved it has even
// when the API left the topics out of the answer.
func withTopic(r Repo, topic string) Repo {
	for _, t := range r.Topics {
		if t == topic {
			return r
		}
	}
	r.Topics = append(append([]string(nil), r.Topics...), topic)
	return r
}
