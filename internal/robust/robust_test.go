package robust

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// sharingViolation is what Windows reports while a virus scanner or indexer
// briefly holds a fresh file open.
var sharingViolation = &os.LinkError{Op: "rename", Err: syscall.Errno(32)}

// flakyRename replaces the real rename with one that fails `failures` times
// with err before it succeeds, and counts the calls.
func flakyRename(t *testing.T, failures int, err error) *int {
	t.Helper()
	calls := 0
	real := osRename
	osRename = func(_, _ string) error {
		calls++
		if calls <= failures {
			return err
		}
		return nil
	}
	t.Cleanup(func() { osRename = real })
	return &calls
}

func TestRenameRetriesWhileBrieflyLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("brief locks are a Windows thing")
	}
	calls := flakyRename(t, 2, sharingViolation)
	if err := Rename("a", "b"); err != nil {
		t.Fatalf("Rename = %v", err)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3", *calls)
	}
}

func TestRenameGivesUpOnALastingLock(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("brief locks are a Windows thing")
	}
	real := retryFor
	retryFor = 50 * time.Millisecond
	t.Cleanup(func() { retryFor = real })
	calls := flakyRename(t, 1_000_000, sharingViolation)

	if err := Rename("a", "b"); !errors.Is(err, syscall.Errno(32)) {
		t.Fatalf("Rename = %v, want the sharing violation", err)
	}
	if *calls < 2 {
		t.Errorf("calls = %d, want retries", *calls)
	}
}

func TestRenameDoesNotRetryOtherErrors(t *testing.T) {
	calls := flakyRename(t, 1_000_000, fs.ErrNotExist)
	if err := Rename("a", "b"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Rename = %v", err)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
}

func TestWriteFileReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for _, text := range []string{"first", "second"} {
		if err := WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "second" {
		t.Errorf("content = %q, %v", got, err)
	}
	assertOnlyFile(t, path)
}

// Writers that share a fixed temporary name trip over each other's file;
// unique names let every write finish.
func TestWriteFileConcurrentWritersAllSucceed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	var wg sync.WaitGroup
	errs := make([]error, 16)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = WriteFile(path, []byte(fmt.Sprintf("writer %d", i)), 0o644)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}
	assertOnlyFile(t, path)
}

func TestRemoveRetriesWhileBrieflyLocked(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("brief locks are a Windows thing")
	}
	calls := 0
	real := osRemove
	osRemove = func(string) error {
		calls++
		if calls <= 2 {
			return &os.PathError{Op: "remove", Err: syscall.Errno(5)}
		}
		return nil
	}
	t.Cleanup(func() { osRemove = real })
	if err := Remove("a"); err != nil || calls != 3 {
		t.Errorf("Remove = %v after %d calls", err, calls)
	}
}

// assertOnlyFile fails when anything but path is in its folder, such as a
// left-over temporary file.
func assertOnlyFile(t *testing.T, path string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("folder holds %v, want only %s", names, filepath.Base(path))
	}
}
