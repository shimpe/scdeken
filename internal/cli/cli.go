// Package cli is the scdeken command line. SuperCollider calls it with
// "scdeken run <request.json> <result.json>"; people and CI use flags. Both
// build a Request and go through Execute.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/sign"
)

// Main runs scdeken with the given arguments (without the program name) and
// returns the process exit code: 0 ok, 1 error, 2 usage, 3 needs the user's
// confirmation.
func Main(args []string, stdout, stderr io.Writer) int {
	return mainWithDeps(args, stdout, stderr, Deps{
		Runner: run.Exec{}, Now: time.Now, EmbeddedKey: sign.EmbeddedPublicKey,
	})
}

const usageText = `usage: scdeken [--context <file>] [--offline] [--index-url <url>] <command> [arguments]

commands:
  platform | search | info | plan | install | remove | list | updates
  doctor | advisories check | advisories keep | quarantine | refresh | version
  recipe new <github-url> [--out <file>]
  recipe check <file> [--build] [--sc-version 3.14.1]
  recipe approve <file> <version> <commit>
  index build|sign|keygen|new-versions   (maintainers)
  advisories validate --dir <folder>
  ci matrix|platforms|pack   (CI)
  build --recipe <file> --version <v> --out <folder>   (CI)
  run <request.json> <result.json>`

// global holds the flags that come before the command name.
type global struct {
	context  string
	offline  bool
	indexURL string
}

func mainWithDeps(args []string, stdout, stderr io.Writer, deps Deps) int {
	deps.Stderr = stderr
	deps = deps.withDefaults()
	var g global
	fs := flag.NewFlagSet("scdeken", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&g.context, "context", "", "session file written by SuperCollider")
	fs.BoolVar(&g.offline, "offline", false, "use only the saved package list")
	fs.StringVar(&g.indexURL, "index-url", "", "package list address")
	if err := fs.Parse(args); err != nil || fs.NArg() == 0 {
		return usageFailure(stderr, err)
	}
	words := fs.Args()
	if words[0] == "run" {
		return runRequestFile(words[1:], stderr, deps)
	}
	return runFlags(g, words, stdout, stderr, deps)
}

// usageFailure prints the short usage and returns exit code 2.
func usageFailure(stderr io.Writer, err error) int {
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	fmt.Fprintln(stderr, usageText)
	return exitUsage
}

// runFlags runs a command typed on the command line.
func runFlags(g global, words []string, stdout, stderr io.Writer, deps Deps) int {
	name, rest := commandName(words)
	cmd, ok := commands[name]
	if !ok {
		return usageFailure(stderr, fmt.Errorf("unknown command %q", words[0]))
	}
	req := Request{Command: name, Offline: g.offline, IndexURL: g.indexURL}
	if err := parseCommand(cmd, rest, &req); err != nil {
		fmt.Fprintf(stderr, "%v\nusage: scdeken %s\n", err, cmd.usage)
		return exitUsage
	}
	var err error
	if req.Context, err = loadContext(g.context); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	data, err := Execute(context.Background(), req, deps)
	if text, ok := data.(plainTexter); ok && err == nil {
		io.WriteString(stdout, text.plainText())
	} else {
		stdout.Write(encodeResponse(newResponse(data, err)))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	return exitCode(err)
}

// plainTexter is implemented by answers that print as text on the command
// line, such as a drafted recipe; the request file form still gets JSON.
type plainTexter interface{ plainText() string }

// commandName joins two-word commands such as "advisories check" into the
// table's "advisories-check".
func commandName(words []string) (string, []string) {
	if len(words) >= 2 {
		if _, ok := commands[words[0]+"-"+words[1]]; ok {
			return words[0] + "-" + words[1], words[2:]
		}
	}
	return words[0], words[1:]
}

func loadContext(path string) (scenv.Context, error) {
	if path == "" {
		return scenv.DefaultContext(), nil
	}
	return scenv.LoadContext(path)
}

// runRequestFile is "scdeken run <request.json> <result.json>": the answer
// goes into the result file, with the same exit code as the flag form.
func runRequestFile(args []string, stderr io.Writer, deps Deps) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: scdeken run <request.json> <result.json>")
		return exitUsage
	}
	requestPath, resultPath := args[0], args[1]
	req, err := readRequest(requestPath)
	var data any
	if err == nil {
		data, err = Execute(context.Background(), req, deps)
	}
	if werr := writeFileAtomic(resultPath, encodeResponse(newResponse(data, err))); werr != nil {
		fmt.Fprintln(stderr, werr)
		return exitError
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	return exitCode(err)
}
