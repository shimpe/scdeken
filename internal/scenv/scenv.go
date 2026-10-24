// Package scenv describes the user's SuperCollider session as reported by
// sclang, and derives every folder sc-deken uses from it.
package scenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/version"
)

// Quark is one installed quark as listed by sclang.
type Quark struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Path   string `json:"path"`
	Commit string `json:"commit"`
}

// Context is what sclang tells the helper, written by the quark as JSON.
type Context struct {
	SCVersion          string   `json:"sc_version"` // Main.version
	SCArch             string   `json:"sc_arch"`    // Platform.architecture
	ExtensionDir       string   `json:"extension_dir"`
	SystemExtensionDir string   `json:"system_extension_dir"`
	AppSupportDir      string   `json:"app_support_dir"` // Platform.userAppSupportDir
	IncludePaths       []string `json:"include_paths"`   // LanguageConfig.includePaths
	Quarks             []Quark  `json:"quarks"`          // Quarks.installed
	OSCPort            int      `json:"osc_port"`        // NetAddr.langPort; 0 = none
	Job                string   `json:"job"`
}

// LoadContext reads a context JSON file written by sclang.
func LoadContext(path string) (Context, error) {
	var c Context
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read session context %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Context{}, fmt.Errorf("parse session context %s: %w", path, err)
	}
	return c, nil
}

// DefaultContext is used when the CLI runs without sclang: it points at the
// operating system's usual SuperCollider folders.
func DefaultContext() Context {
	appSupport, system := defaultDirs(platform.HostOS())
	return Context{
		AppSupportDir:      appSupport,
		ExtensionDir:       filepath.Join(appSupport, "Extensions"),
		SystemExtensionDir: system,
	}
}

// defaultDirs returns the per-user SuperCollider folder and the system
// extension folder for the given OS.
func defaultDirs(goos string) (appSupport, system string) {
	home, _ := os.UserHomeDir()
	switch goos {
	case "macos":
		return filepath.Join(home, "Library", "Application Support", "SuperCollider"),
			"/Library/Application Support/SuperCollider/Extensions"
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "SuperCollider"),
			filepath.Join(os.Getenv("ProgramData"), "SuperCollider", "Extensions")
	default:
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(data, "SuperCollider"), "/usr/share/SuperCollider/Extensions"
	}
}

// Paths holds every folder and file sc-deken owns.
type Paths struct {
	Root, Cache, Catalog, Headers, Work, Quarantine, Backup, State, TrustedKeys, Managed string
}

// Paths derives sc-deken's folders from the session's directories.
func (c Context) Paths() Paths {
	root := filepath.Join(c.AppSupportDir, "sc-deken")
	return Paths{
		Root:        root,
		Cache:       filepath.Join(root, "cache"),
		Catalog:     filepath.Join(root, "catalog"),
		Headers:     filepath.Join(root, "sc-headers"),
		Work:        filepath.Join(root, "work"),
		Quarantine:  filepath.Join(root, "quarantine"),
		Backup:      filepath.Join(root, "backup"),
		State:       filepath.Join(root, "state.json"),
		TrustedKeys: filepath.Join(root, "trusted-keys.txt"),
		Managed:     filepath.Join(c.ExtensionDir, "sc-deken"),
	}
}

// Version parses the SuperCollider version sclang reported.
func (c Context) Version() (version.Version, error) {
	v, err := version.Parse(c.SCVersion)
	if err != nil {
		return version.Version{}, fmt.Errorf("SuperCollider version %q: %w", c.SCVersion, err)
	}
	return v, nil
}

// Arch returns the normalized CPU architecture, falling back to the host's
// when sclang did not report one.
func (c Context) Arch() (string, error) {
	raw := c.SCArch
	if raw == "" {
		raw = runtime.GOARCH
	}
	arch, err := platform.NormalizeArch(raw)
	if err != nil {
		return "", fmt.Errorf("CPU architecture %q: %w", raw, err)
	}
	return arch, nil
}
