package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sc-deken/sc-deken/internal/run"
)

// Deps are the things tests replace: the process runner, the clock, the
// trusted index key and where human-readable log lines go.
type Deps struct {
	Runner      run.Runner
	Now         func() time.Time
	EmbeddedKey string
	Stderr      io.Writer
}

func (d Deps) withDefaults() Deps {
	if d.Runner == nil {
		d.Runner = run.Exec{}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Stderr == nil {
		d.Stderr = io.Discard
	}
	return d
}

// command is one row of the command table. The command line and a request
// file reach the same row, so both behave the same.
type command struct {
	usage string                                 // e.g. "install <package>[@version] [flags]"
	flags func(fs *flag.FlagSet, req *Request)   // registers the command's flags; may be nil
	args  func(pos []string, req *Request) error // reads the words that are not flags
	run   func(ctx context.Context, req Request, s *session) (any, error)
}

// commands is the command table. The maintainer commands add their own rows
// from init functions in their own files.
var commands = map[string]command{
	"platform": {usage: "platform", args: noArgs, run: runPlatform},
	"search": {
		usage: "search [query] [--kind plugin|quark|plugin+quark] [--include-unreviewed]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Kind, "kind", "", "only this kind of package")
			fs.BoolVar(&req.IncludeUnreviewed, "include-unreviewed", false, "list unreviewed packages too")
		},
		args: func(pos []string, req *Request) error { req.Query = strings.Join(pos, " "); return nil },
		run:  runSearch,
	},
	"info": {usage: "info <package>", args: onePackage, run: runInfo},
	"plan": {usage: "plan <package>[@version]", args: onePackage, run: runPlan},
	"install": {
		usage: "install <package>[@version] [--yes-unreviewed] [--yes-retracted] [--on-duplicate replace|keep]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.BoolVar(&req.Choices.ConfirmUnreviewed, "yes-unreviewed", false, "install a package nobody has reviewed")
			fs.BoolVar(&req.Choices.ConfirmRetracted, "yes-retracted", false, "install a version that was withdrawn")
			fs.StringVar(&req.Choices.OnDuplicate, "on-duplicate", "", "replace or keep a copy that is already installed")
		},
		args: onePackage,
		run:  runInstall,
	},
	"remove":           {usage: "remove <package>", args: onePackage, run: runRemove},
	"list":             {usage: "list", args: noArgs, run: runList},
	"updates":          {usage: "updates", args: noArgs, run: runUpdates},
	"doctor":           {usage: "doctor [<package>]", args: optionalPackage, run: runDoctor},
	"advisories-check": {usage: "advisories check", args: noArgs, run: runAdvisoriesCheck},
	"advisories-keep": {
		usage: "advisories keep <advisory-id>", run: runAdvisoriesKeep,
		args: func(pos []string, req *Request) error { return oneWord(pos, &req.AdvisoryID, "an advisory id") },
	},
	"quarantine": {
		usage: "quarantine <path> --id <advisory-id>",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.AdvisoryID, "id", "", "the advisory that withdrew this package")
		},
		args: func(pos []string, req *Request) error { return oneWord(pos, &req.Path, "a path") },
		run:  runQuarantine,
	},
	"quarantine-clear": {usage: "quarantine clear", args: noArgs, run: runQuarantineClear},
	"refresh":          {usage: "refresh", args: noArgs, run: runRefresh},
	"version":          {usage: "version", args: noArgs, run: runVersion},
}

// Execute runs one request and returns the data to answer with. An error may
// come with data, for example the plan that needs the user's confirmation.
func Execute(ctx context.Context, req Request, deps Deps) (any, error) {
	cmd, ok := commands[req.Command]
	if !ok {
		return nil, usageError{fmt.Sprintf("unknown command %q", req.Command)}
	}
	s := newSession(req, deps.withDefaults())
	defer s.close()
	return cmd.run(ctx, req, s)
}

// parseCommand fills req from the words after the command name.
func parseCommand(cmd command, args []string, req *Request) error {
	fs := flag.NewFlagSet(req.Command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if cmd.flags != nil {
		cmd.flags(fs, req)
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return usageError{err.Error()}
	}
	if cmd.args == nil {
		cmd.args = noArgs
	}
	return cmd.args(pos, req)
}

// parseInterspersed parses flags that may come before, between or after the
// other words, and returns those other words.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return words, nil
		}
		words = append(words, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func noArgs(pos []string, _ *Request) error {
	if len(pos) > 0 {
		return usageError{fmt.Sprintf("unexpected %q", pos[0])}
	}
	return nil
}

func oneWord(pos []string, dst *string, what string) error {
	if len(pos) != 1 {
		return usageError{"expected " + what}
	}
	*dst = pos[0]
	return nil
}

// onePackage reads "<name>" or "<name>@<version>".
func onePackage(pos []string, req *Request) error {
	var word string
	if err := oneWord(pos, &word, "a package name"); err != nil {
		return err
	}
	req.Package, req.Version, _ = strings.Cut(word, "@")
	return nil
}

func optionalPackage(pos []string, req *Request) error {
	if len(pos) == 0 {
		return nil
	}
	return onePackage(pos, req)
}
