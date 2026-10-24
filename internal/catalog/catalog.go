// Package catalog loads the package index and decides whether to trust it.
// An index is used only if its minisign signature verifies against a trusted
// key, and that check also runs on the saved copy used as a fallback. An index
// older than one already accepted is refused, so a replayed old list does no
// harm.
//
// The fetch package's response cache is never trusted as a fallback: it holds
// whatever the server last said. The fallback is the last pair this package
// verified and accepted, kept in Options.Dir.
package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	defaultMaxAge = time.Hour
	indexFile     = "index.json"
	sigFile       = "index.json.minisig"
	stateFile     = "last-generated.txt"
)

// Options say where the index lives and how much to trust and reuse.
type Options struct {
	URL             string        // config.DefaultIndexURL unless overridden (--index-url)
	Offline         bool          // use the saved, verified copy only
	MaxAge          time.Duration // use the saved copy without asking the server while younger; 0 means 1h
	TrustedKeysPath string        // file of extra trusted public keys
	EmbeddedKey     string        // sign.EmbeddedPublicKey (tests pass their own)
	Dir             string        // folder for this package's files, one subfolder per URL; empty turns off the rollback check and the saved copy
	Log             io.Writer     // receives notes for the user; nil discards them
}

// pair is an index together with the signature that must vouch for it. The
// two always come from the same source.
type pair struct {
	index, sig []byte
}

// Load returns the signed index. It never returns an index whose signature
// did not verify. Only after a pair has been verified, decoded and checked
// against rollback does it write anything: the new trusted keys named in the
// index, then (in Options.Dir) the pair itself and its generated time. The
// fetch cache is written by fetch as soon as bytes arrive and is never used
// as a fallback.
func Load(ctx context.Context, f *fetch.Client, o Options) (*index.Index, error) {
	dir := urlDir(o)
	saved, savedAt, haveSaved := readSaved(dir)
	local := isFileURL(o.URL)

	if o.Offline && !local {
		if !haveSaved {
			return nil, errors.New("No package list yet; connect to the internet once so sc-deken can download it.")
		}
		return check(saved, o)
	}
	if haveSaved && !local && time.Since(savedAt) < maxAge(o) {
		if ix, err := check(saved, o); err == nil {
			return ix, nil
		}
		// A bad saved copy is not fatal while the server can be asked.
	}
	fresh, err := fetchPair(ctx, f, o.URL)
	if err != nil {
		if ix, ok := fallback(saved, haveSaved, o); ok {
			logf(o, "Using the package list from %s; could not reach the server.", clock(savedAt))
			return ix, nil
		}
		return nil, err
	}
	ix, err := check(fresh, o)
	if err != nil {
		if fb, ok := fallback(saved, haveSaved, o); ok {
			logf(o, "The package list from the server could not be verified (%v); using the package list from %s.", err, clock(savedAt))
			return fb, nil
		}
		return nil, err
	}
	if err := commit(ix, fresh, dir, o); err != nil {
		return nil, err
	}
	return ix, nil
}

func maxAge(o Options) time.Duration {
	if o.MaxAge <= 0 {
		return defaultMaxAge
	}
	return o.MaxAge
}

func clock(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func logf(o Options, format string, args ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format+"\n", args...)
	}
}

func isFileURL(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(rawURL), "file:")
}

// urlDir is the folder for this URL's files, or "" when Dir is not set.
func urlDir(o Options) string {
	if o.Dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(o.URL))
	return filepath.Join(o.Dir, hex.EncodeToString(sum[:])[:16])
}

// readSaved returns the last accepted pair and when it was saved.
func readSaved(dir string) (pair, time.Time, bool) {
	if dir == "" {
		return pair{}, time.Time{}, false
	}
	body, err := os.ReadFile(filepath.Join(dir, indexFile))
	if err != nil {
		return pair{}, time.Time{}, false
	}
	info, err := os.Stat(filepath.Join(dir, indexFile))
	if err != nil {
		return pair{}, time.Time{}, false
	}
	sig, err := os.ReadFile(filepath.Join(dir, sigFile))
	if err != nil {
		return pair{}, time.Time{}, false
	}
	return pair{index: body, sig: sig}, info.ModTime(), true
}

// fallback accepts the saved pair if there is one and it still passes every
// check.
func fallback(saved pair, have bool, o Options) (*index.Index, bool) {
	if !have {
		return nil, false
	}
	ix, err := check(saved, o)
	return ix, err == nil
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

// check runs the trust checks and writes nothing: verify the signature,
// decode, refuse a rollback.
func check(p pair, o Options) (*index.Index, error) {
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
	if err := checkRollback(urlDir(o), ix.Generated); err != nil {
		return nil, err
	}
	return ix, nil
}

// commit records an accepted index: learn the next keys, then save the pair
// and the generated time.
func commit(ix *index.Index, p pair, dir string, o Options) error {
	if len(ix.NextPublicKeys) > 0 {
		if err := sign.AddTrusted(o.TrustedKeysPath, ix.NextPublicKeys); err != nil {
			return err
		}
	}
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("save package list in %s: %w", dir, err)
	}
	stamp := []byte(ix.Generated.Format(time.RFC3339Nano) + "\n")
	// The index goes last so its file time says when the pair was complete.
	for _, w := range []struct {
		name string
		data []byte
	}{{sigFile, p.sig}, {stateFile, stamp}, {indexFile, p.index}} {
		if err := writeAtomic(filepath.Join(dir, w.name), w.data); err != nil {
			return fmt.Errorf("save package list in %s: %w", dir, err)
		}
	}
	return nil
}

func checkRollback(dir string, generated time.Time) error {
	if dir == "" {
		return nil
	}
	statePath := filepath.Join(dir, stateFile)
	data, err := os.ReadFile(statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", statePath, err)
	}
	seen, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("read %s: %w", statePath, err)
	}
	if generated.Before(seen) {
		return fmt.Errorf("The package list from the server (%s) is older than the one already seen (%s); refusing it in case someone is replaying an old list.",
			generated.Format(time.RFC3339), seen.Format(time.RFC3339))
	}
	return nil
}

// writeAtomic writes data to a temp file and renames it into place.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
