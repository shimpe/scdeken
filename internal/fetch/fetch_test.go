package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func TestGetUsesETag(t *testing.T) {
	var requests, conditional int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			atomic.AddInt32(&conditional, 1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	c := New(t.TempDir())
	for i := 0; i < 2; i++ {
		got, err := c.Get(context.Background(), srv.URL)
		if err != nil || string(got) != "hello" {
			t.Fatalf("call %d: got %q, %v", i, got, err)
		}
	}
	if requests != 2 || conditional != 1 {
		t.Errorf("requests=%d conditional=%d, want 2 and 1", requests, conditional)
	}
	body, when, ok := c.Cached(srv.URL)
	if !ok || string(body) != "hello" || when.IsZero() {
		t.Errorf("Cached = %q, %v, %v", body, when, ok)
	}
}

func TestCachedMissing(t *testing.T) {
	if _, _, ok := New(t.TempDir()).Cached("http://example.invalid/x"); ok {
		t.Error("Cached reported a hit for an unseen URL")
	}
}

func TestGetHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := New(t.TempDir()).Get(context.Background(), srv.URL+"/index.json")
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), srv.URL+"/index.json") {
		t.Fatalf("err = %v, want status and URL", err)
	}
}

func serveBody(body []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
}

func TestDownloadVerifiesHash(t *testing.T) {
	body := []byte("archive bytes")
	srv := serveBody(body)
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "a.zip")
	if err := New(t.TempDir()).Download(context.Background(), srv.URL, dst, sum(body), nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(body) {
		t.Fatalf("dst = %q, %v", got, err)
	}
}

func TestDownloadHashMismatchLeavesNothing(t *testing.T) {
	srv := serveBody([]byte("archive bytes"))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "a.zip")
	err := New(t.TempDir()).Download(context.Background(), srv.URL, dst, sum([]byte("other")), nil)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want checksum error", err)
	}
	for _, p := range []string{dst, dst + ".part"} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s should not exist", p)
		}
	}
}

func TestDownloadReportsProgress(t *testing.T) {
	body := []byte(strings.Repeat("x", 100000))
	srv := serveBody(body)
	defer srv.Close()
	var done, total int64
	progress := func(d, t int64) { done, total = d, t }
	dst := filepath.Join(t.TempDir(), "a.zip")
	if err := New(t.TempDir()).Download(context.Background(), srv.URL, dst, sum(body), progress); err != nil {
		t.Fatal(err)
	}
	if done != int64(len(body)) || total != int64(len(body)) {
		t.Errorf("progress done=%d total=%d, want %d", done, total, len(body))
	}
}

func TestGetFileURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := New(t.TempDir()).Get(context.Background(), fileURL(path))
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDownloadFileURL(t *testing.T) {
	body := []byte("local archive")
	src := filepath.Join(t.TempDir(), "src.zip")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "dst.zip")
	if err := New(t.TempDir()).Download(context.Background(), fileURL(src), dst, sum(body), nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != string(body) {
		t.Errorf("dst = %q", got)
	}
}

func TestFailedBodyWriteLeavesNoETag(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("If-None-Match"))
		if len(seen) == 1 {
			w.Header().Set("ETag", `"v1"`)
			_, _ = w.Write([]byte("old"))
			return
		}
		w.Header().Set("ETag", `"v2"`)
		_, _ = w.Write([]byte("new"))
	}))
	defer srv.Close()

	c := New(t.TempDir())
	if _, err := c.Get(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	// A directory where the temp file belongs makes the body write fail.
	if err := os.Mkdir(c.path(srv.URL, "body")+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := c.Get(context.Background(), srv.URL); err != nil || string(got) != "new" {
		t.Fatalf("second Get = %q, %v", got, err)
	}
	if _, err := c.Get(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 3 || seen[1] != `"v1"` || seen[2] != "" {
		t.Errorf("If-None-Match per request = %q, want [\"\" \"v1\" \"\"]", seen)
	}
}
