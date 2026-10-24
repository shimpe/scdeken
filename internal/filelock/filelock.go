// Package filelock keeps scdeken processes that run at the same time from
// changing the same files together. A lock is a file that only one process
// can create; one left behind by a process that crashed is taken over once
// it is ten minutes old.
package filelock

import (
	"fmt"
	"os"
	"time"

	"github.com/shimpe/scdeken/internal/robust"
)

// staleAfter is the age at which a lock file is taken to be left over from
// a process that crashed.
const staleAfter = 10 * time.Minute

// pollEvery is how often a waiting Lock tries again.
const pollEvery = 20 * time.Millisecond

// Lock creates the lock file at path, waiting up to wait while another
// process holds it. Call unlock when done. A lock that cannot be removed is
// taken over by the next process once it is stale.
func Lock(path string, wait time.Duration) (unlock func(), err error) {
	deadline := time.Now().Add(wait)
	for {
		err := create(path)
		if err == nil {
			return func() { _ = robust.Remove(path) }, nil
		}
		if removeStale(path) {
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("another scdeken is still changing the same files (lock %s); try again in a moment: %w", path, err)
		}
		time.Sleep(pollEvery)
	}
}

// create makes the lock file, failing when it already exists. It holds the
// process id, to help whoever finds a stale one.
func create(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintf(f, "%d\n", os.Getpid())
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// removeStale deletes the lock at path when it is older than staleAfter and
// reports whether it did.
func removeStale(path string) bool {
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) < staleAfter {
		return false
	}
	return os.Remove(path) == nil
}
