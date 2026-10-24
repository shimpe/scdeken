package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shimpe/scdeken/internal/discover"
	"github.com/shimpe/scdeken/internal/run"
)

// DefaultRepoTimeout is how long one repository may take to list or scan.
const DefaultRepoTimeout = 3 * time.Minute

// GitScanner is the real Scanner: it asks git for refs and scans a shallow
// checkout in a scratch folder. git never asks for a password, and a
// repository that takes longer than Timeout counts as failed, so one bad
// repository cannot stall the nightly run.
type GitScanner struct {
	Runner  run.Runner
	Timeout time.Duration // 0 means DefaultRepoTimeout
}

// Refs lists the head and tags of the repository at url.
func (g GitScanner) Refs(ctx context.Context, url string) (string, map[string]string, error) {
	ctx, cancel := g.limit(ctx)
	defer cancel()
	return discover.RemoteRefs(ctx, noPrompt{g.Runner}, url)
}

// Scan inspects url at commit; the scratch folder is removed afterwards.
func (g GitScanner) Scan(ctx context.Context, url, commit string) (discover.Scan, error) {
	ctx, cancel := g.limit(ctx)
	defer cancel()
	tmp, err := os.MkdirTemp("", "scdeken-index-")
	if err != nil {
		return discover.Scan{}, fmt.Errorf("scan %s: %w", url, err)
	}
	defer os.RemoveAll(tmp)
	return discover.ScanRepo(ctx, noPrompt{g.Runner}, url, commit, filepath.Join(tmp, "src"))
}

func (g GitScanner) limit(ctx context.Context) (context.Context, context.CancelFunc) {
	if g.Timeout <= 0 {
		return context.WithTimeout(ctx, DefaultRepoTimeout)
	}
	return context.WithTimeout(ctx, g.Timeout)
}

// noPrompt runs every process with git's password prompts switched off, so a
// repository that has become private fails instead of waiting for input.
type noPrompt struct{ run.Runner }

func (n noPrompt) Run(ctx context.Context, c run.Cmd) error {
	return n.Runner.Run(ctx, withoutPrompts(c))
}

func (n noPrompt) Output(ctx context.Context, c run.Cmd) (string, error) {
	return n.Runner.Output(ctx, withoutPrompts(c))
}

func withoutPrompts(c run.Cmd) run.Cmd {
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	c.Env = append(append([]string(nil), env...), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	return c
}
