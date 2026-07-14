package report

// This file writes the JUnit XML a CI job reads. The schema is a de facto one — there
// is no official JUnit XSD — so this targets what the readers actually parse: the
// element names and attributes Jenkins, GitLab and the GitHub test-reporter actions all
// agree on. Anything they disagree about is left out rather than guessed at.

import (
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
)

// reportMode is the permission for the written report. It is a build artifact a CI job
// reads back, not a secret.
const reportMode = 0o644

// summaryLimit bounds the one-line `message` a dashboard shows in its list of tests. A
// body diff can be a screenful, and a list row is not where to put one.
const summaryLimit = 160

// failureType is the `type` attribute on a <failure>. A reader groups by it, so the two
// are kept apart: a case that FAILED ran and did not do what it said, while one that
// ERRORED never ran at all — the same distinction dolphin's exit codes draw, carried
// into the report so a CI dashboard can draw it too.
const (
	failureTypeAssertion = "assertion"
	errorTypeRun         = "run"
)

// testsuites is the document root.
type testsuites struct {
	XMLName  xml.Name    `xml:"testsuites"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr"`
	Suites   []testsuite `xml:"testsuite"`
}

// testsuite is one _test.yaml.
type testsuite struct {
	Name     string     `xml:"name,attr"`
	Tests    int        `xml:"tests,attr"`
	Failures int        `xml:"failures,attr"`
	Errors   int        `xml:"errors,attr"`
	Skipped  int        `xml:"skipped,attr"`
	Time     string     `xml:"time,attr"`
	Cases    []testcase `xml:"testcase"`
}

// testcase is one case. Classname is the flow, so a CI dashboard groups the cases by the
// thing they test rather than by the file they happen to live in.
type testcase struct {
	Name      string   `xml:"name,attr"`
	Classname string   `xml:"classname,attr"`
	Time      string   `xml:"time,attr"`
	Failure   *failure `xml:"failure,omitempty"`
	Error     *failure `xml:"error,omitempty"`
	Skipped   *skipped `xml:"skipped,omitempty"`
	SystemOut string   `xml:"system-out,omitempty"`
}

// failure carries what went wrong: a one-line message a dashboard shows in its list, and
// the full detail in the body for whoever clicks through.
type failure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Detail  string `xml:",chardata"`
}

// skipped carries the reason the case said not to run it.
type skipped struct {
	Message string `xml:"message,attr"`
}

// JUnit writes report to path as JUnit XML.
func JUnit(path string, report []runner.SuiteResult) error {
	doc := document(report)

	encoded, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the junit report: %w", err)
	}
	// A reader is entitled to a declaration, and encoding/xml does not add one.
	body := append([]byte(xml.Header), encoded...)
	body = append(body, '\n')

	if err := os.WriteFile(path, body, reportMode); err != nil {
		return fmt.Errorf("write the junit report to %s: %w", path, err)
	}
	return nil
}

// document builds the report.
func document(report []runner.SuiteResult) testsuites {
	total := Tally(report)
	doc := testsuites{
		Name:     "dolphin",
		Tests:    total.Cases(),
		Failures: total.Failed,
		Errors:   total.Errored,
		Skipped:  total.Skipped + total.NotRun,
		Time:     seconds(total.Elapsed),
		Suites:   make([]testsuite, 0, len(report)),
	}
	for _, s := range report {
		doc.Suites = append(doc.Suites, suiteOf(s))
	}
	return doc
}

// suiteOf renders one suite.
func suiteOf(s runner.SuiteResult) testsuite {
	t := Tally([]runner.SuiteResult{s})
	out := testsuite{
		Name:     s.Target.File.Path,
		Tests:    t.Cases(),
		Failures: t.Failed,
		Errors:   t.Errored,
		Skipped:  t.Skipped + t.NotRun,
		Time:     seconds(t.Elapsed),
		Cases:    make([]testcase, 0, len(s.Results)),
	}
	for _, r := range s.Results {
		out.Cases = append(out.Cases, caseOf(s, r))
	}
	return out
}

// caseOf renders one case.
func caseOf(s runner.SuiteResult, r runner.Result) testcase {
	out := testcase{
		Name:      r.Case.Name,
		Classname: s.Target.File.Flow,
		Time:      seconds(r.Outcome.Elapsed),
	}

	switch r.Status {
	case runner.Failed:
		out.Failure = &failure{
			Message: summarize(r.Failures),
			Type:    failureTypeAssertion,
			Detail:  strings.Join(r.Failures, "\n"),
		}
		// The reproduce command goes in system-out, where every reader shows it. Someone
		// reading a red build in a browser can copy the line that reproduces it locally,
		// which is the whole point of dolphin driving a real octo.
		out.SystemOut = r.Outcome.Command

	case runner.Errored:
		out.Error = &failure{
			Message: r.Err.Error(),
			Type:    errorTypeRun,
			Detail:  r.Err.Error(),
		}
		out.SystemOut = r.Outcome.Command

	case runner.Skipped:
		out.Skipped = &skipped{Message: r.Case.Skip}

	case runner.NotRun:
		// A case the run never reached is reported as skipped, which is what it is from
		// the reader's side — but with a reason that says it was not the suite's choice.
		out.Skipped = &skipped{Message: "not run: the run was cut short"}

	case runner.Passed:
	}
	return out
}

// summarize is the `message` attribute: the one line a dashboard shows in its LIST of
// tests, before anyone clicks through to the detail.
//
// It has to earn that line. Taking the first line of the first failure would give
// "expect.body:" for the commonest failure there is — a body diff, whose first line is a
// label and whose next two hold the whole story — which tells a reader scanning a red
// build precisely nothing. So the failure is flattened onto one line instead, and
// truncated: "expect.body: want: {…} got: {…}" says what broke at a glance.
func summarize(failures []string) string {
	if len(failures) == 0 {
		return "failed"
	}

	// Truncated by RUNE, not by byte: a body diff can hold anything a flow returned, and
	// cutting a multi-byte character in half would put an invalid one in the report.
	flat := []rune(strings.Join(strings.Fields(failures[0]), " "))
	if len(flat) > summaryLimit {
		return string(flat[:summaryLimit]) + "…"
	}
	return string(flat)
}

// seconds renders a duration the way JUnit wants it: a decimal number of seconds.
func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
