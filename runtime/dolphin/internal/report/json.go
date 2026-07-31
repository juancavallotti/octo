package report

// This file writes the report a PROGRAM reads. JUnit XML is for CI dashboards; this is
// for the editor's Testing tab and for MCP, which need the things a dashboard has no use
// for — what the flow actually returned, what each spy saw, which case is which.
//
// It goes to a file rather than stdout for the same reason the invoke envelope does: a
// flow with a `log` block writes to stdout too, and a report interleaved with a log line
// is not JSON. stdout is not the CLI's private channel.

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/types"
)

// The status names. They are strings and not runner.Status because that type's zero
// value is NotRun: marshalled as an int, a case whose status was never written would
// read as `0` — indistinguishable from an absent field, and the one value that must
// never be mistaken for "passed".
//
// Deliberately NOT a String() method on runner.Status: console.go already prints these
// as "FAIL"/"SKIP"/"NOT RUN", and one type with two official spellings is a trap.
const (
	statusPassed  = "passed"
	statusFailed  = "failed"
	statusErrored = "errored"
	statusSkipped = "skipped"
	statusNotRun  = "not-run"
)

// jsonReport is the document root.
//
// Grow it ADDITIVELY: a reader is a separately-built program (the editor, an MCP
// client), and a field that changes meaning under one is a bug it cannot see. There is
// no schema version here on purpose — octo is pre-release and the shape is still
// moving, so a version number would be ceremony that documents nothing. Add one when
// the format has to stay still for someone.
type jsonReport struct {
	// Dolphin is the version line of the binary that wrote this, so a reader can say
	// which one it is talking to.
	Dolphin string `json:"dolphin,omitempty"`
	// WallMs is how long the run actually took. Distinct from Totals.ElapsedMs, which
	// sums the cases: under --parallel > 1 the wall clock is shorter, and reporting one
	// as the other would make the document lie.
	WallMs int64 `json:"wallMs"`
	// WorkDir is where the failing runs were left, empty when nothing failed.
	WorkDir string      `json:"workDir,omitempty"`
	Totals  jsonTotals  `json:"totals"`
	Suites  []jsonSuite `json:"suites"`
}

// jsonTotals is what the run added up to.
type jsonTotals struct {
	Cases   int `json:"cases"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Errored int `json:"errored"`
	Skipped int `json:"skipped"`
	NotRun  int `json:"notRun"`
	// ElapsedMs is the SUM of the cases' own times — comparable between runs in a way
	// the wall clock is not. See Totals.Elapsed.
	ElapsedMs int64 `json:"elapsedMs"`
}

// jsonSuite is one _test.yaml.
type jsonSuite struct {
	Path      string     `json:"path"`
	Config    string     `json:"config"`
	Flow      string     `json:"flow"`
	ElapsedMs int64      `json:"elapsedMs"`
	Cases     []jsonCase `json:"cases"`
}

// jsonCase is one case, run or not.
type jsonCase struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	ElapsedMs int64  `json:"elapsedMs"`
	// Summary is the one line a list row shows, flattened from the first failure.
	Summary string `json:"summary,omitempty"`
	// Failures are the expectations that did not hold, in the order they were checked.
	Failures []jsonFailure `json:"failures,omitempty"`
	// Error is why an ERRORED case could not be run. A case that ran and failed an
	// expectation has Failures instead, and the two must not be conflated: one says the
	// suite is wrong, the other says the flow is.
	Error string `json:"error,omitempty"`
	// Stderr is octo's, carried only for an errored case — that is the one place where
	// the child's own complaint is the whole story. On a passing case it is noise.
	Stderr string `json:"stderr,omitempty"`
	// Skip is the reason a skipped case gave.
	Skip string `json:"skip,omitempty"`
	// Reproduce is the octo invocation that produced this, ready to paste into a shell.
	Reproduce string `json:"reproduce,omitempty"`
	// Outcome is what the flow actually did. Present for every case that RAN — not only
	// the failures — because a passing case's output is what a caller promoting a run
	// into a new test reads.
	Outcome *jsonOutcome `json:"outcome,omitempty"`
}

// jsonFailure is one expectation that did not hold: the line a list shows, and the full
// text underneath it.
type jsonFailure struct {
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

// jsonOutcome is what octo reported, as distinct from what the case wanted. Rendering
// want-vs-got is the reader's job: it already holds the `expect` it wrote.
type jsonOutcome struct {
	Result  *types.Message              `json:"result,omitempty"`
	Dropped bool                        `json:"dropped,omitempty"`
	Error   string                      `json:"error,omitempty"`
	Spies   map[string][]core.SpyRecord `json:"spies,omitempty"`
}

// JSON writes report to path as the machine-readable document described above. wall is
// how long the whole run took, and workDir where the failing runs were left ("" when
// they were cleaned up).
func JSON(path string, report []runner.SuiteResult, wall time.Duration, workDir, version string) error {
	doc := jsonDocument(report, wall, workDir, version)

	// Indented: a person reads this too, when a test behaves oddly and they go looking
	// at what the runner actually saw.
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the json report: %w", err)
	}
	body = append(body, '\n')

	if err := os.WriteFile(path, body, reportMode); err != nil {
		return fmt.Errorf("write the json report to %s: %w", path, err)
	}
	return nil
}

// jsonDocument builds the report.
func jsonDocument(report []runner.SuiteResult, wall time.Duration, workDir, version string) jsonReport {
	total := Tally(report)
	doc := jsonReport{
		Dolphin: version,
		WallMs:  millis(wall),
		WorkDir: workDir,
		Totals: jsonTotals{
			Cases:     total.Cases(),
			Passed:    total.Passed,
			Failed:    total.Failed,
			Errored:   total.Errored,
			Skipped:   total.Skipped,
			NotRun:    total.NotRun,
			ElapsedMs: millis(total.Elapsed),
		},
		// Never nil: a reader iterating `suites` should not have to special-case a run
		// that matched no suites.
		Suites: make([]jsonSuite, 0, len(report)),
	}
	for _, s := range report {
		doc.Suites = append(doc.Suites, jsonSuiteOf(s))
	}
	return doc
}

// jsonSuiteOf renders one suite.
func jsonSuiteOf(s runner.SuiteResult) jsonSuite {
	out := jsonSuite{
		Path:      s.Target.File.Path,
		Config:    s.Target.Config,
		Flow:      s.Target.File.Flow,
		ElapsedMs: millis(Tally([]runner.SuiteResult{s}).Elapsed),
		Cases:     make([]jsonCase, 0, len(s.Results)),
	}
	for _, r := range s.Results {
		out.Cases = append(out.Cases, jsonCaseOf(r))
	}
	return out
}

// jsonCaseOf renders one case.
func jsonCaseOf(r runner.Result) jsonCase {
	out := jsonCase{
		Name:      r.Case.Name,
		Status:    statusName(r.Status),
		ElapsedMs: millis(r.Outcome.Elapsed),
		Reproduce: r.Outcome.Command,
	}

	switch r.Status {
	case runner.Failed:
		out.Summary = summarize(r.Failures)
		out.Failures = failuresOf(r.Failures)
		out.Outcome = outcomeOf(r)

	case runner.Errored:
		if r.Err != nil {
			out.Summary = r.Err.Error()
			out.Error = r.Err.Error()
		}
		out.Stderr = r.Outcome.Stderr
		out.Outcome = outcomeOf(r)

	case runner.Skipped:
		out.Skip = r.Case.Skip

	case runner.Passed:
		out.Outcome = outcomeOf(r)

	case runner.NotRun:
		// Nothing ran, so there is nothing to report but the name and the status.
	}
	return out
}

// failuresOf renders the expectations that did not hold. Each carries both the flat line
// a list row shows and the full text — the same two forms JUnit's message/detail pair
// carries, because a want/got diff is three lines and neither form serves both places.
func failuresOf(failures []string) []jsonFailure {
	out := make([]jsonFailure, 0, len(failures))
	for _, f := range failures {
		out = append(out, jsonFailure{Summary: summarize([]string{f}), Detail: f})
	}
	return out
}

// outcomeOf renders what the flow did, or nil when there is nothing to say.
func outcomeOf(r runner.Result) *jsonOutcome {
	o := r.Outcome
	if o.Result == nil && !o.Dropped && o.Error == "" && len(o.Spies) == 0 {
		return nil
	}
	return &jsonOutcome{
		Result:  o.Result,
		Dropped: o.Dropped,
		Error:   o.Error,
		Spies:   o.Spies,
	}
}

// statusName is the wire name of a status. An unknown value reports as not-run, which is
// the safe reading: a status nobody wrote must never come back as "passed".
func statusName(s runner.Status) string {
	switch s {
	case runner.Passed:
		return statusPassed
	case runner.Failed:
		return statusFailed
	case runner.Errored:
		return statusErrored
	case runner.Skipped:
		return statusSkipped
	case runner.NotRun:
		return statusNotRun
	default:
		return statusNotRun
	}
}

// millis renders a duration as whole milliseconds, which is the resolution a reader
// displays and finer than a test run is meaningfully measured to.
func millis(d time.Duration) int64 {
	return d.Milliseconds()
}
