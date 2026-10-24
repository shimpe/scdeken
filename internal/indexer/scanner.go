package indexer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sc-deken/sc-deken/internal/discover"
	"github.com/sc-deken/sc-deken/internal/run"
)

// GitScanner is the real Scanner: it asks git for refs and scans a shallow
// checkout in a scratch folder.
type GitScanner struct {
	Runner run.Runner
}

// Refs lists the head and tags of the repository at url.
func (g GitScanner) Refs(ctx context.Context, url string) (string, map[string]string, error) {
	return discover.RemoteRefs(ctx, g.Runner, url)
}

// Scan inspects url at commit; the scratch folder is removed afterwards.
func (g GitScanner) Scan(ctx context.Context, url, commit string) (discover.Scan, error) {
	tmp, err := os.MkdirTemp("", "scdeken-index-")
	if err != nil {
		return discover.Scan{}, fmt.Errorf("scan %s: %w", url, err)
	}
	defer os.RemoveAll(tmp)
	return discover.ScanRepo(ctx, g.Runner, url, commit, filepath.Join(tmp, "src"))
}
