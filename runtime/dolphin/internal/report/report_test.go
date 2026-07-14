package report

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// results is a run with one of everything in it, which is what both reporters have to
// render and what a CI reader has to parse.
func results() []runner.SuiteResult {
	return []runner.SuiteResult{{
		Target: suite.Target{
			Config: "flows/orders.yaml",
			File:   suite.File{Flow: "charge-flowlevel", Path: "flows/orders_test.yaml"},
		},
		Results: []runner.Result{{
			Case:    suite.Case{Name: "a small charge is captured"},
			Status:  runner.Passed,
			Outcome: runner.Outcome{Elapsed: 120 * time.Millisecond},
		}, {
			Case:   suite.Case{Name: "a declined card takes the error path"},
			Status: runner.Failed,
			Failures: []string{
				"expect.body:\n      want: {\"ok\":true}\n      got:  {\"ok\":false}",
				"expect.vars.httpStatus: not set (vars: none)",
			},
			Outcome: runner.Outcome{
				Elapsed: 80 * time.Millisecond,
				Command: "octo invoke --config flows/orders.yaml --flow charge-flowlevel --envelope",
			},
		}, {
			Case:    suite.Case{Name: "it watches a block that is not there"},
			Status:  runner.Errored,
			Err:     errors.New(`octo rejected the run: spy "orders.nope": no block "nope" in that chain`),
			Outcome: runner.Outcome{Command: "octo invoke --config flows/orders.yaml --flow x --envelope"},
		}, {
			Case:   suite.Case{Name: "not ready", Skip: "the upstream contract is still moving"},
			Status: runner.Skipped,
		}, {
			Case:   suite.Case{Name: "never reached"},
			Status: runner.NotRun,
		}},
	}}
}

// console renders the run and returns what was printed.
func console(t *testing.T, verbose bool) string {
	t.Helper()
	var out bytes.Buffer
	c := NewConsole(&out, verbose)
	for _, s := range results() {
		c.Suite(s)
	}
	c.Summary(results(), "/tmp/dolphin-123")
	return out.String()
}

// TestConsoleSaysWhatWentWrongAndHowToReproduceIt.
func TestConsoleSaysWhatWentWrongAndHowToReproduceIt(t *testing.T) {
	out := console(t, false)

	for _, want := range []string{
		"FAIL flows/orders_test.yaml",
		"--- FAIL: a declined card takes the error path",
		"expect.vars.httpStatus: not set",
		"--- ERROR: it watches a block that is not there",
		`no block "nope" in that chain`,
		"--- SKIP: not ready (the upstream contract is still moving)",
		"--- NOT RUN: never reached",
		"1 passed, 1 failed, 1 errored, 1 skipped, 1 not run",
		"the runs are in /tmp/dolphin-123",
		"reproduce: octo invoke --config flows/orders.yaml",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not contain %q:\n%s", want, out)
		}
	}
}

// TestConsoleIsQuietAboutPassingCases: a passing run should not print a line per case. A
// hundred lines of "ok" is a hundred lines nobody reads, and the failures drown in them.
func TestConsoleIsQuietAboutPassingCases(t *testing.T) {
	if out := console(t, false); strings.Contains(out, "a small charge is captured") {
		t.Errorf("a passing case was named without -v:\n%s", out)
	}
	if out := console(t, true); !strings.Contains(out, "--- PASS: a small charge is captured") {
		t.Errorf("-v did not name the passing case:\n%s", out)
	}
}

// TestAMultiLineFailureIsIndentedUnderItself: a body diff is three lines, and a diff
// whose later lines start at the margin is one the eye cannot pair up with its failure.
func TestAMultiLineFailureIsIndentedUnderItself(t *testing.T) {
	out := console(t, false)

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "want: {") && !strings.HasPrefix(line, "        ") {
			t.Errorf("a diff line is not indented under its failure:\n%q", line)
		}
	}
}

// junitDoc writes the report and parses it back.
func junitDoc(t *testing.T) (testsuites, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.xml")
	if err := JUnit(path, results()); err != nil {
		t.Fatalf("JUnit: %v", err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var doc testsuites
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the report is not parseable XML: %v\n%s", err, raw)
	}
	return doc, string(raw)
}

// TestJUnitIsValidXMLAndAddsUp. A report a CI reader chokes on is worse than no report:
// the build goes green on a run that failed. So this parses it back rather than matching
// strings, and checks the counts a dashboard shows.
func TestJUnitIsValidXMLAndAddsUp(t *testing.T) {
	doc, raw := junitDoc(t)

	if !strings.HasPrefix(raw, xml.Header) {
		t.Error("the report has no XML declaration")
	}
	if doc.Tests != 5 {
		t.Errorf("tests = %d, want 5", doc.Tests)
	}
	if doc.Failures != 1 {
		t.Errorf("failures = %d, want 1", doc.Failures)
	}
	if doc.Errors != 1 {
		t.Errorf("errors = %d, want 1", doc.Errors)
	}
	// A case the run never reached is reported as skipped, because that is what it is
	// from the reader's side: 1 the suite skipped, 1 the run never got to.
	if doc.Skipped != 2 {
		t.Errorf("skipped = %d, want 2 (1 skipped, 1 not run)", doc.Skipped)
	}
	if len(doc.Suites) != 1 || len(doc.Suites[0].Cases) != 5 {
		t.Fatalf("the suite did not round-trip: %+v", doc.Suites)
	}
}

// TestJUnitKeepsFailedAndErroredApart. They mean different things — a FAILED case ran and
// did not do what it said, an ERRORED one never ran at all — which is the same line
// dolphin's exit codes draw. A reader that cannot tell them apart shows a broken suite as
// a broken flow.
func TestJUnitKeepsFailedAndErroredApart(t *testing.T) {
	doc, _ := junitDoc(t)
	cases := doc.Suites[0].Cases

	failed := cases[1]
	if failed.Failure == nil || failed.Error != nil {
		t.Fatalf("the failed case is not a <failure>: %+v", failed)
	}
	if failed.Failure.Type != failureTypeAssertion {
		t.Errorf("failure type = %q, want %q", failed.Failure.Type, failureTypeAssertion)
	}
	// The message is the one line a dashboard shows in its LIST of tests, and it has to
	// earn that line. The first line of a body diff is the label "expect.body:" — the
	// story is on the two lines after it — so a message that stopped at the first line
	// would tell someone scanning a red build precisely nothing.
	if strings.Contains(failed.Failure.Message, "\n") {
		t.Errorf("the failure message is not one line: %q", failed.Failure.Message)
	}
	for _, want := range []string{"expect.body:", "want:", "got:"} {
		if !strings.Contains(failed.Failure.Message, want) {
			t.Errorf("the failure message %q does not say %q — it is a label, not a summary",
				failed.Failure.Message, want)
		}
	}
	// The detail is every failure, for whoever clicks through.
	if !strings.Contains(failed.Failure.Detail, "expect.vars.httpStatus") {
		t.Errorf("the detail dropped a failure: %q", failed.Failure.Detail)
	}

	errored := cases[2]
	if errored.Error == nil || errored.Failure != nil {
		t.Fatalf("the errored case is not an <error>: %+v", errored)
	}
	if errored.Error.Type != errorTypeRun {
		t.Errorf("error type = %q, want %q", errored.Error.Type, errorTypeRun)
	}
}

// TestJUnitCarriesTheReproduceCommand: someone reading a red build in a browser can copy
// the line that reproduces it on their laptop, which is the whole point of dolphin driving
// a real octo.
func TestJUnitCarriesTheReproduceCommand(t *testing.T) {
	doc, _ := junitDoc(t)

	if got := doc.Suites[0].Cases[1].SystemOut; !strings.Contains(got, "octo invoke") {
		t.Errorf("system-out = %q, want the reproduce command", got)
	}
}

// TestJUnitClassnameIsTheFlow, so a dashboard groups cases by the thing they test rather
// than by the file they happen to live in.
func TestJUnitClassnameIsTheFlow(t *testing.T) {
	doc, _ := junitDoc(t)

	for _, c := range doc.Suites[0].Cases {
		if c.Classname != "charge-flowlevel" {
			t.Errorf("case %q has classname %q, want the flow", c.Name, c.Classname)
		}
	}
}

// TestJUnitEscapesWhatItIsGiven. A failure message holds a body diff full of quotes and
// angle brackets, and a report that is not well-formed is one a CI reader silently drops —
// turning a red build green.
func TestJUnitEscapesWhatItIsGiven(t *testing.T) {
	nasty := []runner.SuiteResult{{
		Target: suite.Target{File: suite.File{Flow: "f", Path: "p_test.yaml"}},
		Results: []runner.Result{{
			Case:     suite.Case{Name: `a case with <angles> & "quotes"`},
			Status:   runner.Failed,
			Failures: []string{`want: {"a":"<b>&</b>"}`},
		}},
	}}

	path := filepath.Join(t.TempDir(), "report.xml")
	if err := JUnit(path, nasty); err != nil {
		t.Fatalf("JUnit: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	var doc testsuites
	if err := xml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("a failure full of XML broke the report: %v\n%s", err, raw)
	}
	if got := doc.Suites[0].Cases[0].Name; got != `a case with <angles> & "quotes"` {
		t.Errorf("the case name did not survive escaping: %q", got)
	}
}
