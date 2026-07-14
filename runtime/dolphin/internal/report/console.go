// Package report renders what a run did — to a terminal, and to the JUnit XML a CI job
// reads.
//
// It is the only thing that writes to stdout. The workers that ran the cases produce
// failures as values and never print, which is what keeps a parallel run's output from
// interleaving into mush, and what lets the same results be rendered twice — once for a
// person, once for a machine — from one source.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
)

// failureIndent is what a failure's continuation lines are indented to, so a want/got
// diff lines up under the failure it belongs to instead of against the margin.
const failureIndent = "\n        "

// Console prints a run as it happens.
type Console struct {
	out io.Writer
	// Verbose names every case, not only the ones that went wrong. A passing run should
	// be quiet by default — a hundred lines of "ok" is a hundred lines nobody reads —
	// but when a suite passes and you do not believe it, this is how you check.
	verbose bool
}

// NewConsole returns a console reporter writing to out.
func NewConsole(out io.Writer, verbose bool) *Console {
	return &Console{out: out, verbose: verbose}
}

// printf writes a line of the report. The write error is discarded deliberately: a
// reporter that cannot write to stdout has nowhere to report that it cannot write to
// stdout, and failing a test run because a pipe closed would be worse than silence.
func (c *Console) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(c.out, format, args...)
}

// Suite prints one suite. It is called as each suite completes, in file order, so a long
// run reports as it goes rather than sitting silent and then printing everything.
func (c *Console) Suite(s runner.SuiteResult) {
	status := "ok  "
	if s.Failed() {
		status = "FAIL"
	}
	c.printf("%s %s  (%s)  %s\n", status, s.Target.File.Path, s.Target.File.Flow, took(s))

	for _, r := range s.Results {
		c.result(r)
	}
}

// result prints one case, when there is anything to say about it.
func (c *Console) result(r runner.Result) {
	switch r.Status {
	case runner.Failed:
		c.printf("  --- FAIL: %s (%s)\n", r.Case.Name, elapsed(r))
		for _, failure := range r.Failures {
			c.printf("        %s\n", indent(failure))
		}
		c.reproduce(r)

	case runner.Errored:
		// An errored case never ran, so there is nothing to say about the flow — only
		// about why octo would not run it.
		c.printf("  --- ERROR: %s\n        %s\n", r.Case.Name, indent(r.Err.Error()))
		c.reproduce(r)

	case runner.Skipped:
		c.printf("  --- SKIP: %s (%s)\n", r.Case.Name, r.Case.Skip)

	case runner.NotRun:
		c.printf("  --- NOT RUN: %s\n", r.Case.Name)

	case runner.Passed:
		if c.verbose {
			c.printf("  --- PASS: %s (%s)\n", r.Case.Name, elapsed(r))
		}
	}
}

// reproduce prints the octo command that produced the result, which is the single
// biggest thing driving octo as a subprocess buys: a failure hands you a runnable
// command rather than a description of one. The debug config it names is kept, so the
// command still works after the run is over.
func (c *Console) reproduce(r runner.Result) {
	if r.Outcome.Command == "" {
		return
	}
	c.printf("        reproduce: %s\n", r.Outcome.Command)
}

// Summary prints the totals and says where the failing runs were left.
func (c *Console) Summary(report []runner.SuiteResult, workDir string) {
	t := Tally(report)

	c.printf("\n%d passed, %d failed, %d errored, %d skipped",
		t.Passed, t.Failed, t.Errored, t.Skipped)
	if t.NotRun > 0 {
		c.printf(", %d not run", t.NotRun)
	}
	c.printf("  (%s)\n", t.Elapsed.Round(time.Millisecond))

	if t.Failed+t.Errored > 0 && workDir != "" {
		c.printf("\nthe runs are in %s\n", workDir)
	}
}

// Totals is what a run added up to.
type Totals struct {
	Passed, Failed, Errored, Skipped, NotRun int
	// Elapsed is the sum of the cases' own times, not the wall clock: the cases ran
	// concurrently, so the wall clock is shorter and would be the wrong thing to compare
	// between runs.
	Elapsed time.Duration
}

// Cases is how many cases the run had.
func (t Totals) Cases() int {
	return t.Passed + t.Failed + t.Errored + t.Skipped + t.NotRun
}

// Failing reports whether anything went wrong.
func (t Totals) Failing() bool { return t.Failed+t.Errored > 0 }

// Tally adds a report up.
func Tally(report []runner.SuiteResult) Totals {
	var t Totals
	for _, s := range report {
		for _, r := range s.Results {
			t.Elapsed += r.Outcome.Elapsed
			switch r.Status {
			case runner.Passed:
				t.Passed++
			case runner.Failed:
				t.Failed++
			case runner.Errored:
				t.Errored++
			case runner.Skipped:
				t.Skipped++
			case runner.NotRun:
				t.NotRun++
			}
		}
	}
	return t
}

// took is how long a suite's cases took, summed.
func took(s runner.SuiteResult) string {
	var total time.Duration
	for _, r := range s.Results {
		total += r.Outcome.Elapsed
	}
	return total.Round(time.Millisecond).String()
}

// elapsed is how long one case took.
func elapsed(r runner.Result) string {
	return r.Outcome.Elapsed.Round(time.Millisecond).String()
}

// indent aligns a multi-line failure under its own first line. A body diff is three
// lines, and a diff whose later lines start at the margin is one the eye cannot pair up.
func indent(failure string) string {
	return strings.ReplaceAll(failure, "\n", failureIndent)
}
