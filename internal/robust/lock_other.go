//go:build !windows

package robust

// isBriefLock reports whether err may only mean that another program has the
// file open for a moment. Outside Windows an open file never blocks a rename
// or removal, so no error is.
func isBriefLock(error) bool { return false }
