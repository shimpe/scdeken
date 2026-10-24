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

// errorNotSameDevice is Windows' ERROR_NOT_SAME_DEVICE: a rename between drives.
const errorNotSameDevice = syscall.Errno(17)

// isCrossDevice reports whether err means src and dst are on different drives.
func isCrossDevice(err error) bool {
	return errors.Is(err, errorNotSameDevice)
}
