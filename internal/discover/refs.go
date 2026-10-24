// Package discover finds packages without manual upkeep: it reads remote
// refs, the official quarks directory and the files inside a repository.
package discover

import (
	"context"
	"fmt"
	"strings"

	"github.com/sc-deken/sc-deken/internal/run"
)

// RemoteRefs asks the repository at url for its HEAD commit and its tags
// (name to commit). For annotated tags the peeled "^{}" commit wins.
func RemoteRefs(ctx context.Context, r run.Runner, url string) (head string, tags map[string]string, err error) {
	out, err := r.Output(ctx, run.Cmd{Name: "git", Args: []string{"ls-remote", url, "HEAD", "refs/tags/*"}})
	if err != nil {
		return "", nil, fmt.Errorf("list refs of %s: %w", url, err)
	}
	tags = map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		sha, ref, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}
		if ref == "HEAD" {
			head = sha
			continue
		}
		name, found := strings.CutPrefix(ref, "refs/tags/")
		if !found {
			continue
		}
		if peeled, isPeeled := strings.CutSuffix(name, "^{}"); isPeeled {
			tags[peeled] = sha
		} else if _, seen := tags[name]; !seen {
			tags[name] = sha
		}
	}
	return head, tags, nil
}
