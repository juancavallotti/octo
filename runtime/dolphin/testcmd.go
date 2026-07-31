package main

// This file holds `dolphin test`: find the suites, run their cases against octo, and say
// what happened. The saying is report's job; this wires the three together and turns the
// tally into an exit code.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/assert"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/report"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// workDirPattern names the directory holding the per-case debug configs.
const workDirPattern = "dolphin-"

// testFlags are the parsed flags of `dolphin test`.
type testFlags struct {
	paths      []string
	config     string
	envFile    string
	junit      string
	reportJSON string
	parallel   int
	failFast   bool
	verbose    bool
}

// testCommand runs the suites the user named.
func testCommand(args []string) error {
	flags, err := parseTestFlags(args)
	if err != nil {
		return err
	}

	// octo skips a .env file that is not there, which is right for octo — a missing .env
	// is not a runtime error — and wrong here: a mistyped --env-file would run the whole
	// suite without the variables it asked for and fail every case on something else.
	if flags.envFile != "" {
		if _, err := os.Stat(flags.envFile); err != nil {
			return usageErr("--env-file: %w", err)
		}
	}

	targets, err := suite.Discover(flags.paths, flags.config)
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}

	octo, err := readyOcto()
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}

	// A failing case's debug config has to outlive the run: the command dolphin prints
	// to reproduce it names that file, and a command pointing at a file we deleted is not
	// a command. The directory is kept when anything failed, and the report says where.
	workDir, err := os.MkdirTemp("", workDirPattern)
	if err != nil {
		return fmt.Errorf("make a working directory: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	console := report.NewConsole(os.Stdout, flags.verbose)
	// Timed from here rather than from the top of the command, so the wall clock covers
	// the same work the per-case times do — the two are meant to be compared, and a
	// figure that also counted discovery and the octo probe would not be comparable.
	started := time.Now()
	results := runner.Run(ctx, runner.Config{
		Options: runner.Options{
			Octo:    octo,
			WorkDir: workDir,
			Verbose: flags.verbose,
			EnvFile: flags.envFile,
		},
		Parallel: flags.parallel,
		FailFast: flags.failFast,
		Check:    assert.Check,
		// Printed as each suite completes, in file order, so a long run reports as it
		// goes rather than sitting silent and then printing everything at the end.
		OnSuiteDone: console.Suite,
	}, targets)

	return finish(flags, console, results, workDir, time.Since(started))
}

// finish writes the reports and turns the tally into an exit code. wall is how long the
// whole run took, which only this level knows: the runner reports each case's own time,
// and under --parallel those do not add up to the clock on the wall.
func finish(
	flags testFlags,
	console *report.Console,
	results []runner.SuiteResult,
	workDir string,
	wall time.Duration,
) error {
	totals := report.Tally(results)
	kept := keptWorkDir(totals, workDir)
	console.Summary(results, kept)
	var errs []error

	if flags.junit != "" {
		if err := report.JUnit(flags.junit, results); err != nil {
			errs = append(errs, err)
		}
	}
	// Written for every verdict, not just a green one: a reader's whole reason for
	// asking is to find out WHICH cases failed, so the report a failing run produces is
	// the one that matters most.
	if flags.reportJSON != "" {
		if err := report.JSON(flags.reportJSON, results, wall, kept, versionLine()); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return verdict(totals)
}

// keptWorkDir removes the working directory when nothing failed — there is nothing left
// to reproduce — and returns it when there is, so the report can say where it went.
func keptWorkDir(totals report.Totals, workDir string) string {
	if totals.Failing() {
		return workDir
	}
	_ = os.RemoveAll(workDir)
	return ""
}

// verdict is the run's exit code.
//
// An ERRORED case never ran: octo refused it, because a block address does not resolve
// or the config does not parse. That is the suite being wrong, not the flows — exit 2,
// the same as a malformed test file — so a CI job is not sent to debug a flow that was
// never called. A case that ran and did not do what it said is exit 1.
func verdict(totals report.Totals) error {
	switch {
	case totals.Errored > 0:
		return &exitError{
			code: exitUsage,
			err:  fmt.Errorf("%d case(s) could not be run — the suite or the config is wrong", totals.Errored),
		}
	case totals.Failed > 0:
		return &exitError{code: exitFailed, err: fmt.Errorf("%d case(s) failed", totals.Failed)}
	default:
		return nil
	}
}

// readyOcto resolves the octo binary and proves it runs, before any case is spawned. A
// run that is going to fail because there is no usable octo should say so once, up front,
// rather than once per case.
func readyOcto() (string, error) {
	bin, err := resolveOcto()
	if err != nil {
		return "", err
	}
	if _, err := bin.version(context.Background()); err != nil {
		return "", err
	}
	return bin.Path, nil
}

// parseTestFlags parses the flags of `dolphin test`. Paths may come before or after the
// flags, as they do for `go test`.
func parseTestFlags(args []string) (testFlags, error) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own

	var flags testFlags
	fs.StringVar(&flags.config, "config", "",
		"the flows to test against, when they are not the ones beside the suite")
	fs.StringVar(&flags.envFile, "env-file", "",
		"a .env file every case runs with (a suite's own env: block overrides it)")
	fs.StringVar(&flags.junit, "junit", "", "write a JUnit XML report to this file")
	fs.StringVar(&flags.reportJSON, "report-json", "",
		"write a machine-readable JSON report to this file")
	fs.IntVar(&flags.parallel, "parallel", 0, "how many cases to run at once (default: one per CPU)")
	fs.BoolVar(&flags.failFast, "fail-fast", false, "stop after the first failing case")
	fs.BoolVar(&flags.verbose, "v", false, "name every case, and let octo's logs through")

	paths, err := parseInterspersed(fs, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return testFlags{}, nil
		}
		return testFlags{}, usageErr("parse test flags: %w", err)
	}
	flags.paths = paths
	return flags, nil
}

// parseInterspersed parses flags that may appear on either side of the paths, and returns
// the paths.
//
// Go's flag package stops at the first non-flag argument, so `dolphin test ./flows
// --junit report.xml` would leave --junit unparsed. Dropping it there would be the worst
// outcome available: a CI job would get no report, and nothing would say why. So each
// non-flag argument is taken as a path and parsing resumes after it — which is what
// people will type, because it is what `go test` accepts.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var paths []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err //nolint:wrapcheck // the caller labels it, and may be checking for flag.ErrHelp
		}
		args = fs.Args()
		if len(args) == 0 {
			return paths, nil
		}
		paths = append(paths, args[0])
		args = args[1:]
	}
}
