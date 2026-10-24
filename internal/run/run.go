// Package run starts external processes. It is the only place the helper
// launches programs, so tests can swap in a Fake.
package run

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// tailLines is how many trailing output lines go into a failure message.
const tailLines = 20

// Cmd describes one process to start. Args are passed as-is, never via a shell.
type Cmd struct {
	Name string
	Args []string
	Dir  string
	Env  []string
	Log  io.Writer // receives combined stdout+stderr line by line; nil discards
}

// Runner starts processes and finds programs on PATH.
type Runner interface {
	// Run runs c to completion, streaming its output to c.Log.
	Run(ctx context.Context, c Cmd) error
	// Output runs c and returns its trimmed stdout.
	Output(ctx context.Context, c Cmd) (string, error)
	// LookPath reports where the named program lives on PATH.
	LookPath(name string) (string, error)
}

// Exec is the real Runner, built on os/exec.
type Exec struct{}

// Run implements Runner. On a non-zero exit the error names the command and
// carries the last lines of its output.
func (Exec) Run(ctx context.Context, c Cmd) error {
	cmd := command(ctx, c)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", c.Name, err)
	}
	tail := &lineTail{max: tailLines}
	done := make(chan struct{})
	go func() {
		defer close(done)
		stream(pr, c.Log, tail)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	if err != nil {
		return fmt.Errorf("run %s: %w\n%s", c.Name, err, tail.String())
	}
	return nil
}

// Output implements Runner. On failure the error includes stderr.
func (Exec) Output(ctx context.Context, c Cmd) (string, error) {
	cmd := command(ctx, c)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run %s: %w\n%s", c.Name, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// LookPath implements Runner.
func (Exec) LookPath(name string) (string, error) { return exec.LookPath(name) }

// waitDelay is how long Wait waits for the output pipes to close after the
// process has exited or been killed. A grandchild (a git credential helper,
// a compiler server) that keeps them open must not hang the helper.
const waitDelay = 10 * time.Second

// command builds the os/exec command for c.
func command(ctx context.Context, c Cmd) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.WaitDelay = waitDelay
	cmd.Dir = c.Dir
	if c.Env != nil {
		cmd.Env = c.Env
	}
	return cmd
}

// stream copies r line by line to log (if any) and remembers the last lines.
func stream(r io.Reader, log io.Writer, tail *lineTail) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		tail.add(line)
		if log != nil {
			fmt.Fprintln(log, line)
		}
	}
	io.Copy(io.Discard, r) // never leave the child blocked on a full pipe
}

// lineTail keeps the most recent lines of output.
type lineTail struct {
	max   int
	lines []string
}

func (t *lineTail) add(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.max {
		t.lines = t.lines[1:]
	}
}

func (t *lineTail) String() string { return strings.Join(t.lines, "\n") }
