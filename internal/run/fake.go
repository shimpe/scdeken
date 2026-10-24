package run

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// FakeResult is the scripted outcome of one command.
type FakeResult struct {
	Out string
	Err error
}

// Fake is a Runner for tests. It records every call and answers from Script.
// It is safe for sequential use only.
type Fake struct {
	Calls  []Cmd
	Script map[string]FakeResult // key: Name + " " + Args joined by spaces; a trailing "*" matches any suffix
	Paths  map[string]string     // LookPath answers; a missing name gives exec.ErrNotFound
	OnRun  func(c Cmd) error     // optional side effects, e.g. creating files
}

// Run implements Runner. Scripted output goes to c.Log.
func (f *Fake) Run(_ context.Context, c Cmd) error {
	res, err := f.answer(c)
	if err != nil {
		return err
	}
	if c.Log != nil && res.Out != "" {
		fmt.Fprintln(c.Log, res.Out)
	}
	if res.Err != nil {
		return res.Err
	}
	if f.OnRun != nil {
		return f.OnRun(c)
	}
	return nil
}

// Output implements Runner.
func (f *Fake) Output(_ context.Context, c Cmd) (string, error) {
	res, err := f.answer(c)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Out), res.Err
}

// LookPath implements Runner.
func (f *Fake) LookPath(name string) (string, error) {
	if p, ok := f.Paths[name]; ok {
		return p, nil
	}
	return "", fmt.Errorf("look up %s: %w", name, exec.ErrNotFound)
}

// answer records the call and finds its scripted result.
func (f *Fake) answer(c Cmd) (FakeResult, error) {
	f.Calls = append(f.Calls, c)
	key := strings.TrimSpace(c.Name + " " + strings.Join(c.Args, " "))
	if res, ok := f.Script[key]; ok {
		return res, nil
	}
	if res, ok := f.longestPrefix(key); ok {
		return res, nil
	}
	return FakeResult{}, fmt.Errorf("fake: unexpected command: %s", key)
}

// longestPrefix finds the longest "prefix*" pattern that matches key.
func (f *Fake) longestPrefix(key string) (FakeResult, bool) {
	best, found := "", false
	var res FakeResult
	for pattern, r := range f.Script {
		prefix, isPattern := strings.CutSuffix(pattern, "*")
		if !isPattern || !strings.HasPrefix(key, prefix) {
			continue
		}
		if !found || len(prefix) > len(best) {
			best, res, found = prefix, r, true
		}
	}
	return res, found
}
