package install

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/scclass"
)

// FindDuplicates looks in roots for a copy of a package that was not
// installed by sc-deken: a file named like one of pluginFiles, or a .sc file
// defining one of classes. Folders in skip are not searched. It reports one
// Duplicate per top folder under a root (or the file itself when it sits
// directly in a root), sorted by path. Missing roots and unreadable files
// are ignored, and links are not followed.
func FindDuplicates(roots []string, skip []string, pluginFiles, classes []string) ([]Duplicate, error) {
	return scanDuplicates(roots, skip, "", pluginFiles, classes)
}

// scanDuplicates is FindDuplicates for a known managed folder: a match inside
// it is reported as the package folder below it, never as the whole tree.
func scanDuplicates(roots, skip []string, managed string, pluginFiles, classes []string) ([]Duplicate, error) {
	f := finder{
		managed: managed,
		skip:    skip,
		plugins: map[string]string{},
		classes: map[string]bool{},
		found:   map[string]Duplicate{},
	}
	for _, name := range pluginFiles {
		f.plugins[strings.ToLower(name)] = name
	}
	for _, name := range classes {
		f.classes[name] = true
	}
	for _, root := range roots {
		f.scan(root)
	}
	return f.result(), nil
}

// finder holds what to look for and what has been found so far.
type finder struct {
	managed string // sc-deken's own folder; "" when unknown
	skip    []string
	plugins map[string]string // lowercase file name -> name as listed
	classes map[string]bool
	found   map[string]Duplicate // by Path
}

// scan walks one root, recording matches.
func (f *finder) scan(root string) {
	if root == "" {
		return
	}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return skipEntry(d)
		case d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0:
			return nil // never follow links or junctions
		case d.IsDir():
			if d.Name() == ".git" || f.skipped(path) {
				return fs.SkipDir
			}
			return nil
		}
		if matched := f.match(path); matched != "" {
			f.record(root, path, matched)
		}
		return nil
	})
}

// skipEntry tells WalkDir to carry on past an entry it could not read.
func skipEntry(d fs.DirEntry) error {
	if d != nil && d.IsDir() {
		return fs.SkipDir
	}
	return nil
}

// skipped reports whether path is one of the folders not to search.
func (f *finder) skipped(path string) bool {
	for _, s := range f.skip {
		if s != "" && strings.EqualFold(filepath.Clean(s), filepath.Clean(path)) {
			return true
		}
	}
	return false
}

// match returns what file path matches (a plugin file name or a class
// name), or "" when it is unrelated.
func (f *finder) match(path string) string {
	base := filepath.Base(path)
	if name, ok := f.plugins[strings.ToLower(base)]; ok {
		return name
	}
	if len(f.classes) == 0 || !strings.EqualFold(filepath.Ext(base), ".sc") {
		return ""
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, c := range scclass.Parse(src) {
		if f.classes[c.Name] {
			return c.Name
		}
	}
	return ""
}

// record keeps the first match found under each top folder of root.
func (f *finder) record(root, path, matched string) {
	if inside(f.managed, path) {
		root = f.managed
	}
	top := topFolder(root, path)
	if _, seen := f.found[top]; !seen {
		f.found[top] = Duplicate{Path: top, Matched: matched}
	}
}

// inside reports whether path lies below dir.
func inside(dir, path string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// topFolder is the first folder below root on the way to path, or path
// itself when it sits directly in root.
func topFolder(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	first, _, nested := strings.Cut(rel, string(filepath.Separator))
	if !nested {
		return path
	}
	return filepath.Join(root, first)
}

// result lists the findings sorted by path.
func (f *finder) result() []Duplicate {
	var out []Duplicate
	for _, d := range f.found {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// findDuplicates searches the user's extension folders for a copy of pkg
// that sc-deken did not install.
func (s *Service) findDuplicates(pkg *index.Package) ([]Duplicate, error) {
	roots := append([]string{s.Ctx.ExtensionDir, s.Ctx.SystemExtensionDir}, s.Ctx.IncludePaths...)
	classes := append(append([]string{}, pkg.Classes...), pkg.UGens...)
	return scanDuplicates(roots, s.skipFolders(pkg), s.Paths.Managed, pkg.PluginFiles, classes)
}

// skipFolders are the folders holding pkg's own, sc-deken-managed files.
func (s *Service) skipFolders(pkg *index.Package) []string {
	var skip []string
	if s.Paths.Managed != "" {
		skip = append(skip, filepath.Join(s.Paths.Managed, pkg.Name))
	}
	repo := advisory.NormalizeRepo(pkg.Repo)
	for _, q := range s.Ctx.Quarks {
		if advisory.NormalizeRepo(q.URL) == repo {
			skip = append(skip, q.Path)
		}
	}
	return skip
}
