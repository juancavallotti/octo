// dolphin is octo's companion test runner: it drives the real octo CLI against an
// integration and checks what comes back, so a flow can have a test suite the way any
// other unit of code does.
//
// A flow file is tested by the suite beside it, as a Go file is — orders.yaml by
// orders_test.yaml — and each case in it is one `octo invoke`, run with the mocks and
// spies the case asked for. main is the command shell; the work is in internal/suite
// (what a suite is), internal/runner (running one), and internal/assert (judging it).
package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/juancavallotti/octo/runtime/core"
)

// Exit codes are a contract, because the thing that reads them is a CI job, not a
// person.
//
// The line between 1 and 2 is the one that matters: 1 means the flows are wrong, 2
// means the suite is. A CI job that cannot tell those apart sends someone to debug the
// wrong thing — so a malformed test file, an unresolvable block address, or a missing
// octo is never reported as a failing test.
const (
	exitOK     = 0 // every case passed
	exitFailed = 1 // one or more cases RAN and did not do what it said — the flows are wrong
	exitUsage  = 2 // bad flags, a bad suite, an unresolvable address, no usable octo
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

// exitCode is the process status for err. Anything that did not name a code is a usage
// error — which is the safe default: a failure dolphin could not classify is dolphin's
// own, and reporting it as 1 would tell a CI job the flows are broken when they may
// never have been called.
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
  dolphin [test] [path...]        Run the test suites at these paths (default: .)
  dolphin schema                  Print the JSON Schema of a _test.yaml
  dolphin version                 Print the version and build date
  dolphin --help                  Show this help

Test flags:
  --config <path>   the flows to test against, when they are not the ones beside the suite
  --env-file <path> a .env file every case runs with
  --junit <path>    write a JUnit XML report to this file
  --parallel <n>    how many cases to run at once (default: one per CPU)
  --fail-fast       stop after the first failing case
  -v                name every case, and let octo's logs through

  Flags may come before or after the paths.

Schema flags:
  --format <fmt>    json (default) or yaml
  --out <path>      write it to a file instead of stdout

  "dolphin schema" prints the shape of a _test.yaml, so an editor can complete one and
  a validator can check it.

Suites:
  A flow file is tested by the suite beside it, the way a Go file is: orders.yaml is
  tested by orders_test.yaml. The config is exactly what you point at.

    dolphin test ./flows              every *_test.yaml in the directory, against it
    dolphin test flows/orders.yaml    its companion orders_test.yaml, against that file
    dolphin test flows/orders_test.yaml    the suite, against the flow file beside it

  Use --config when the suite does not live beside the flows it exercises, which is
  also how you name a flow file and a test file that are not a pair:

    dolphin test --config flows/orders.yaml tests/smoke_test.yaml

Environment:
  A config's env is resolved when it is LOADED, before any block runs — so a flow whose
  connector reads ${ANTHROPIC_API_KEY} cannot even be built without one, and mocking the
  block that uses it does not help. Supply it in the suite:

    env:
      ANTHROPIC_API_KEY: test-key

  A suite's env: wins over --env-file, which wins over what you have exported. That order
  is deliberate: a test that quietly used your real API key because it was in your shell
  is a test that passes on your machine, bills you for it, and fails in CI.

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
  1  one or more cases failed  — the flows are wrong
  2  usage, suite, or octo-binary error  — the suite is wrong

  The line between 1 and 2 is load-bearing: a CI job that cannot tell "your flow is
  broken" from "your test file is broken" sends someone to debug the wrong thing.

Flags accept one or two dashes (--config or -config).`

// cmdTest names the one subcommand that does any work. It is also the default, so
// `dolphin ./flows` and `dolphin --parallel 4` both work without typing it — and an
// argument that is not "test" is a path, not an unknown command.
const (
	cmdTest   = "test"
	cmdSchema = "schema"
)

// run dispatches to a subcommand.
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

	if args[0] == cmdSchema {
		return schemaCommand(args[1:])
	}
	if args[0] == cmdTest {
		args = args[1:]
	}
	return testCommand(args)
}
