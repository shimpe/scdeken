package run

import (
	"context"
	"os"
)

// NoPrompt runs every process with git's password prompts switched off, so
// a repository that has become private fails instead of waiting for input
// nobody can give.
type NoPrompt struct{ Runner }

// Run implements Runner.
func (n NoPrompt) Run(ctx context.Context, c Cmd) error {
	return n.Runner.Run(ctx, WithoutPrompts(c))
}

// Output implements Runner.
func (n NoPrompt) Output(ctx context.Context, c Cmd) (string, error) {
	return n.Runner.Output(ctx, WithoutPrompts(c))
}

// WithoutPrompts returns c with GIT_TERMINAL_PROMPT=0 and
// GCM_INTERACTIVE=never added to its environment (the current process's
// environment when c has none of its own).
func WithoutPrompts(c Cmd) Cmd {
	env := c.Env
	if env == nil {
		env = os.Environ()
	}
	c.Env = append(append([]string(nil), env...), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	return c
}
