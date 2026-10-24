package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/config"
	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/fetch"
	"github.com/sc-deken/sc-deken/internal/github"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/indexer"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
)

// topics are the GitHub topics whose repositories become unreviewed packages.
var topics = []string{"supercollider-quark", "supercollider-plugin", "supercollider-plugins", "supercollider-ugens"}

// gatherInputs reads and downloads everything "index build" needs. Local
// files must be valid; anything fetched from the network that fails is logged
// and left out, so one outage does not stop the nightly run.
func gatherInputs(ctx context.Context, m MaintainerArgs, s *session) (indexer.Inputs, error) {
	in := indexer.Inputs{Now: s.deps.Now(), Log: s.log}
	var err error
	if in.Recipes, err = recipe.LoadDir(m.Recipes); err != nil {
		return in, err
	}
	if m.Advisories != "" {
		if in.Advisories, err = advisory.LoadDir(m.Advisories); err != nil {
			return in, err
		}
	}
	if m.SCVersions != "" {
		if in.SCVersions, err = loadSCVersions(m.SCVersions); err != nil {
			return in, err
		}
	}
	tmp, err := os.MkdirTemp("", "scdeken-index-")
	if err != nil {
		return in, fmt.Errorf("make a scratch folder: %w", err)
	}
	defer os.RemoveAll(tmp)
	web := fetch.New(tmp) // a fresh cache folder per run, so nothing stale is reused
	client := &github.Client{Token: os.Getenv("GITHUB_TOKEN"), BaseURL: s.deps.GitHubURL}
	g := gatherer{web: web, client: client, log: s.log}

	in.Previous = g.previous(ctx, m.Previous)
	if in.Directory, err = g.directory(ctx, m.Directory, m.Limit); err != nil {
		return in, err
	}
	in.Topics = g.topics(ctx, m.Limit)
	in.Meta = g.meta(ctx, in)
	if m.ReleasesRepo != "" {
		g.releases(ctx, m.ReleasesRepo, &in)
	}
	return in, nil
}

// sCVersionsFile is the layout of sc-versions.toml.
type sCVersionsFile struct {
	SC []platform.SCVersion `toml:"sc"`
}

func loadSCVersions(path string) ([]platform.SCVersion, error) {
	var f sCVersionsFile
	md, err := toml.DecodeFile(path, &f)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}
	return f.SC, nil
}

// gatherer downloads the network inputs of an index build.
type gatherer struct {
	web    *fetch.Client
	client *github.Client
	log    io.Writer
}

func (g gatherer) logf(format string, args ...any) { fmt.Fprintf(g.log, format+"\n", args...) }

// read returns the text at an address or file path.
func (g gatherer) read(ctx context.Context, where string) ([]byte, error) {
	if strings.HasPrefix(where, "http://") || strings.HasPrefix(where, "https://") || strings.HasPrefix(where, "file://") {
		return g.web.Get(ctx, where)
	}
	data, err := os.ReadFile(where)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", where, err)
	}
	return data, nil
}

// previous loads the last published index; a missing or unreadable one is
// logged and treated as no index at all.
func (g gatherer) previous(ctx context.Context, where string) *index.Index {
	if where == "" {
		return nil
	}
	data, err := g.read(ctx, where)
	if err != nil {
		g.logf("No previous index (%v); every repository will be scanned.", err)
		return nil
	}
	ix, err := index.Decode(data)
	if err != nil {
		g.logf("Previous index is unusable (%v); every repository will be scanned.", err)
		return nil
	}
	return ix
}

// directory reads the quarks directory and keeps the first limit entries.
func (g gatherer) directory(ctx context.Context, where string, limit int) ([]discover.Entry, error) {
	if where == "" {
		where = config.QuarksDirectoryURL
	}
	data, err := g.read(ctx, where)
	if err != nil {
		return nil, fmt.Errorf("read the quarks directory: %w", err)
	}
	return firstN(discover.ParseDirectory(string(data)), limit), nil
}

// topics searches GitHub for every topic and merges the answers.
func (g gatherer) topics(ctx context.Context, limit int) []github.Repo {
	seen := map[string]bool{}
	var repos []github.Repo
	for _, topic := range topics {
		found, err := g.client.SearchTopic(ctx, topic)
		if err != nil {
			g.logf("Skipping topic %s: %v", topic, err)
			continue
		}
		for _, r := range found {
			if key := advisory.NormalizeRepo(r.HTMLURL); !seen[key] {
				seen[key] = true
				repos = append(repos, r)
			}
		}
	}
	return firstN(repos, limit)
}

func firstN[T any](list []T, n int) []T {
	if n > 0 && len(list) > n {
		return list[:n]
	}
	return list
}

// meta fetches GitHub's data for every recipe and directory repository. The
// topic repositories came with theirs.
func (g gatherer) meta(ctx context.Context, in indexer.Inputs) map[string]github.Repo {
	meta := map[string]github.Repo{}
	for _, t := range in.Topics {
		meta[advisory.NormalizeRepo(t.HTMLURL)] = t
	}
	var urls []string
	for _, r := range in.Recipes {
		urls = append(urls, r.Repo)
	}
	for _, e := range in.Directory {
		urls = append(urls, e.URL)
	}
	for _, url := range urls {
		key := advisory.NormalizeRepo(url)
		if _, have := meta[key]; have {
			continue
		}
		owner, name, err := discover.ParseGitHubURL(url)
		if err != nil {
			continue
		}
		if repo, err := g.client.Repo(ctx, owner+"/"+name); err != nil {
			g.logf("No GitHub details for %s: %v", url, err)
		} else {
			meta[key] = repo
		}
	}
	return meta
}

// releases lists the prebuilt releases of the recipes repository and, for
// the ones the recipes approve, downloads their checksums and file lists.
func (g gatherer) releases(ctx context.Context, repo string, in *indexer.Inputs) {
	all, err := g.client.Releases(ctx, repo)
	if err != nil {
		g.logf("No prebuilt releases: %v", err)
		return
	}
	newest := map[string]bool{}
	wanted := map[string]bool{}
	for _, r := range in.Recipes {
		for i, v := range r.ApprovedVersions() {
			wanted[r.Name+"-"+v] = true
			newest[r.Name+"-"+v] = i == 0
		}
	}
	in.Sums, in.Files = map[string]map[string]string{}, map[string]map[string][]string{}
	for _, rel := range all {
		if !wanted[rel.Tag] {
			continue
		}
		in.Releases = append(in.Releases, rel)
		g.readAssets(ctx, rel, newest[rel.Tag], in)
	}
}

// readAssets downloads SHA256SUMS and, for the newest version, the
// <platform-key>.files.txt lists of one release.
func (g gatherer) readAssets(ctx context.Context, rel github.Release, wantFiles bool, in *indexer.Inputs) {
	for _, a := range rel.Assets {
		switch {
		case a.Name == "SHA256SUMS":
			if text, ok := g.text(ctx, rel.Tag, a); ok {
				in.Sums[rel.Tag] = parseSums(text)
			}
		case wantFiles && strings.HasSuffix(a.Name, ".files.txt"):
			key := strings.TrimSuffix(a.Name, ".files.txt")
			if _, err := platform.ParseKey(key); err != nil {
				continue
			}
			if text, ok := g.text(ctx, rel.Tag, a); ok {
				if in.Files[rel.Tag] == nil {
					in.Files[rel.Tag] = map[string][]string{}
				}
				in.Files[rel.Tag][key] = strings.Fields(text)
			}
		}
	}
}

func (g gatherer) text(ctx context.Context, tag string, a github.Asset) (string, bool) {
	data, err := g.web.Get(ctx, a.URL)
	if err != nil {
		g.logf("Cannot read %s of release %s: %v", a.Name, tag, err)
		return "", false
	}
	return string(data), true
}

// parseSums reads sha256sum output: "<hex>  <name>" per line, where the name
// may start with "*" in binary mode.
func parseSums(text string) map[string]string {
	sums := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		sum, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !isHex(sum) {
			continue
		}
		sums[strings.TrimPrefix(strings.TrimSpace(name), "*")] = strings.ToLower(sum)
	}
	return sums
}

// isHex reports whether s is a non-empty string of hexadecimal digits.
func isHex(s string) bool {
	return s != "" && strings.Trim(s, "0123456789abcdefABCDEF") == ""
}
