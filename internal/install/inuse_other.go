//go:build !windows

package install

import (
	"errors"
	"io/fs"
)

// isInUse reports whether err means a file may not be touched right now.
func isInUse(err error) bool {
	return errors.Is(err, fs.ErrPermission)
}
