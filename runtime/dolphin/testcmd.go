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

	"github.com/juancavallotti/octo/runtime/dolphin/internal/assert"
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
		Check:    assert.Check,
	}, targets)

	return summarize(report, workDir)
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
				fmt.Printf("        %s\n", indent(failure))
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

// failureIndent is what a failure's continuation lines are indented to, so that a
// want/got diff lines up under the failure it belongs to instead of against the margin.
const failureIndent = "\n        "

// indent aligns a multi-line failure under its own first line. A body diff is two lines,
// and a diff whose second line starts at the margin is one the eye cannot pair up.
func indent(failure string) string {
	return strings.ReplaceAll(failure, "\n", failureIndent)
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
