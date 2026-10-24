package state

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestStateMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Packages == nil || len(s.Packages) != 0 || len(s.KeptAdvisories) != 0 {
		t.Errorf("want empty state, got %+v", s)
	}
}

func TestStateNilPackagesBecomesEmptyMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte(`{"packages": null}`), 0o644)
	s, err := Load(path)
	if err != nil || s.Packages == nil {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestStateCorruptIsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(path, []byte("{garbage"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("want error for corrupt state")
	}
}

func sample() *State {
	return &State{
		Packages: map[string]Entry{"foo": {
			Name: "Foo", Version: "1.2.0", Commit: "abc", Repo: "o/foo", Platform: "linux-x86_64-api3",
			Path: "/ext/sc-deken/Foo", Kind: "plugin", Route: Prebuilt,
			InstalledAt: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		}},
		KeptAdvisories: []string{"SCD-1"},
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "state.json")
	want := sample()
	if err := want.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file left behind")
	}
}

func TestSaveIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := sample().Save(path); err != nil {
		t.Fatal(err)
	}
	// A crash mid-save leaves a half-written temp file; the real file is untouched.
	os.WriteFile(path+".tmp", []byte("{half written garb"), 0o644)
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sample()) {
		t.Errorf("last good state lost: %+v", got)
	}
	// The next Save overwrites the stale temp file and replaces state.json.
	next := sample()
	next.KeptAdvisories = nil
	if err := next.Save(path); err != nil {
		t.Fatal(err)
	}
	got, _ = Load(path)
	if len(got.KeptAdvisories) != 0 {
		t.Errorf("second save not applied: %+v", got)
	}
}
