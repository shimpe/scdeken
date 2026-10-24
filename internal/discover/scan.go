package discover

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/run"
	"github.com/sc-deken/sc-deken/internal/scclass"
)

// Scan is what a repository's files tell us about the package inside.
type Scan struct {
	Classes, UGens    []string
	HasQuark          bool
	Quark             QuarkFile
	CMakeDir          string // "" none, "." root, or the subdirectory holding CMakeLists.txt
	SCPathVar         string
	Submodules        bool
	MentionsSupernova bool
	LicenseFile       string      // SPDX guess from LICENSE/COPYING text, or ""
	Kind              recipe.Kind // "" when neither plugin nor quark
}

// scPathVars are the CMake variables that point at the SuperCollider sources,
// in order of preference.
var scPathVars = []string{"SC_PATH", "SC_SRC_PATH", "SUPERCOLLIDER_PATH", "SC_SOURCE_DIR"}

// ScanRepo fetches url at commit into workDir (a shallow fetch, like a
// build), inspects the files and removes workDir again.
func ScanRepo(ctx context.Context, r run.Runner, url, commit, workDir string) (Scan, error) {
	defer os.RemoveAll(workDir)
	if err := fetchCommit(ctx, r, url, commit, workDir); err != nil {
		return Scan{}, err
	}
	s, err := inspect(workDir)
	if err != nil {
		return Scan{}, fmt.Errorf("scan %s: %w", url, err)
	}
	return s, nil
}

// fetchCommit gets exactly one commit; submodules are not needed to scan.
func fetchCommit(ctx context.Context, r run.Runner, url, commit, dir string) error {
	steps := [][]string{
		{"init", dir},
		{"-C", dir, "remote", "add", "origin", url},
		{"-C", dir, "fetch", "--depth", "1", "origin", commit},
		{"-C", dir, "checkout", "--quiet", "FETCH_HEAD"},
	}
	for _, args := range steps {
		if err := r.Run(ctx, run.Cmd{Name: "git", Args: args}); err != nil {
			return fmt.Errorf("fetch %s at %s: %w", url, commit, err)
		}
	}
	return nil
}

// inspect reads an already checked-out tree.
func inspect(dir string) (Scan, error) {
	var s Scan
	classes, err := readClasses(dir)
	if err != nil {
		return s, err
	}
	s.UGens, s.Classes = scclass.Split(classes) // Classes: every class that is not a UGen
	if err := scanQuark(dir, &s); err != nil {
		return s, err
	}
	if err := scanCMake(dir, &s); err != nil {
		return s, err
	}
	_, statErr := os.Stat(filepath.Join(dir, ".gitmodules"))
	s.Submodules = statErr == nil
	s.LicenseFile = readLicense(dir)
	s.Kind = kindOf(s)
	return s, nil
}

// readClasses parses every .sc file in the tree, skipping .git.
func readClasses(dir string) ([]scclass.Class, error) {
	var classes []scclass.Class
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(path) != ".sc" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		classes = append(classes, scclass.Parse(src)...)
		return nil
	})
	return classes, err
}

// scanQuark looks for a *.quark file at the repository root.
func scanQuark(dir string, s *Scan) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*.quark"))
	if err != nil || len(matches) == 0 {
		return err
	}
	sort.Strings(matches)
	src, err := os.ReadFile(matches[0])
	if err != nil {
		return err
	}
	q, err := ParseQuarkFile(string(src))
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(matches[0]), err)
	}
	s.HasQuark, s.Quark = true, q
	return nil
}

// scanCMake finds the CMake project that builds against SuperCollider: the
// root first, then each direct subdirectory in sorted order.
func scanCMake(dir string, s *Scan) error {
	candidates, err := cmakeCandidates(dir)
	if err != nil {
		return err
	}
	for _, rel := range candidates {
		abs := filepath.Join(dir, rel)
		text, err := os.ReadFile(filepath.Join(abs, "CMakeLists.txt"))
		if err != nil {
			continue
		}
		if v := scPathVar(string(text)); v != "" {
			s.CMakeDir, s.SCPathVar = rel, v
			s.MentionsSupernova = mentionsSupernova(abs, string(text))
			return nil
		}
	}
	return nil
}

// cmakeCandidates lists "." followed by the sorted names of subdirectories.
func cmakeCandidates(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	candidates := []string{"."}
	for _, e := range entries {
		if e.IsDir() && e.Name() != ".git" {
			candidates = append(candidates, e.Name())
		}
	}
	return candidates, nil
}

// scPathVar returns the first known SuperCollider path variable the CMake
// text mentions, or "".
func scPathVar(cmake string) string {
	for _, v := range scPathVars {
		if regexp.MustCompile(`\b` + v + `\b`).MatchString(cmake) {
			return v
		}
	}
	return ""
}

// mentionsSupernova checks CMakeLists.txt and cmake/*.cmake for "SUPERNOVA".
func mentionsSupernova(dir, cmakeLists string) bool {
	if strings.Contains(cmakeLists, "SUPERNOVA") {
		return true
	}
	files, _ := filepath.Glob(filepath.Join(dir, "cmake", "*.cmake"))
	for _, f := range files {
		if text, err := os.ReadFile(f); err == nil && strings.Contains(string(text), "SUPERNOVA") {
			return true
		}
	}
	return false
}

// readLicense guesses the license from LICENSE* and COPYING* files at the root.
func readLicense(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if e.IsDir() || !(strings.HasPrefix(name, "LICENSE") || strings.HasPrefix(name, "COPYING")) {
			continue
		}
		if text, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil {
			if id := guessLicense(string(text)); id != "" {
				return id
			}
		}
	}
	return ""
}

// kindOf combines the plugin and quark findings.
func kindOf(s Scan) recipe.Kind {
	switch {
	case s.CMakeDir != "" && s.HasQuark:
		return recipe.KindPluginQuark
	case s.CMakeDir != "":
		return recipe.KindPlugin
	case s.HasQuark:
		return recipe.KindQuark
	}
	return ""
}
