package run

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

const helperEnv = "SCDEKEN_HELPER_PROCESS"

// TestHelperProcess is not a test: the tests below re-run the test binary
// with it to get a child process. Mode "args" echoes its arguments as JSON,
// mode "lines" prints three lines, mode "fail" exits with status 3.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch args[0] {
	case "args":
		json.NewEncoder(os.Stdout).Encode(args[1:])
	case "lines":
		fmt.Println("one")
		fmt.Fprintln(os.Stderr, "two")
		fmt.Println("three")
	case "fail":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	}
	os.Exit(0)
}

// helper builds a Cmd that runs the helper process in the given mode.
func helper(mode string, args ...string) Cmd {
	all := append([]string{"-test.run=TestHelperProcess", "--", mode}, args...)
	return Cmd{Name: os.Args[0], Args: all, Env: append(os.Environ(), helperEnv+"=1")}
}

func TestRunKeepsSpacedArgsWhole(t *testing.T) {
	input := `C:\Users\Stefaan Himpe\x y`
	out, err := Exec{}.Output(context.Background(), helper("args", input))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("child output %q: %v", out, err)
	}
	if len(got) != 1 || got[0] != input {
		t.Fatalf("args = %q, want exactly [%q]", got, input)
	}
}

func TestRunStreamsLog(t *testing.T) {
	var log bytes.Buffer
	c := helper("lines")
	c.Log = &log
	if err := (Exec{}).Run(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(log.String())
	if strings.Join(got, ",") != "one,two,three" {
		t.Fatalf("log lines = %q, want one,two,three", got)
	}
}

func TestRunFailureIncludesOutput(t *testing.T) {
	err := Exec{}.Run(context.Background(), helper("fail"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want it to contain the command output", err)
	}
}

func TestFakeLookupOrder(t *testing.T) {
	f := &Fake{Script: map[string]FakeResult{
		"git *":         {Out: "short"},
		"git status *":  {Out: "long"},
		"git status -s": {Out: "exact"},
	}}
	ctx := context.Background()
	for args, want := range map[string]string{"status -s": "exact", "status -b": "long", "log": "short"} {
		got, err := f.Output(ctx, Cmd{Name: "git", Args: strings.Fields(args)})
		if err != nil || got != want {
			t.Errorf("git %s = %q, %v; want %q", args, got, err, want)
		}
	}
	_, err := f.Output(ctx, Cmd{Name: "make"})
	if err == nil || !strings.Contains(err.Error(), "fake: unexpected command: make") {
		t.Errorf("unscripted call err = %v", err)
	}
	if len(f.Calls) != 4 {
		t.Errorf("recorded %d calls, want 4", len(f.Calls))
	}
}

func TestWithoutPromptsKeepsTheEnvironmentAndSilencesGit(t *testing.T) {
	t.Setenv("SCDEKEN_TEST_MARKER", "kept")
	c := WithoutPrompts(Cmd{Name: "git"})
	env := strings.Join(c.Env, "\n")
	for _, want := range []string{"SCDEKEN_TEST_MARKER=kept", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s", want)
		}
	}
	given := WithoutPrompts(Cmd{Name: "git", Env: []string{"A=1"}})
	if len(given.Env) != 3 || given.Env[0] != "A=1" {
		t.Errorf("a given environment is extended, not replaced: %q", given.Env)
	}
}

func TestNoPromptWrapsEveryCall(t *testing.T) {
	f := &Fake{Script: map[string]FakeResult{"*": {}}}
	r := NoPrompt{Runner: f}
	r.Run(context.Background(), Cmd{Name: "git", Args: []string{"fetch"}})
	r.Output(context.Background(), Cmd{Name: "git", Args: []string{"ls-remote"}})
	for _, c := range f.Calls {
		if !strings.Contains(strings.Join(c.Env, " "), "GIT_TERMINAL_PROMPT=0") {
			t.Errorf("%v ran with prompts on", c.Args)
		}
	}
}

// A child that leaves a grandchild holding its output open must not block
// Wait forever.
func TestCommandsGiveUpWaitingForOutput(t *testing.T) {
	if got := command(context.Background(), Cmd{Name: "git"}).WaitDelay; got != waitDelay || got == 0 {
		t.Errorf("WaitDelay = %v", got)
	}
}
