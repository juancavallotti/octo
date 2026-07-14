package main

// This file holds `dolphin test`: find the suites, run their cases against octo, and
// say what happened.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// workDirPattern names the directory holding the per-case debug configs.
const workDirPattern = "dolphin-"

// testFlags are the parsed flags of `dolphin test`.
type testFlags struct {
	paths    []string
	config   string
	parallel int
	failFast bool
	verbose  bool
}

// testCommand runs the suites the user named.
func testCommand(args []string) error {
	flags, err := parseTestFlags(args)
	if err != nil {
		return err
	}

	targets, err := suite.Discover(flags.paths, flags.config)
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}

	bin, err := resolveOcto()
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}
	if _, err := bin.version(context.Background()); err != nil {
		return &exitError{code: exitUsage, err: err}
	}

	// A failing case's debug config has to outlive the run: the command dolphin prints
	// to reproduce it names that file, and a command pointing at a file we deleted is
	// not a command. The directory is left behind, and the report says where.
	workDir, err := os.MkdirTemp("", workDirPattern)
	if err != nil {
		return fmt.Errorf("make a working directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report := runner.Run(ctx, runner.Config{
		Options: runner.Options{
			Octo:    bin.Path,
			WorkDir: workDir,
			Verbose: flags.verbose,
		},
		Parallel: flags.parallel,
		FailFast: flags.failFast,
		Check:    check,
	}, targets)

	return summarize(report, workDir)
}

// check judges an outcome against what the case expected.
//
// The matchers are not here yet, so this checks the one thing every case asserts
// whether it says so or not: that the flow COMPLETED. A case with no `expect` at all is
// a smoke test, and a smoke test that passes when the flow blew up is not a test.
//
// Anything more than that, it refuses — loudly. Returning "no failures" for an
// expectation it cannot evaluate would be the worst possible stub: the suite would go
// green while asserting nothing, which is the exact failure a test runner exists to
// prevent.
func check(c suite.Case, outcome runner.Outcome) []string {
	if failures := checkCompleted(c, outcome); len(failures) > 0 {
		return failures
	}
	if !c.Expect.IsEmpty() || len(c.Spies) > 0 {
		return []string{"dolphin cannot check `body`, `vars`, `that` or `spies` yet"}
	}
	return nil
}

// checkCompleted checks what the flow DID — completed, dropped, or failed — which is
// the assertion every case makes, including the ones that make no others. A case with
// no `expect` at all is a smoke test, and a smoke test that goes green when the flow
// blew up is not a test.
//
// The `error` substring is checked here rather than left to the matchers, because
// half-checking it would be worse than not checking it: a case expecting "card declined"
// would pass on ANY failure, and a green run that proved the wrong thing is the one
// outcome a test runner must never produce.
func checkCompleted(c suite.Case, outcome runner.Outcome) []string {
	switch {
	case c.Expect.Error != "":
		if outcome.Error == "" {
			return []string{fmt.Sprintf("the flow did not fail, and the case expected it to fail with %q", c.Expect.Error)}
		}
		if !strings.Contains(outcome.Error, c.Expect.Error) {
			return []string{fmt.Sprintf("the flow failed with %q, which does not contain %q", outcome.Error, c.Expect.Error)}
		}
	case c.Expect.Dropped:
		if !outcome.Dropped {
			return []string{"the flow did not drop the message, and the case expected it to"}
		}
	case outcome.Error != "":
		return []string{fmt.Sprintf("the flow failed: %s", outcome.Error)}
	case outcome.Dropped:
		return []string{"the flow dropped the message"}
	}
	return nil
}

// summarize prints the report and returns the run's verdict.
func summarize(report []runner.SuiteResult, workDir string) error {
	var passed, failed, errored, skipped, notRun int
	for _, s := range report {
		for _, r := range s.Results {
			switch r.Status {
			case runner.Passed:
				passed++
			case runner.Failed:
				failed++
			case runner.Errored:
				errored++
			case runner.Skipped:
				skipped++
			case runner.NotRun:
				notRun++
			}
		}
	}

	for _, s := range report {
		printSuite(s)
	}

	fmt.Printf("\n%d passed, %d failed, %d errored, %d skipped", passed, failed, errored, skipped)
	if notRun > 0 {
		fmt.Printf(", %d not run", notRun)
	}
	fmt.Println()

	if failed+errored == 0 {
		_ = os.RemoveAll(workDir) // nothing failed, so nothing needs reproducing
		return nil
	}
	fmt.Printf("\nthe runs are in %s\n", workDir)

	// An ERRORED case never ran: octo refused it, because a block address does not
	// resolve or the config does not parse. That is the suite being wrong, not the
	// flows — exit 2, the same as a malformed test file, so a CI job is not sent to
	// debug a flow that was never even called. A failing case, which did run, is 1.
	if errored > 0 {
		return &exitError{
			code: exitUsage,
			err:  fmt.Errorf("%d case(s) could not be run — the suite or the config is wrong", errored),
		}
	}
	return &exitError{code: exitFailed, err: fmt.Errorf("%d case(s) failed", failed)}
}

// printSuite prints one suite's results.
func printSuite(s runner.SuiteResult) {
	status := "ok  "
	if s.Failed() {
		status = "FAIL"
	}
	fmt.Printf("%s %s  (%s)\n", status, s.Target.File.Path, s.Target.File.Flow)

	for _, r := range s.Results {
		switch r.Status {
		case runner.Failed:
			fmt.Printf("  --- FAIL: %s\n", r.Case.Name)
			for _, failure := range r.Failures {
				fmt.Printf("        %s\n", failure)
			}
			fmt.Printf("        reproduce: %s\n", r.Outcome.Command)
		case runner.Errored:
			fmt.Printf("  --- ERROR: %s\n        %s\n", r.Case.Name, r.Err)
			if r.Outcome.Command != "" {
				fmt.Printf("        reproduce: %s\n", r.Outcome.Command)
			}
		case runner.Skipped:
			fmt.Printf("  --- SKIP: %s (%s)\n", r.Case.Name, r.Case.Skip)
		case runner.NotRun:
			fmt.Printf("  --- NOT RUN: %s\n", r.Case.Name)
		case runner.Passed:
		}
	}
}

// parseTestFlags parses the flags of `dolphin test`. Paths may come before or after the
// flags, as they do for `go test`.
func parseTestFlags(args []string) (testFlags, error) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own

	var flags testFlags
	fs.StringVar(&flags.config, "config", "",
		"the flows to test against, when they are not the ones beside the suite")
	fs.IntVar(&flags.parallel, "parallel", 0, "how many cases to run at once (default: one per CPU)")
	fs.BoolVar(&flags.failFast, "fail-fast", false, "stop after the first failing case")
	fs.BoolVar(&flags.verbose, "v", false, "let octo's logs through")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return testFlags{}, nil
		}
		return testFlags{}, usageErr("parse test flags: %w", err)
	}
	flags.paths = positional(fs.Args())
	return flags, nil
}

// positional pulls the paths out of the remaining arguments, allowing them on either
// side of the flags.
func positional(args []string) []string {
	paths := make([]string, 0, len(args))
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			paths = append(paths, arg)
		}
	}
	return paths
}
