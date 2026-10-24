//go:build !windows

package install

import (
	"errors"
	"io/fs"
	"syscall"
)

// isInUse reports whether err means a file may not be touched right now.
func isInUse(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}

// isCrossDevice reports whether err means src and dst are on different
// file systems.
func isCrossDevice(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}
