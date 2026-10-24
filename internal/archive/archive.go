// Package archive packs and unpacks zip files safely. Unpacking refuses
// anything that could write outside the target folder (zip-slip).
package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fixedTime is stamped on every entry so identical trees give identical zips.
var fixedTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Zip packs the regular files and directories under srcDir into the zip file
// dst. Names are relative, slash-separated and sorted; times are fixed, so the
// output is reproducible. Symlinks and other special files are an error.
func Zip(srcDir, dst string) (err error) {
	names, err := listTree(srcDir)
	if err != nil {
		return fmt.Errorf("zip %s: %w", srcDir, err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("zip %s: %w", srcDir, err)
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(dst)
			err = fmt.Errorf("zip %s: %w", srcDir, err)
		}
	}()
	zw := zip.NewWriter(out)
	for _, name := range names {
		if err := addEntry(zw, srcDir, name); err != nil {
			return err
		}
	}
	return zw.Close()
}

// listTree returns the sorted slash-separated names of everything under root.
func listTree(root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file or folder", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			name += "/"
		}
		names = append(names, name)
		return nil
	})
	sort.Strings(names)
	return names, err
}

// addEntry writes one directory or file entry to zw.
func addEntry(zw *zip.Writer, srcDir, name string) error {
	hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: fixedTime}
	if strings.HasSuffix(name, "/") {
		hdr.Method = zip.Store
		hdr.SetMode(fs.ModeDir | 0o755)
		_, err := zw.CreateHeader(hdr)
		return err
	}
	hdr.SetMode(0o644)
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := os.Open(filepath.Join(srcDir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// Unzip unpacks the zip file src into dstDir, which must not exist yet.
// Every entry name is checked before anything is written. If any entry is
// refused, or unpacking fails midway, dstDir is removed again.
func Unzip(src, dstDir string) (err error) {
	if _, statErr := os.Lstat(dstDir); statErr == nil {
		return fmt.Errorf("unzip %s: %s already exists", src, dstDir)
	}
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("unzip %s: %w", src, err)
	}
	defer zr.Close()
	if err := checkEntries(zr.File); err != nil {
		return fmt.Errorf("unzip %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dstDir), 0o755); err != nil {
		return fmt.Errorf("unzip %s: %w", src, err)
	}
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		return fmt.Errorf("unzip %s: %w", src, err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dstDir)
			err = fmt.Errorf("unzip %s: %w", src, err)
		}
	}()
	for _, f := range zr.File {
		if err := extract(f, dstDir); err != nil {
			return fmt.Errorf("entry %q: %w", f.Name, err)
		}
	}
	return nil
}

// checkEntries refuses the whole archive if any entry is unsafe or duplicated.
func checkEntries(files []*zip.File) error {
	seen := make(map[string]bool)
	for _, f := range files {
		if err := checkName(f.Name); err != nil {
			return fmt.Errorf("entry %q refused: %w", f.Name, err)
		}
		if t := f.Mode().Type(); t != 0 && t != fs.ModeDir {
			return fmt.Errorf("entry %q refused: links and special files are not allowed", f.Name)
		}
		key := strings.TrimSuffix(f.Name, "/")
		if seen[key] {
			return fmt.Errorf("entry %q refused: listed twice", f.Name)
		}
		seen[key] = true
	}
	return nil
}

// checkName accepts only plain relative slash-separated names.
func checkName(name string) error {
	if strings.ContainsAny(name, `\:`) {
		return fmt.Errorf("backslash or drive letter in name")
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf("absolute path")
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("empty, '.' or '..' path element")
		}
	}
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		return fmt.Errorf("path leaves the target folder")
	}
	return nil
}

// extract writes one already-checked entry below dstDir.
func extract(f *zip.File, dstDir string) error {
	target := filepath.Join(dstDir, filepath.FromSlash(f.Name))
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
