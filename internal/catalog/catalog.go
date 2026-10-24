// Package catalog loads the package index and decides whether to trust it.
// An index is used only if its minisign signature verifies against a trusted
// key, and that check also runs on copies read from the cache. An index older
// than one already accepted is refused, so a replayed old list does no harm.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sc-deken/sc-deken/internal/fetch"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/sign"
)

const defaultMaxAge = time.Hour

// Options say where the index lives and how much to trust and reuse.
type Options struct {
	URL             string        // config.DefaultIndexURL unless overridden (--index-url)
	Offline         bool          // use the cache only
	MaxAge          time.Duration // use the cache without asking the server while younger; 0 means 1h
	TrustedKeysPath string        // file of extra trusted public keys
	EmbeddedKey     string        // sign.EmbeddedPublicKey (tests pass their own)
	StatePath       string        // file holding the newest "generated" time seen; empty turns the rollback check off
	Log             io.Writer     // receives notes for the user; nil discards them
}

// pair is an index together with the signature that must vouch for it. The
// two always come from the same source.
type pair struct {
	index, sig []byte
}

// Load returns the signed index. It never returns an index whose signature
// did not verify, and it changes no files unless the whole load succeeds.
func Load(ctx context.Context, f *fetch.Client, o Options) (*index.Index, error) {
	cached, cachedAt, haveCache := cachedPair(f, o.URL)
	offline := o.Offline && !isFileURL(o.URL)

	if offline {
		if !haveCache {
			return nil, errors.New("No package list yet; connect to the internet once so sc-deken can download it.")
		}
		return accept(cached, o)
	}
	if haveCache && time.Since(cachedAt) < maxAge(o) {
		if ix, err := accept(cached, o); err == nil {
			return ix, nil
		}
		// A bad cached copy is not fatal while the server can be asked.
	}
	fresh, err := fetchPair(ctx, f, o.URL)
	if err != nil {
		if !haveCache {
			return nil, err
		}
		logf(o, "Using the package list from %s; could not reach the server.", cachedAt.Local().Format("2006-01-02 15:04"))
		return accept(cached, o)
	}
	return accept(fresh, o)
}

func maxAge(o Options) time.Duration {
	if o.MaxAge <= 0 {
		return defaultMaxAge
	}
	return o.MaxAge
}

func logf(o Options, format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format+"\n", args...)
	}
}

func isFileURL(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(rawURL), "file:")
}

// cachedPair returns the cached index and signature, and when the index was
// fetched. Both must be present.
func cachedPair(f *fetch.Client, url string) (pair, time.Time, bool) {
	body, at, ok := f.Cached(url)
	if !ok {
		return pair{}, time.Time{}, false
	}
	sig, _, ok := f.Cached(url + ".minisig")
	if !ok {
		return pair{}, time.Time{}, false
	}
	return pair{index: body, sig: sig}, at, true
}

func fetchPair(ctx context.Context, f *fetch.Client, url string) (pair, error) {
	body, err := f.Get(ctx, url)
	if err != nil {
		return pair{}, err
	}
	sig, err := f.Get(ctx, url+".minisig")
	if err != nil {
		return pair{}, err
	}
	return pair{index: body, sig: sig}, nil
}

// accept runs every trust check in order and only then records the result:
// verify the signature, decode, check for rollback, learn the next keys,
// remember the generated time.
func accept(p pair, o Options) (*index.Index, error) {
	trusted, err := sign.Trusted(o.EmbeddedKey, o.TrustedKeysPath)
	if err != nil {
		return nil, err
	}
	if err := sign.Verify(p.index, p.sig, trusted); err != nil {
		return nil, fmt.Errorf("package list from %s: %w", o.URL, err)
	}
	ix, err := index.Decode(p.index)
	if err != nil {
		return nil, err
	}
	if err := checkRollback(o.StatePath, ix.Generated); err != nil {
		return nil, err
	}
	if len(ix.NextPublicKeys) > 0 {
		if err := sign.AddTrusted(o.TrustedKeysPath, ix.NextPublicKeys); err != nil {
			return nil, err
		}
	}
	if err := saveGenerated(o.StatePath, ix.Generated); err != nil {
		return nil, err
	}
	return ix, nil
}

// readGenerated returns the stored time; ok is false when none is stored.
func readGenerated(statePath string) (t time.Time, ok bool, err error) {
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read %s: %w", statePath, err)
	}
	t, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return time.Time{}, false, fmt.Errorf("read %s: %w", statePath, err)
	}
	return t, true, nil
}

func checkRollback(statePath string, generated time.Time) error {
	if statePath == "" {
		return nil
	}
	seen, ok, err := readGenerated(statePath)
	if err != nil {
		return err
	}
	if ok && generated.Before(seen) {
		return fmt.Errorf("The package list from the server (%s) is older than the one already seen (%s); refusing it in case someone is replaying an old list.",
			generated.Format(time.RFC3339), seen.Format(time.RFC3339))
	}
	return nil
}

// saveGenerated writes the time atomically.
func saveGenerated(statePath string, generated time.Time) error {
	if statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(generated.Format(time.RFC3339Nano)+"\n"), 0o644); err != nil {
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save %s: %w", statePath, err)
	}
	return nil
}
