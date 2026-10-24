package discover

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
)

func TestRemoteRefsPeeled(t *testing.T) {
	f := &run.Fake{Script: map[string]run.FakeResult{
		"git ls-remote https://x/y HEAD refs/tags/*": {Out: "aaa\tHEAD\nbbb\trefs/tags/v1.0\nccc\trefs/tags/v1.0^{}\nddd\trefs/tags/v2.0\n"},
	}}
	head, tags, err := RemoteRefs(context.Background(), f, "https://x/y")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"v1.0": "ccc", "v2.0": "ddd"}
	if head != "aaa" || !reflect.DeepEqual(tags, want) {
		t.Fatalf("head = %q, tags = %v", head, tags)
	}
}

func TestRemoteRefsPeeledBeforeTag(t *testing.T) {
	f := &run.Fake{Script: map[string]run.FakeResult{
		"git ls-remote u HEAD refs/tags/*": {Out: "ccc\trefs/tags/v1^{}\nbbb\trefs/tags/v1"},
	}}
	_, tags, err := RemoteRefs(context.Background(), f, "u")
	if err != nil || tags["v1"] != "ccc" {
		t.Fatalf("tags = %v, err = %v", tags, err)
	}
}

func TestParseDirectory(t *testing.T) {
	txt := "Foo=https://github.com/a/foo\n\n#x\n  Bar = https://github.com/b/bar@tags/1.0 \nSsh=git@github.com:c/d@v1\nHost=https://example.org"
	got := ParseDirectory(txt)
	want := []Entry{
		{"Foo", "https://github.com/a/foo"},
		{"Bar", "https://github.com/b/bar"},
		{"Ssh", "git@github.com:c/d"},
		{"Host", "https://example.org"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseQuarkFile(t *testing.T) {
	src := `( name: "Panola", summary: "Notation \"quoted\"", version: "1.1", dependencies: ["TemporalDict", "https://github.com/x/y", \Sym, 'Other'] )`
	q, err := ParseQuarkFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := QuarkFile{Name: "Panola", Summary: `Notation "quoted"`, Version: "1.1",
		Dependencies: []Dependency{{Name: "TemporalDict"}, {Name: "y", URL: "https://github.com/x/y"}, {Name: "Sym"}, {Name: "Other"}}}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("got %+v", q)
	}
}

func TestParseQuarkFileLenient(t *testing.T) {
	src := "(\n// comment with name: \"no\"\n  summary: \"S\",\n  schelp: \"x\", author: (a: [1, 2]), \n)"
	q, err := ParseQuarkFile(src)
	if err != nil || q.Summary != "S" || q.Name != "" {
		t.Fatalf("q = %+v, err = %v", q, err)
	}
}

func TestParseQuarkFileMalformed(t *testing.T) {
	for _, src := range []string{"{ this is not a quark", "", `( name: "open`, `( name: "x"`, `( dependencies: ["a" )`} {
		if _, err := ParseQuarkFile(src); err == nil {
			t.Errorf("%q: expected an error", src)
		}
	}
}

// fetchFake answers git fetch steps and writes files into the work dir on checkout.
func fetchFake(files map[string]string) *run.Fake {
	return &run.Fake{
		Script: map[string]run.FakeResult{"git *": {}},
		OnRun: func(c run.Cmd) error {
			if c.Args[0] != "-C" || c.Args[2] != "checkout" {
				return nil
			}
			for name, text := range files {
				path := filepath.Join(c.Args[1], filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func scan(t *testing.T, files map[string]string) (Scan, string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "work")
	s, err := ScanRepo(context.Background(), fetchFake(files), "https://x/y", "abc123", work)
	if err != nil {
		t.Fatal(err)
	}
	return s, work
}

func TestScanPluginRepo(t *testing.T) {
	s, work := scan(t, map[string]string{
		"CMakeLists.txt": "include_directories(${SC_PATH}/include)\noption(SUPERNOVA \"x\" ON)\n",
		"classes/Foo.sc": "Foo : UGen {}\nBar { }\n",
		".gitmodules":    "[submodule \"a\"]",
	})
	if s.Kind != recipe.KindPlugin || s.CMakeDir != "." || s.SCPathVar != "SC_PATH" || !s.MentionsSupernova || !s.Submodules {
		t.Fatalf("scan = %+v", s)
	}
	if !reflect.DeepEqual(s.UGens, []string{"Foo"}) || !reflect.DeepEqual(s.Classes, []string{"Bar"}) {
		t.Fatalf("classes = %v, ugens = %v", s.Classes, s.UGens)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("workDir not removed: %v", err)
	}
}

func TestScanSubdirAndQuark(t *testing.T) {
	s, _ := scan(t, map[string]string{
		"Bar.quark":                         `( name: "Bar", version: "0.1" )`,
		"aaa/CMakeLists.txt":                "project(unrelated)",
		"supercollider/CMakeLists.txt":      "set(X ${SC_SRC_PATH})",
		"supercollider/cmake/Options.cmake": "if(SUPERNOVA)",
	})
	if s.Kind != recipe.KindPluginQuark || s.CMakeDir != "supercollider" || s.SCPathVar != "SC_SRC_PATH" {
		t.Fatalf("scan = %+v", s)
	}
	if !s.HasQuark || s.Quark.Name != "Bar" || !s.MentionsSupernova {
		t.Fatalf("scan = %+v", s)
	}
}

func TestScanQuarkOnlyAndNothing(t *testing.T) {
	s, _ := scan(t, map[string]string{"X.quark": `( name: "X" )`})
	if s.Kind != recipe.KindQuark {
		t.Fatalf("kind = %q", s.Kind)
	}
	s, _ = scan(t, map[string]string{"README.md": "hi"})
	if s.Kind != "" || s.CMakeDir != "" {
		t.Fatalf("scan = %+v", s)
	}
}

func TestScanRemovesWorkDirOnError(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work")
	os.MkdirAll(work, 0o755)
	f := &run.Fake{} // every command is unscripted, so fetch fails
	if _, err := ScanRepo(context.Background(), f, "u", "c", work); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("workDir not removed: %v", err)
	}
}

func TestScanLicense(t *testing.T) {
	cases := map[string]string{
		"GNU GENERAL PUBLIC LICENSE\n Version 3, 29 June 2007":                      "GPL-3.0",
		"GNU GENERAL PUBLIC LICENSE\r\n Version 2, June 1991":                       "GPL-2.0",
		"GNU LESSER GENERAL PUBLIC LICENSE\n Version 2.1, February 1999":            "LGPL-2.1",
		"MIT License\n\nCopyright":                                                  "MIT",
		"Apache License\n Version 2.0, January 2004":                                "Apache-2.0",
		"Redistribution and use in source and binary forms ... Neither the name of": "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with two clauses":       "",
		"all rights reserved": "",
	}
	for text, want := range cases {
		s, _ := scan(t, map[string]string{"LICENSE": text})
		if s.LicenseFile != want {
			t.Errorf("%q: got %q, want %q", text, s.LicenseFile, want)
		}
	}
	s, _ := scan(t, map[string]string{"COPYING.txt": "MIT License"})
	if s.LicenseFile != "MIT" || strings.Contains(s.LicenseFile, " ") {
		t.Fatalf("COPYING not read: %q", s.LicenseFile)
	}
}

func TestScanMalformedQuarkKeepsFindings(t *testing.T) {
	s, _ := scan(t, map[string]string{
		"Bad.quark":      "{ not a quark",
		"CMakeLists.txt": "include(${SC_PATH})",
		"Foo.sc":         "Foo : UGen {}",
	})
	if s.Kind != recipe.KindPluginQuark || !s.HasQuark || s.Quark.Name != "" || s.QuarkError == "" {
		t.Fatalf("scan = %+v", s)
	}
	if !reflect.DeepEqual(s.UGens, []string{"Foo"}) {
		t.Fatalf("ugens = %v", s.UGens)
	}
}

func TestParseQuarkFileLeadingComments(t *testing.T) {
	for _, src := range []string{
		"// my quark (v2)\n( name: \"X\" )",
		"/* header (old) ) */\n( name: \"X\" )",
	} {
		q, err := ParseQuarkFile(src)
		if err != nil || q.Name != "X" {
			t.Errorf("%q: q = %+v, err = %v", src, q, err)
		}
	}
}
