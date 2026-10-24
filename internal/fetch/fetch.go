// Package fetch downloads the index and plugin archives: GET with an ETag
// cache, and downloads that are verified before they appear on disk.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	getTimeout  = 30 * time.Second
	idleTimeout = 60 * time.Second
)

// Client fetches URLs over HTTP(S) or from file:// paths.
type Client struct {
	HTTP      *http.Client
	CacheDir  string
	UserAgent string
}

// New returns a Client that keeps its response cache in cacheDir.
func New(cacheDir string) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: getTimeout},
		CacheDir:  cacheDir,
		UserAgent: "sc-deken",
	}
}

// Get returns the body at rawURL. HTTP responses are cached by ETag, and a
// 304 answer returns the cached body. file:// URLs are read from disk.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	if isFileURL(rawURL) {
		return readFileURL(rawURL)
	}
	body, err := c.getHTTP(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", rawURL, err)
	}
	return body, nil
}

func (c *Client) getHTTP(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := c.newRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	cached, _, haveCache := c.Cached(rawURL)
	if etag, err := os.ReadFile(c.path(rawURL, "etag")); err == nil && haveCache {
		req.Header.Set("If-None-Match", string(etag))
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified && haveCache:
		now := time.Now()
		_ = os.Chtimes(c.path(rawURL, "body"), now, now)
		return cached, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.store(rawURL, body, resp.Header.Get("ETag"))
	return body, nil
}

// Cached returns the last body stored for an http(s) URL and when it was
// fetched. It needs no network, so offline mode can use it.
func (c *Client) Cached(rawURL string) ([]byte, time.Time, bool) {
	if isFileURL(rawURL) || c.CacheDir == "" {
		return nil, time.Time{}, false
	}
	path := c.path(rawURL, "body")
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	return body, info.ModTime(), true
}

// store caches a body and its ETag. Cache problems never fail a fetch.
func (c *Client) store(rawURL string, body []byte, etag string) {
	if c.CacheDir == "" || os.MkdirAll(c.CacheDir, 0o755) != nil {
		return
	}
	etagPath := c.path(rawURL, "etag")
	if etag == "" {
		_ = os.Remove(etagPath)
	} else if writeAtomic(etagPath, []byte(etag)) != nil {
		return
	}
	_ = writeAtomic(c.path(rawURL, "body"), body)
}

func (c *Client) path(rawURL, ext string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return filepath.Join(c.CacheDir, hex.EncodeToString(sum[:])+"."+ext)
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

func (c *Client) newRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	return req, nil
}

// Download saves rawURL to dst, but only if its SHA-256 equals sha256hex.
// The bytes go to dst+".part" first; on any failure that file is removed, so
// dst never holds an unverified or partial download. progress may be nil;
// total is -1 until the end when the size is unknown.
func (c *Client) Download(ctx context.Context, rawURL, dst, sha256hex string, progress func(done, total int64)) error {
	part := dst + ".part"
	if err := c.downloadTo(ctx, rawURL, part, sha256hex, progress); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	return nil
}

func (c *Client) downloadTo(ctx context.Context, rawURL, part, sha256hex string, progress func(done, total int64)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	body, total, err := c.open(ctx, rawURL)
	if err != nil {
		return err
	}
	defer body.Close()
	src := newIdleReader(body, idleTimeout, cancel)
	defer src.timer.Stop()

	out, err := os.Create(part)
	if err != nil {
		return err
	}
	hash := sha256.New()
	var done int64
	count := countWriter(func(n int) {
		done += int64(n)
		if progress != nil {
			progress(done, total)
		}
	})
	_, copyErr := io.Copy(io.MultiWriter(out, hash, count), src)
	if closeErr := out.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return copyErr
	}
	if progress != nil && total < 0 {
		progress(done, done) // the size was unknown; now it is known
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, sha256hex) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", sha256hex, got)
	}
	return nil
}

// open starts reading rawURL and reports its size, or -1 if unknown.
func (c *Client) open(ctx context.Context, rawURL string) (io.ReadCloser, int64, error) {
	if isFileURL(rawURL) {
		return openFileURL(rawURL)
	}
	req, err := c.newRequest(ctx, rawURL)
	if err != nil {
		return nil, 0, err
	}
	hc := *c.HTTP
	hc.Timeout = 0 // downloads are limited by the idle timeout instead
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("server answered %s", resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}

func openFileURL(rawURL string) (io.ReadCloser, int64, error) {
	path, err := fileURLPath(rawURL)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// countWriter adapts a function to io.Writer, reporting each write's size.
type countWriter func(n int)

func (f countWriter) Write(p []byte) (int, error) {
	f(len(p))
	return len(p), nil
}

// idleReader cancels the request when a single Read takes longer than the
// idle timeout, so a stalled download fails instead of hanging.
type idleReader struct {
	io.Reader
	timer   *time.Timer
	timeout time.Duration
}

func newIdleReader(r io.Reader, timeout time.Duration, cancel context.CancelFunc) *idleReader {
	return &idleReader{Reader: r, timeout: timeout, timer: time.AfterFunc(timeout, cancel)}
}

func (r *idleReader) Read(p []byte) (int, error) {
	r.timer.Reset(r.timeout)
	return r.Reader.Read(p)
}

func isFileURL(rawURL string) bool {
	return strings.HasPrefix(strings.ToLower(rawURL), "file:")
}

var windowsDrivePath = regexp.MustCompile(`^/[A-Za-z]:`)

// fileURLPath converts file:///C:/x/y or file:///tmp/y to a local path.
func fileURLPath(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Path == "" {
		return "", errors.New("file URL has no path")
	}
	p := u.Path
	if windowsDrivePath.MatchString(p) {
		p = p[1:]
	}
	return filepath.FromSlash(p), nil
}

func readFileURL(rawURL string) ([]byte, error) {
	path, err := fileURLPath(rawURL)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return data, nil
}
