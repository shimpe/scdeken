package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrInUse marks a package folder that cannot be replaced because
// SuperCollider has files in it open.
var ErrInUse = errors.New("in use")

// rename moves files; tests replace it to simulate locked files.
var rename = os.Rename

// inUseError carries the musician-facing message and matches ErrInUse.
type inUseError struct {
	msg   string
	cause error
}

func (e inUseError) Error() string        { return e.msg }
func (e inUseError) Is(target error) bool { return target == ErrInUse }
func (e inUseError) Unwrap() error        { return e.cause }

// swapError explains a failed rename of the folder of package name.
func swapError(name string, err error) error {
	if isInUse(err) {
		return inUseError{
			msg:   fmt.Sprintf("Can't replace %s while SuperCollider is using it. Quit the server (s.quit) and try again.", name),
			cause: err,
		}
	}
	return fmt.Errorf("replace %s: %w", name, err)
}

// swapIn puts the finished staging folder in place of target. The previous
// copy is kept as ".old-<name>" until the new one is in place and is put
// back if anything goes wrong, so target is never left half-installed.
func swapIn(staging, target string) error {
	name := filepath.Base(target)
	old := filepath.Join(filepath.Dir(target), ".old-"+name)
	if err := os.RemoveAll(old); err != nil {
		return fmt.Errorf("clear leftover %s: %w", old, err)
	}
	hadOld, err := moveAside(target, old)
	if err != nil {
		return swapError(name, err)
	}
	if err := rename(staging, target); err != nil {
		if hadOld {
			_ = rename(old, target)
		}
		return swapError(name, err)
	}
	if hadOld {
		_ = os.RemoveAll(old) // only a leftover; the next install clears it too
	}
	return nil
}

// moveAside renames target to old when target exists.
func moveAside(target, old string) (moved bool, err error) {
	if _, err := os.Lstat(target); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, rename(target, old)
}
