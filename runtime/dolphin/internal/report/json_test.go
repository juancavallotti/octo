package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/types"
)

// writeJSON renders the shared fixture and reads the document back as it was written.
func writeJSON(t *testing.T, report []runner.SuiteResult) map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.json")
	if err := JSON(path, report, 412*time.Millisecond, "/tmp/dolphin-123", "dolphin 9.9.9"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read the report: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the report is not valid JSON: %v\n%s", err, raw)
	}
	return doc
}

// cases returns the fixture's one suite's cases, keyed by name.
func cases(t *testing.T, doc map[string]any) map[string]map[string]any {
	t.Helper()
	suites, ok := doc["suites"].([]any)
	if !ok || len(suites) != 1 {
		t.Fatalf("suites = %v, want exactly one", doc["suites"])
	}
	out := map[string]map[string]any{}
	for _, c := range suites[0].(map[string]any)["cases"].([]any) {
		entry := c.(map[string]any)
		out[entry["name"].(string)] = entry
	}
	return out
}

// TestJSONNamesEveryStatus: the statuses are the whole point of the document, and the
// zero-value one is the trap. runner.Status is `NotRun = iota`, so a status marshalled as
// an int would write 0 for a case that never ran — the same thing a reader sees for an
// absent field, and the one value that must never be read as "passed".
func TestJSONNamesEveryStatus(t *testing.T) {
	got := cases(t, writeJSON(t, results()))

	for name, want := range map[string]string{
		"a small charge is captured":           statusPassed,
		"a declined card takes the error path": statusFailed,
		"it watches a block that is not there": statusErrored,
		"not ready":                            statusSkipped,
		"never reached":                        statusNotRun,
	} {
		if status := got[name]["status"]; status != want {
			t.Errorf("%q status = %v, want %q", name, status, want)
		}
	}
}

// TestJSONCarriesTheReproduceCommandForFailedAndErrored: a failing case hands back a
// runnable command, and so does one octo refused — the two are debugged the same way, and
// the console reports both.
func TestJSONCarriesTheReproduceCommandForFailedAndErrored(t *testing.T) {
	got := cases(t, writeJSON(t, results()))

	for _, name := range []string{
		"a declined card takes the error path",
		"it watches a block that is not there",
	} {
		if cmd, _ := got[name]["reproduce"].(string); !strings.HasPrefix(cmd, "octo invoke ") {
			t.Errorf("%q reproduce = %q, want the octo command", name, cmd)
		}
	}
}

// TestJSONSeparatesFailedFromErrored: a case that RAN and did not do what it said carries
// failures; one that never ran carries an error. Conflating them tells a reader the flow
// is broken when the suite is.
func TestJSONSeparatesFailedFromErrored(t *testing.T) {
	got := cases(t, writeJSON(t, results()))

	failed := got["a declined card takes the error path"]
	if _, ok := failed["error"]; ok {
		t.Errorf("a failed case carries error = %v, want none", failed["error"])
	}
	if n := len(failed["failures"].([]any)); n != 2 {
		t.Errorf("failed case failures = %d, want 2", n)
	}

	errored := got["it watches a block that is not there"]
	if _, ok := errored["failures"]; ok {
		t.Errorf("an errored case carries failures = %v, want none", errored["failures"])
	}
	if msg, _ := errored["error"].(string); !strings.Contains(msg, `no block "nope"`) {
		t.Errorf("errored case error = %q, want octo's complaint", msg)
	}
}

// TestJSONFailureSummaryIsOneLineAndDetailIsNot: a list row and a detail pane need
// different things from the same failure, which is why both are carried. The commonest
// failure is a body diff whose first line is only a label.
func TestJSONFailureSummaryIsOneLineAndDetailIsNot(t *testing.T) {
	got := cases(t, writeJSON(t, results()))

	first := got["a declined card takes the error path"]["failures"].([]any)[0].(map[string]any)
	summary := first["summary"].(string)
	detail := first["detail"].(string)

	if strings.Contains(summary, "\n") {
		t.Errorf("summary = %q, want one line", summary)
	}
	if !strings.Contains(summary, "want:") || !strings.Contains(summary, "got:") {
		t.Errorf("summary = %q, want the diff flattened into it", summary)
	}
	if !strings.Contains(detail, "\n") {
		t.Errorf("detail = %q, want the full multi-line text", detail)
	}
}

// TestJSONTotalsMatchTheTally: the document's own summary must agree with the tally every
// other reporter uses, or two readers of one run disagree about whether it passed.
func TestJSONTotalsMatchTheTally(t *testing.T) {
	doc := writeJSON(t, results())
	want := Tally(results())
	totals := doc["totals"].(map[string]any)

	for field, n := range map[string]int{
		"cases":   want.Cases(),
		"passed":  want.Passed,
		"failed":  want.Failed,
		"errored": want.Errored,
		"skipped": want.Skipped,
		"notRun":  want.NotRun,
	} {
		if got := int(totals[field].(float64)); got != n {
			t.Errorf("totals.%s = %d, want %d", field, got, n)
		}
	}
}

// TestJSONKeepsTheTwoClocksApart: totals.elapsedMs sums the cases while wallMs is the
// clock on the wall. Under --parallel they differ, and reporting one as the other would
// make the document lie about a run's cost.
func TestJSONKeepsTheTwoClocksApart(t *testing.T) {
	doc := writeJSON(t, results())

	if wall := int64(doc["wallMs"].(float64)); wall != 412 {
		t.Errorf("wallMs = %d, want 412", wall)
	}
	// The fixture's cases sum to 200ms, which is deliberately not the 412ms wall clock.
	if sum := int64(doc["totals"].(map[string]any)["elapsedMs"].(float64)); sum != 200 {
		t.Errorf("totals.elapsedMs = %d, want the 200 the cases sum to", sum)
	}
}

// TestJSONReportsWhatTheFlowDid: the outcome is what makes this document worth writing
// over the JUnit one — a reader holds the `expect` it authored and needs the `got`.
// It is carried for a PASSING case too, because promoting a run into a new test reads it.
func TestJSONReportsWhatTheFlowDid(t *testing.T) {
	report := results()
	report[0].Results[0].Outcome.Result = &types.Message{
		EventID: "evt-1",
		Body:    map[string]any{"status": "captured"},
	}
	report[0].Results[1].Outcome.Spies = map[string][]core.SpyRecord{
		"orders.charge": {{Seq: 1, Error: "card declined"}},
	}

	got := cases(t, writeJSON(t, report))

	passed := got["a small charge is captured"]["outcome"].(map[string]any)
	body := passed["result"].(map[string]any)["body"].(map[string]any)
	if body["status"] != "captured" {
		t.Errorf("passed case body = %v, want the flow's result", body)
	}

	failed := got["a declined card takes the error path"]["outcome"].(map[string]any)
	spies := failed["spies"].(map[string]any)["orders.charge"].([]any)
	if len(spies) != 1 {
		t.Fatalf("spies = %v, want the one crossing", spies)
	}
	if e := spies[0].(map[string]any)["error"]; e != "card declined" {
		t.Errorf("spy record error = %v, want the block's failure", e)
	}
}

// TestJSONOmitsTheOutcomeForACaseThatNeverRan: a skipped or not-run case has nothing to
// report, and an empty outcome object would invite a reader to believe otherwise.
func TestJSONOmitsTheOutcomeForACaseThatNeverRan(t *testing.T) {
	got := cases(t, writeJSON(t, results()))

	for _, name := range []string{"not ready", "never reached"} {
		if outcome, ok := got[name]["outcome"]; ok {
			t.Errorf("%q outcome = %v, want none", name, outcome)
		}
	}
	if reason := got["not ready"]["skip"]; reason != "the upstream contract is still moving" {
		t.Errorf("skip = %v, want the reason the case gave", reason)
	}
}

// TestJSONNamesTheBinaryThatWroteIt: the reader and the writer ship in one release but
// are DEPLOYED apart — a developer's dolphin can be weeks older than the editor driving
// it — so the report says which dolphin produced it, and a reader can warn about a pair
// that does not match the octo beside it.
func TestJSONNamesTheBinaryThatWroteIt(t *testing.T) {
	doc := writeJSON(t, results())

	if v := doc["dolphin"]; v != "dolphin 9.9.9" {
		t.Errorf("dolphin = %v, want the version line of the binary that wrote it", v)
	}
	if dir := doc["workDir"]; dir != "/tmp/dolphin-123" {
		t.Errorf("workDir = %v, want where the failing runs were left", dir)
	}
}

// TestJSONWritesAValidDocumentForAnEmptyRun: a reader iterating `suites` should not have
// to special-case a run that matched nothing.
func TestJSONWritesAValidDocumentForAnEmptyRun(t *testing.T) {
	doc := writeJSON(t, nil)

	suites, ok := doc["suites"].([]any)
	if !ok {
		t.Fatalf("suites = %v, want an empty array and not null", doc["suites"])
	}
	if len(suites) != 0 {
		t.Errorf("suites = %v, want empty", suites)
	}
	if n := int(doc["totals"].(map[string]any)["cases"].(float64)); n != 0 {
		t.Errorf("totals.cases = %d, want 0", n)
	}
}

// TestJSONIsReadableByTheUserWhoseTestFailed: the report is a build artifact someone
// opens, not a secret.
func TestJSONIsReadableByTheUserWhoseTestFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := JSON(path, results(), time.Second, "", "dolphin 9.9.9"); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != reportMode {
		t.Errorf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(reportMode))
	}
}

// TestStatusNameNeverReadsAnUnknownStatusAsPassed pins the defensive default: an
// unmapped value is reported as not-run, the safe reading.
func TestStatusNameNeverReadsAnUnknownStatusAsPassed(t *testing.T) {
	if got := statusName(runner.Status(99)); got != statusNotRun {
		t.Errorf("statusName(99) = %q, want %q", got, statusNotRun)
	}
}

// TestJSONSuiteNamesTheConfigItRanAgainst: a suite can be run against a config that is
// not beside it (--config), so the pairing has to be in the report or a reader cannot
// tell what was actually exercised.
func TestJSONSuiteNamesTheConfigItRanAgainst(t *testing.T) {
	doc := writeJSON(t, results())
	s := doc["suites"].([]any)[0].(map[string]any)

	if s["path"] != "flows/orders_test.yaml" {
		t.Errorf("path = %v, want the suite", s["path"])
	}
	if s["config"] != "flows/orders.yaml" {
		t.Errorf("config = %v, want the config it ran against", s["config"])
	}
	if s["flow"] != "charge-flowlevel" {
		t.Errorf("flow = %v, want the flow under test", s["flow"])
	}
}
