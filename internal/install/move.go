package install

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// moveTree moves a file or folder. When the destination is on another drive
// it copies the tree and then removes the source; if the copy fails, the
// source is left untouched and the partial copy is removed.
func moveTree(src, dst string) error {
	err := rename(src, dst)
	if err == nil || !isCrossDevice(err) {
		return err
	}
	if err := copyTree(src, dst); err != nil {
		os.RemoveAll(dst)
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	return os.RemoveAll(src)
}

// copyTree copies a file, folder or link, keeping permissions.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		return copyEntry(path, filepath.Join(dst, rel), d)
	})
}

// copyEntry copies one entry found by copyTree.
func copyEntry(src, dst string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return err
	}
	switch {
	case d.IsDir():
		return os.MkdirAll(dst, info.Mode().Perm()|0o700)
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.Mode().IsRegular():
		return copyFile(src, dst, info.Mode().Perm())
	}
	return fmt.Errorf("%s is not a regular file", src)
}

func copyFile(src, dst string, perm fs.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
