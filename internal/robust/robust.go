// Package robust renames, removes and writes files in ways that survive the
// brief locks Windows virus scanners and indexers put on fresh files, and
// that never leave half a file behind. It follows cmd/go's robustio.
package robust

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// retryFor is how long an operation keeps trying while a file is briefly
// locked.
var retryFor = 2 * time.Second

// The operations being retried; tests replace them.
var (
	osRename = os.Rename
	osRemove = os.Remove
)

// Rename is os.Rename, retried for up to two seconds while Windows reports
// the file as briefly locked (access denied or sharing violation). Other
// errors, and a lock that lasts, are returned as they are.
func Rename(from, to string) error {
	return retry(func() error { return osRename(from, to) })
}

// Remove is os.Remove, retried like Rename.
func Remove(path string) error {
	return retry(func() error { return osRemove(path) })
}

// WriteFile writes data to a new temporary file next to path, flushes it to
// disk and renames it over path, so readers see either the old file or the
// whole new one. Every call gets its own temporary name, so two processes
// writing the same file never share one.
func WriteFile(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	err = writeSynced(tmp, data, perm)
	if err == nil {
		err = Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// writeSynced writes data to f, flushes it to disk, closes it and gives it
// the permissions perm.
func writeSynced(f *os.File, data []byte, perm fs.FileMode) error {
	_, werr := f.Write(data)
	serr := f.Sync()
	if err := errors.Join(werr, serr, f.Close()); err != nil {
		return err
	}
	return os.Chmod(f.Name(), perm)
}

// retry runs op until it succeeds, fails for a reason other than a brief
// lock, or retryFor has passed.
func retry(op func() error) error {
	deadline := time.Now().Add(retryFor)
	pause := time.Millisecond
	for {
		err := op()
		if err == nil || !isBriefLock(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(pause)
		pause = min(2*pause, 100*time.Millisecond)
	}
}
