package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchTopicPaginates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.Header.Get("User-Agent") != "sc-deken" {
			t.Errorf("bad headers: %v", r.Header)
		}
		if r.URL.Query().Get("q") != "topic:supercollider" || r.URL.Query().Get("per_page") != "100" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/search/repositories?q=topic:supercollider&per_page=100&page=2>; rel="next"`, srv.URL))
			fmt.Fprint(w, `{"items":[{"full_name":"a/one","license":{"spdx_id":"MIT"},"stargazers_count":3,"archived":true}]}`)
			return
		}
		fmt.Fprint(w, `{"items":[{"full_name":"b/two","license":{"spdx_id":"NOASSERTION"},"pushed_at":"2026-01-02T03:04:05Z"}]}`)
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Token: "tok"}
	repos, err := c.SearchTopic(context.Background(), "supercollider")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].FullName != "a/one" || repos[0].License != "MIT" || repos[0].Stars != 3 || !repos[0].Archived {
		t.Fatalf("repos = %+v", repos)
	}
	if repos[1].License != "" || repos[1].PushedAt.Year() != 2026 {
		t.Fatalf("second = %+v", repos[1])
	}
}

func TestReleases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/a/one/releases" {
			t.Errorf("path = %s", r.URL.Path)
		}
		fmt.Fprint(w, `[{"tag_name":"v1","assets":[{"name":"x.zip","browser_download_url":"https://e/x.zip","size":42}]}]`)
	}))
	defer srv.Close()
	rels, err := (&Client{BaseURL: srv.URL}).Releases(context.Background(), "a/one")
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 1 || rels[0].Tag != "v1" || rels[0].Assets[0] != (Asset{"x.zip", "https://e/x.zip", 42}) {
		t.Fatalf("rels = %+v", rels)
	}
}

func TestRepo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"full_name":"a/one","default_branch":"main","license":null}`)
	}))
	defer srv.Close()
	r, err := (&Client{BaseURL: srv.URL}).Repo(context.Background(), "a/one")
	if err != nil || r.DefaultBranch != "main" || r.License != "" {
		t.Fatalf("r = %+v, err = %v", r, err)
	}
}

func TestRateLimitHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL}).Repo(context.Background(), "a/one")
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchTopicRecordsTopics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"full_name":"a/one","topics":["supercollider-quark","audio"]},{"full_name":"b/two"}]}`)
	}))
	defer srv.Close()
	repos, err := (&Client{BaseURL: srv.URL}).SearchTopic(context.Background(), "supercollider-quark")
	if err != nil || len(repos) != 2 {
		t.Fatalf("%v %v", repos, err)
	}
	if got := strings.Join(repos[0].Topics, ","); got != "supercollider-quark,audio" {
		t.Errorf("topics from the API = %q", got)
	}
	if got := strings.Join(repos[1].Topics, ","); got != "supercollider-quark" {
		t.Errorf("topic from the search = %q", got)
	}
}
