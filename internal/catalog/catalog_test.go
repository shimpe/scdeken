package catalog

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shimpe/scdeken/internal/fetch"
	"github.com/shimpe/scdeken/internal/sign"
)

const password = "test-password"

type testKey struct {
	pub  string
	priv []byte
}

var (
	keysOnce sync.Once
	keys     [2]testKey // key generation is slow, so each test binary makes two
)

func testKeys() [2]testKey {
	keysOnce.Do(func() {
		for i := range keys {
			pub, priv, err := sign.GenerateKey(password)
			if err != nil {
				panic(err)
			}
			keys[i] = testKey{pub, priv}
		}
	})
	return keys
}

// server serves an index and its signature, both replaceable, and counts hits.
type server struct {
	*httptest.Server
	mu       sync.Mutex
	index    []byte
	sig      []byte
	sigDown  bool
	requests atomic.Int32
}

func newServer(t *testing.T, index, sig []byte) *server {
	t.Helper()
	s := &server{index: index, sig: sig}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ".minisig") && !s.sigDown:
			w.Write(s.sig)
		case strings.HasSuffix(r.URL.Path, ".minisig"):
			http.NotFound(w, r)
		default:
			w.Write(s.index)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) set(index, sig []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.index, s.sig = index, sig
}

func (s *server) breakSig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sigDown = true
}

func (s *server) url() string { return s.Server.URL + "/index.json" }

func signed(t *testing.T, k testKey, body string) (index, sig []byte) {
	t.Helper()
	sig, err := sign.Sign(k.priv, password, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(body), sig
}

func indexJSON(generated string, extra string) string {
	return `{"schema":1,"generated":"` + generated + `","sc_versions":[],"packages":[{"name":"mi-ugens","repo":"example/mi-ugens","kind":"plugin","trust":"reviewed"}],"advisories":[]` + extra + `}`
}

const (
	t1 = "2026-01-01T00:00:00Z"
	t2 = "2026-02-01T00:00:00Z"
)

type env struct {
	client *fetch.Client
	opts   Options
	log    *bytes.Buffer
}

func newEnv(t *testing.T, url string, k testKey) env {
	t.Helper()
	dir := t.TempDir()
	log := &bytes.Buffer{}
	return env{
		client: fetch.New(filepath.Join(dir, "cache")),
		log:    log,
		opts: Options{
			URL:             url,
			TrustedKeysPath: filepath.Join(dir, "trusted-keys"),
			EmbeddedKey:     k.pub,
			Dir:             filepath.Join(dir, "catalog"),
			Log:             log,
		},
	}
}

func (e env) load() error {
	_, err := Load(context.Background(), e.client, e.opts)
	return err
}

func TestLoadVerified(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	ix, err := Load(context.Background(), e.client, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Find("mi-ugens"); !ok {
		t.Errorf("package missing from %+v", ix.Packages)
	}
}

func TestLoadRejectsBadSignature(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[1], indexJSON(t1, ""))) // signed by another key
	e := newEnv(t, srv.url(), k[0])
	ix, err := Load(context.Background(), e.client, e.opts)
	if err == nil || !strings.Contains(err.Error(), "signature") || ix != nil {
		t.Fatalf("got %v, %v; want a signature error and no index", ix, err)
	}
	if files, _ := filepath.Glob(filepath.Join(e.opts.Dir, "*", "*")); len(files) != 0 {
		t.Errorf("files written for a rejected index: %v", files)
	}
}

func TestLoadVerifiesBeforeDecoding(t *testing.T) {
	k := testKeys()
	srv := newServer(t, []byte("not json at all"), []byte("not a signature"))
	e := newEnv(t, srv.url(), k[0])
	err := e.load()
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("got %v; want a signature error", err)
	}
}

func TestLoadRejectsUnknownSchema(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], `{"schema":2}`))
	e := newEnv(t, srv.url(), k[0])
	err := e.load()
	if err == nil || !strings.Contains(err.Error(), "newer than this sc-deken") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadOfflineUsesCache(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	e.opts.Offline = true
	ix, err := Load(context.Background(), e.client, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ix.Find("mi-ugens"); !ok {
		t.Error("cached index lost its package")
	}
}

func TestLoadOfflineVerifiesCache(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	// Tamper with the saved index on disk.
	saved := e.savedIndexPath(t)
	data, _ := os.ReadFile(saved)
	os.WriteFile(saved, bytes.Replace(data, []byte("mi-ugens"), []byte("evil-pkg"), 1), 0o644)
	e.opts.Offline = true
	if err := e.load(); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("got %v; want a signature error for the tampered cache", err)
	}
}

func TestLoadOfflineNoCache(t *testing.T) {
	k := testKeys()
	e := newEnv(t, "http://127.0.0.1:1/index.json", k[0])
	e.opts.Offline = true
	err := e.load()
	if err == nil || !strings.Contains(err.Error(), "connect to the internet once") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadAddsNextKeys(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, `,"next_public_keys":["`+k[1].pub+`"]`)))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	trusted, err := sign.Trusted("", e.opts.TrustedKeysPath)
	if err != nil || len(trusted) != 1 || trusted[0] != k[1].pub {
		t.Fatalf("trusted = %v, %v", trusted, err)
	}
}

func TestLoadRejectsRollback(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t2, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	srv.set(signed(t, k[0], indexJSON(t1, ""))) // validly signed, but older
	e.opts.MaxAge = time.Nanosecond
	// The replay is refused (the reason is logged) and the verified T2 pair is used.
	if got := e.loadedGenerated(t); !got.Equal(mustTime(t2)) {
		t.Errorf("generated = %v; want %s", got, t2)
	}
	if !strings.Contains(e.log.String(), "older than the one already seen") {
		t.Errorf("log = %q", e.log.String())
	}
	if got := e.storedGenerated(t); got != t2 {
		t.Errorf("stored state = %s; want %s", got, t2)
	}
	e.opts.Offline = true
	if got := e.loadedGenerated(t); !got.Equal(mustTime(t2)) {
		t.Errorf("offline generated = %v; want %s", got, t2)
	}
}

func TestLoadAcceptsSameGeneratedTime(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t2, "")))
	e := newEnv(t, srv.url(), k[0])
	e.opts.MaxAge = time.Nanosecond
	for i := 0; i < 2; i++ {
		if err := e.load(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLoadFreshCacheSkipsNetwork(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	before := srv.requests.Load()
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	if after := srv.requests.Load(); after != before {
		t.Errorf("server was asked %d more times; the cache was fresh", after-before)
	}
}

func TestLoadNetworkFailureUsesVerifiedCache(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	e.opts.MaxAge = time.Nanosecond
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.log.String(), "could not reach the server.") {
		t.Errorf("log = %q", e.log.String())
	}
}

func TestLoadNeverMixesFreshIndexWithCachedSignature(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	// A new index appears, but its signature cannot be fetched.
	idx, _ := signed(t, k[0], indexJSON(t2, ""))
	srv.set(idx, []byte("stale"))
	srv.breakSig()
	e.opts.MaxAge = time.Nanosecond
	ix, err := Load(context.Background(), e.client, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if !ix.Generated.Equal(mustTime(t1)) {
		t.Errorf("generated = %v; want the old, verified pair", ix.Generated)
	}
}

func TestLoadFileURL(t *testing.T) {
	k := testKeys()
	dir := t.TempDir()
	idx, sig := signed(t, k[0], indexJSON(t1, ""))
	os.WriteFile(filepath.Join(dir, "index.json"), idx, 0o644)
	os.WriteFile(filepath.Join(dir, "index.json.minisig"), sig, 0o644)
	url := "file:///" + filepath.ToSlash(filepath.Join(dir, "index.json"))
	e := newEnv(t, url, k[0])
	e.opts.Offline = true // there is no cache for files; they are always read
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
}

func mustTime(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}

func (e env) savedIndexPath(t *testing.T) string {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(e.opts.Dir, "*", "index.json"))
	if len(paths) != 1 {
		t.Fatalf("saved index files = %v", paths)
	}
	return paths[0]
}

func (e env) storedGenerated(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(e.savedIndexPath(t)), "last-generated.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// loadedGenerated loads and returns the generated time of the result.
func (e env) loadedGenerated(t *testing.T) time.Time {
	t.Helper()
	ix, err := Load(context.Background(), e.client, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	return ix.Generated
}

func TestLoadBrokenSignatureKeepsGoodPairForOffline(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	idx, _ := signed(t, k[0], indexJSON(t2, ""))
	srv.set(idx, nil)
	srv.breakSig()
	e.opts.MaxAge = time.Nanosecond
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	e.opts.Offline = true
	if got := e.loadedGenerated(t); !got.Equal(mustTime(t1)) {
		t.Errorf("offline generated = %v; want %s", got, t1)
	}
}

func TestLoadCaptivePortalFallsBackToVerifiedPair(t *testing.T) {
	k := testKeys()
	srv := newServer(t, nil, nil)
	srv.set(signed(t, k[0], indexJSON(t1, "")))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err != nil {
		t.Fatal(err)
	}
	srv.set([]byte("<html>please log in</html>"), []byte("<html>please log in</html>"))
	e.opts.MaxAge = time.Nanosecond
	if got := e.loadedGenerated(t); !got.Equal(mustTime(t1)) {
		t.Errorf("generated = %v; want %s", got, t1)
	}
	if !strings.Contains(e.log.String(), "could not be verified") || !strings.Contains(e.log.String(), "using the package list from") {
		t.Errorf("log = %q", e.log.String())
	}
}

func TestLoadPortalWithoutSavedPairFails(t *testing.T) {
	k := testKeys()
	srv := newServer(t, []byte("<html></html>"), []byte("<html></html>"))
	e := newEnv(t, srv.url(), k[0])
	if err := e.load(); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("got %v", err)
	}
}
