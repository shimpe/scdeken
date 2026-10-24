package scenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPathsUnderAppSupport(t *testing.T) {
	c := Context{
		AppSupportDir: `C:\Users\Stefaan Himpe\AppData\Local\SuperCollider`,
		ExtensionDir:  filepath.Join("ext", "Extensions"),
	}
	p := c.Paths()
	if !strings.HasSuffix(p.State, filepath.Join("sc-deken", "state.json")) {
		t.Errorf("State = %q", p.State)
	}
	if want := filepath.Join(c.AppSupportDir, "sc-deken"); p.Root != want {
		t.Errorf("Root = %q, want %q", p.Root, want)
	}
	for name, got := range map[string]string{
		"Cache": p.Cache, "Catalog": p.Catalog, "Headers": p.Headers, "Work": p.Work, "Quarantine": p.Quarantine,
		"Backup": p.Backup, "TrustedKeys": p.TrustedKeys,
	} {
		if filepath.Dir(got) != p.Root {
			t.Errorf("%s = %q is not directly under Root", name, got)
		}
	}
	if want := filepath.Join(c.ExtensionDir, "sc-deken"); p.Managed != want {
		t.Errorf("Managed = %q, want %q", p.Managed, want)
	}
	if filepath.Base(p.Headers) != "sc-headers" || filepath.Base(p.TrustedKeys) != "trusted-keys.txt" {
		t.Errorf("unexpected names: %q %q", p.Headers, p.TrustedKeys)
	}
}

func TestLoadContext(t *testing.T) {
	const doc = `{
 "sc_version": "3.13.0", "sc_arch": "x86_64",
 "extension_dir": "/e", "system_extension_dir": "/se", "app_support_dir": "/a",
 "include_paths": ["/i1", "/i2"],
 "quarks": [{"name": "Q", "url": "https://x/q", "path": "/p", "commit": "abc"}],
 "osc_port": 57120, "job": "install"
}`
	path := filepath.Join(t.TempDir(), "ctx.json")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadContext(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Context{
		SCVersion: "3.13.0", SCArch: "x86_64", ExtensionDir: "/e", SystemExtensionDir: "/se",
		AppSupportDir: "/a", IncludePaths: []string{"/i1", "/i2"},
		Quarks:  []Quark{{Name: "Q", URL: "https://x/q", Path: "/p", Commit: "abc"}},
		OSCPort: 57120, Job: "install",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestLoadContextErrors(t *testing.T) {
	if _, err := LoadContext(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("missing file: want error")
	}
	path := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(path, []byte("{"), 0o644)
	if _, err := LoadContext(path); err == nil {
		t.Error("bad JSON: want error")
	}
}

func TestVersionAndArch(t *testing.T) {
	c := Context{SCVersion: "3.13.1", SCArch: "arm64"}
	v, err := c.Version()
	if err != nil || v.String() == "" {
		t.Fatalf("Version: %v %v", v, err)
	}
	if a, err := c.Arch(); err != nil || a != "arm64" {
		t.Errorf("Arch = %q, %v", a, err)
	}
	if a, err := (Context{}).Arch(); err != nil || a == "" {
		t.Errorf("host Arch = %q, %v", a, err)
	}
	if _, err := (Context{SCVersion: "bogus"}).Version(); err == nil {
		t.Error("bogus version: want error")
	}
}

func TestDefaultContext(t *testing.T) {
	c := DefaultContext()
	if c.SCVersion != "" || c.SCArch != "" || c.OSCPort != 0 {
		t.Errorf("unexpected fields: %+v", c)
	}
	if c.AppSupportDir == "" || c.SystemExtensionDir == "" {
		t.Errorf("dirs empty: %+v", c)
	}
	if want := filepath.Join(c.AppSupportDir, "Extensions"); c.ExtensionDir != want {
		t.Errorf("ExtensionDir = %q, want %q", c.ExtensionDir, want)
	}
}
