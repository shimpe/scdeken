package install

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shimpe/scdeken/internal/platform"
)

// Copies moved aside (backup, quarantine) must never load again. Two
// defences: the folder they go to must not lie inside a folder sclang
// compiles or scsynth loads plugins from (checked before anything moves),
// and the files sclang and scsynth would pick up are renamed with
// disabledSuffix once moved, which covers search paths sc-deken can't see
// (such as the server's ugenPluginsPath).

// disabledSuffix is appended to every loadable file of a moved-aside copy.
const disabledSuffix = ".disabled"

// loadable is true for the files sclang compiles (.sc) and scsynth loads as
// plugins (.scx; .so on Linux).
func loadable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sc", ".scx", ".so":
		return true
	}
	return false
}

// searchPath is a folder SuperCollider loads from, and the setting behind it.
type searchPath struct{ dir, setting string }

func (s *Service) searchPaths() []searchPath {
	paths := []searchPath{
		{s.Ctx.ExtensionDir, "Platform.userExtensionDir"},
		{s.Ctx.SystemExtensionDir, "Platform.systemExtensionDir"},
	}
	for _, p := range s.Ctx.IncludePaths {
		paths = append(paths, searchPath{p, "an include path in LanguageConfig"})
	}
	return paths
}

// checkOutsideSearchPaths refuses when dir (the backup or quarantine folder,
// named by what) lies inside a folder SuperCollider loads from.
func (s *Service) checkOutsideSearchPaths(dir, what string) error {
	for _, p := range s.searchPaths() {
		if within(dir, p.dir) {
			return fmt.Errorf("Nothing was moved: the %s folder %s is inside %s (%s), so SuperCollider would load what sc-deken puts there. "+
				"Change that setting so it no longer contains %s, then try again.", what, dir, p.dir, p.setting, dir)
		}
	}
	return nil
}

// within reports whether child is parent or lies inside it. Links are
// followed as far as the paths exist; an empty parent contains nothing.
func within(child, parent string) bool {
	if parent == "" {
		return false
	}
	c, p := resolved(child), resolved(parent)
	if goos := platform.HostOS(); goos == "windows" || goos == "macos" {
		c, p = strings.ToLower(c), strings.ToLower(p)
	}
	rel, err := filepath.Rel(p, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolved is path with links followed in the part that exists.
func resolved(path string) string {
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolved(parent), filepath.Base(path))
}

// disableTree renames every loadable file in root (a folder or one file)
// to <name>.disabled.
func disableTree(root string) error {
	return renameLoadable(root, func(name string) (string, bool) {
		return name + disabledSuffix, loadable(name)
	})
}

// enableTree undoes disableTree.
func enableTree(root string) error {
	return renameLoadable(root, func(name string) (string, bool) {
		orig := strings.TrimSuffix(name, disabledSuffix)
		return orig, orig != name && loadable(orig)
	})
}

// renameLoadable renames each file in root for which to says so. It never
// replaces an existing file.
func renameLoadable(root string, to func(name string) (string, bool)) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name, ok := to(d.Name())
		if !ok {
			return nil
		}
		dst := filepath.Join(filepath.Dir(path), name)
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("rename %s: %s already exists", path, name)
		}
		if err := rename(path, dst); err != nil {
			return fmt.Errorf("rename %s: %w", path, err)
		}
		return nil
	})
}
