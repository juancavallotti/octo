// dolphin is octo's companion test runner: it drives the real octo CLI against an
// integration and checks what comes back, so a flow can have a test suite the way
// any other unit of code does.
//
// This is the bootstrap: dolphin finds the octo binary and reads its flags. Running
// the cases in a config comes next — see `dolphin run`.
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
)

// Exit codes are a contract, because the thing that reads them is a CI job, not a
// person. They are fixed now, before dolphin runs anything, so that a suite wired
// up today keeps meaning the same thing once it does. 1 is reserved for "one or
// more cases failed" and is not produced yet.
const (
	exitOK             = 0 // every case passed
	exitUsage          = 2 // bad flags, bad config, or no usable octo binary
	exitNotImplemented = 3 // dolphin resolved everything but cannot run the cases yet
)

// exitError carries the exit code a failure must produce, so the code lives with
// the failure that chose it instead of being re-derived from the message in main.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usageErr is a failure the user can fix by typing something different: a missing
// flag, a config that is not there, an octo binary we could not run.
func usageErr(format string, args ...any) error {
	return &exitError{code: exitUsage, err: fmt.Errorf(format, args...)}
}

// exitCode is the process status for err. Anything that did not name a code is a
// usage error: dolphin only fails on its own behalf until it can run tests.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var coded *exitError
	if errors.As(err, &coded) {
		return coded.code
	}
	return exitUsage
}

func main() {
	level, levelErr := core.ParseLevel(os.Getenv("LOG_LEVEL"))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(os.Args[1:]); err != nil {
		slog.Error("dolphin stopped with error", "error", err)
		os.Exit(exitCode(err))
	}
}

// usage is the help page, printed for `dolphin`, `dolphin --help`, and a
// subcommand's --help.
const usage = `dolphin — unit-test octo integrations

Usage:
  dolphin [run] --config <path>   Run the cases in a dolphin test config (default)
  dolphin version                 Print the version and build date
  dolphin --help                  Show this help

Run flags:
  --config <path>   path to the dolphin test config

The octo binary:
  dolphin drives the real octo CLI. It looks for it in this order, and stops at the
  first one it finds:

    1. $OCTO_PATH   the binary itself, or a directory holding it
    2. ./octo       in the current directory
    3. octo         on your PATH

  $OCTO_PATH is an override, not a hint: when it is set and does not name a runnable
  octo, dolphin fails rather than quietly testing against a different build than the
  one you asked for.

Exit codes:
  0  every case passed
  1  one or more cases failed
  2  usage, config, or octo-binary error
  3  running the cases is not implemented yet

Flags accept one or two dashes (--config or -config).`

// run dispatches to a subcommand. The default (no subcommand, or a leading flag) is
// "run", so `dolphin --config x.yaml` works without typing it.
func run(args []string) error {
	// Help and version are handled before dispatch: the subcommand flagsets do not
	// define them. Go's flag package treats -x and --x alike, so honor both forms.
	if len(args) == 0 {
		fmt.Println(usage)
		return nil
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Println(usage)
		return nil
	case "-version", "--version", "version":
		fmt.Println(versionLine())
		return nil
	}

	cmd := "run"
	if !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runCommand(args)
	default:
		return usageErr("unknown command %q (expected \"run\" or \"version\")", cmd)
	}
}
