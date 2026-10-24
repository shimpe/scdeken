//go:build windows

package install

import (
	"errors"
	"io/fs"
	"syscall"
)

// errorSharingViolation is Windows' ERROR_SHARING_VIOLATION: another process
// (usually the SuperCollider server) has the file open.
const errorSharingViolation = syscall.Errno(32)

// isInUse reports whether err means a file is locked by another program.
func isInUse(err error) bool {
	return errors.Is(err, errorSharingViolation) || errors.Is(err, fs.ErrPermission)
}
