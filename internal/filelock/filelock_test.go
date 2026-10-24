package filelock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLockAndUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock file missing while held: %v", err)
	}
	unlock()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file still there after unlock: %v", err)
	}
}

func TestLockWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan time.Time, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		released <- time.Now()
		unlock()
	}()
	unlock2, err := Lock(path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock2()
	if got := time.Now(); got.Before(<-released) {
		t.Error("second Lock returned before the first was released")
	}
}

func TestLockGivesUpWhenHeldTooLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	unlock, err := Lock(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = Lock(path, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "another scdeken") {
		t.Fatalf("err = %v", err)
	}
}

func TestLockTakesOverAStaleLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	if err := os.WriteFile(path, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-staleAfter - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := Lock(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	unlock()
}
