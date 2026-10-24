//go:build windows

package robust

import (
	"errors"
	"syscall"
)

// Windows error codes for a file another program has open right now.
const (
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
)

// isBriefLock reports whether err may only mean that another program (such
// as a virus scanner) has the file open for a moment.
func isBriefLock(err error) bool {
	return errors.Is(err, errorAccessDenied) || errors.Is(err, errorSharingViolation)
}
