package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// fakeOcto writes a script standing in for the octo binary, so the runner can be tested
// without one. body is the shell body; it may print an envelope on stdout and exit how
// it likes.
func fakeOcto(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake octo is a shell script")
	}

	path := filepath.Join(t.TempDir(), "octo")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // G306: it must be executable
		t.Fatalf("write fake octo: %v", err)
	}
	return path
}

// targets builds n suites of caseCount cases each, named so a test can tell them apart.
func targets(t *testing.T, n, caseCount int) []suite.Target {
	t.Helper()
	out := make([]suite.Target, 0, n)
	for s := range n {
		cases := make([]suite.Case, 0, caseCount)
		for i := range caseCount {
			cases = append(cases, suite.Case{Name: fmt.Sprintf("s%d-case%d", s, i)})
		}
		out = append(out, suite.Target{
			Config: "flows.yaml",
			File: suite.File{
				Flow:  "orders",
				Path:  fmt.Sprintf("s%d_test.yaml", s),
				Cases: cases,
			},
		})
	}
	return out
}

// config is a run against the fake octo, with a checker that passes everything.
func config(t *testing.T, octo string) Config {
	t.Helper()
	return Config{
		Options: Options{Octo: octo, WorkDir: t.TempDir()},
		Check:   func(suite.Case, Outcome) []string { return nil },
	}
}

// statuses renders the report as "case:status", flattened in report order.
func statuses(report []SuiteResult) []string {
	var out []string
	for _, s := range report {
		for _, r := range s.Results {
			out = append(out, fmt.Sprintf("%s:%s", r.Case.Name, r.Status))
		}
	}
	return out
}

func (s Status) String() string {
	switch s {
	case NotRun:
		return "notrun"
	case Passed:
		return "pass"
	case Failed:
		return "fail"
	case Errored:
		return "error"
	case Skipped:
		return "skip"
	default:
		return "?"
	}
}

// TestNotRunIsTheZeroStatus. Every result lands in a preallocated slot, so the zero
// value is what an unwritten one reads as — and the only safe thing for that to mean is
// "never ran". If it meant "passed", a bug that skipped a case would turn it green.
func TestNotRunIsTheZeroStatus(t *testing.T) {
	var zero Status
	if zero != NotRun {
		t.Errorf("the zero Status is %s, want notrun: an unwritten result must never read as a pass", zero)
	}
}

// emits is a fake octo body that answers with envelope, the way the real one does: in the
// file named by --envelope-out, NOT on stdout.
//
// That is the contract, and it is worth a fake this fussy. stdout belongs to the flow —
// a `log` block over a logger connector writes its JSON there — so an envelope printed to
// stdout would be interleaved with the flow's own output, and dolphin would be parsing
// whatever came out. A fake that printed the envelope on stdout would be testing a
// protocol nobody speaks.
func emits(envelope string) string {
	return `
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--envelope-out" ]; then out="$arg"; fi
  prev="$arg"
done
printf '%s\n' '` + envelope + `' > "$out"`
}

var okEnvelope = emits(`{"result":{"event_id":"e1","body":{"ok":true}}}`)

// TestRunReportsInFileOrderNoMatterTheFinishingOrder is the property the whole
// scheduler exists to preserve.
//
// The fake octo sleeps longer the EARLIER the case, so cases finish in almost exactly
// the reverse of the order they were queued in. The report must still read in file
// order, then case order: a test runner whose output reshuffles between runs is one
// nobody trusts, and a JUnit report that reorders cannot be diffed.
func TestRunReportsInFileOrderNoMatterTheFinishingOrder(t *testing.T) {
	// The case index is in the debug config's filename, which is the last --run-debug-config
	// argument; sleep on it so case 0 finishes last.
	octo := fakeOcto(t, `
for arg in "$@"; do
  case "$arg" in *.yaml) last="$arg";; esac
done
n=$(basename "$last" | sed 's/[^0-9]*\([0-9]*\)\.yaml/\1/')
sleep 0.$(( 9 - ${n:-0} ))
`+okEnvelope)

	report := Run(context.Background(), config(t, octo), targets(t, 2, 4))

	got := strings.Join(statuses(report), " ")
	want := "s0-case0:pass s0-case1:pass s0-case2:pass s0-case3:pass " +
		"s1-case0:pass s1-case1:pass s1-case2:pass s1-case3:pass"
	if got != want {
		t.Errorf("report order = %q,\nwant             %q", got, want)
	}
}

// TestSuitesAreAnnouncedInFileOrder: a run gives live per-file feedback as `go test`
// does, but a suite is only announced once every suite BEFORE it has been — otherwise
// the fast files would jump the queue and the output would not match the report.
func TestSuitesAreAnnouncedInFileOrder(t *testing.T) {
	octo := fakeOcto(t, `
for arg in "$@"; do
  case "$arg" in *_test.*.yaml) last="$arg";; esac
done
case "$last" in *s0*) sleep 0.4;; esac
`+okEnvelope)

	var mu sync.Mutex
	var announced []string
	cfg := config(t, octo)
	cfg.OnSuiteDone = func(s SuiteResult) {
		mu.Lock()
		defer mu.Unlock()
		announced = append(announced, s.Target.File.Path)
	}

	// s0 is slow, s1 and s2 are fast: without the ordering rule they would be announced
	// first.
	Run(context.Background(), cfg, targets(t, 3, 2))

	got := strings.Join(announced, " ")
	if want := "s0_test.yaml s1_test.yaml s2_test.yaml"; got != want {
		t.Errorf("announced %q, want %q — a suite must wait for the ones before it", got, want)
	}
}

// TestParallelRespectsTheLimit: --parallel 1 is the escape hatch for a config whose
// non-source connectors touch something shared (a database file), so it has to actually
// serialize.
func TestParallelRespectsTheLimit(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "concurrent")

	// Each child appends a byte on entry and truncates on exit; the high-water mark is
	// the file's greatest length. Crude, but it needs no coordination with the parent.
	octo := fakeOcto(t, fmt.Sprintf(`
printf x >> %[1]s
wc -c < %[1]s | tr -d ' ' >> %[1]s.peak
sleep 0.05
printf '' > %[1]s
`, counter)+"\n"+okEnvelope)

	if err := os.WriteFile(counter, nil, 0o600); err != nil {
		t.Fatalf("seed counter: %v", err)
	}

	cfg := config(t, octo)
	cfg.Parallel = 1
	Run(context.Background(), cfg, targets(t, 1, 4))

	peaks, err := os.ReadFile(counter + ".peak") //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read peaks: %v", err)
	}
	for _, line := range strings.Fields(string(peaks)) {
		if line != "1" {
			t.Errorf("saw %s concurrent octos under --parallel 1, want 1 (peaks: %q)", line, peaks)
		}
	}
}

// TestSkippedCasesNeverSpawnOcto: a skipped case must not run the flow. Spawning it and
// discarding the result would make `skip` a lie, and would still hit whatever the flow
// touches.
func TestSkippedCasesNeverSpawnOcto(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "spawned")
	octo := fakeOcto(t, fmt.Sprintf("printf x >> %s\n", marker)+okEnvelope)

	tg := targets(t, 1, 2)
	tg[0].File.Cases[0].Skip = "the upstream contract is still moving"

	report := Run(context.Background(), config(t, octo), tg)

	if got := statuses(report); strings.Join(got, " ") != "s0-case0:skip s0-case1:pass" {
		t.Errorf("statuses = %v", got)
	}
	spawned, err := os.ReadFile(marker) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if len(spawned) != 1 {
		t.Errorf("octo was spawned %d times, want 1: a skipped case must not run the flow", len(spawned))
	}
}

// TestAFlowFailureIsAResultNotAnError draws the line --envelope exists to draw. octo
// exits 0 and reports the failure in the envelope, so the case RAN — it is the checker's
// business whether "the flow failed" is what the case wanted.
func TestAFlowFailureIsAResultNotAnError(t *testing.T) {
	octo := fakeOcto(t, emits(`{"error":"block \"charge\": card declined"}`))

	cfg := config(t, octo)
	cfg.Check = func(_ suite.Case, o Outcome) []string {
		if o.Error == "" {
			return []string{"expected the flow to fail"}
		}
		return nil
	}

	report := Run(context.Background(), cfg, targets(t, 1, 1))
	result := report[0].Results[0]

	if result.Status != Passed {
		t.Errorf("status = %s, want pass: a flow that failed is a result the case can expect", result.Status)
	}
	if !strings.Contains(result.Outcome.Error, "card declined") {
		t.Errorf("outcome.Error = %q, want the flow's failure", result.Outcome.Error)
	}
}

// TestABadRequestErrorsTheCase: octo exiting non-zero means it would not run at all — an
// unresolvable block address, a config that does not parse. That is the SUITE being
// broken, not the flow, and it must not be reported as a failing test.
//
// It also pins the unwrapping. octo is a service, so it reports a fatal error through
// slog, as a structured line. Reprinting that line verbatim would bury the one part the
// user needs — including, here, the list of blocks that DO exist, which is the answer to
// the question they are about to ask — inside logging machinery they did not ask about.
func TestABadRequestErrorsTheCase(t *testing.T) {
	octo := fakeOcto(t, `
echo 'time=2026-07-13T20:15:18.646-07:00 level=WARN msg="something earlier"' >&2
echo 'time=2026-07-13T20:15:18.646-07:00 level=ERROR msg="cli stopped with error" '\
'error="spy \"demo.nope\": no block \"nope\" in that chain (blocks: seed-orders, charge)"' >&2
exit 1`)

	report := Run(context.Background(), config(t, octo), targets(t, 1, 1))
	result := report[0].Results[0]

	if result.Status != Errored {
		t.Fatalf("status = %s, want error: octo refused the run, so nothing was tested", result.Status)
	}

	got := result.Err.Error()
	want := `octo rejected the run: spy "demo.nope": no block "nope" in that chain (blocks: seed-orders, charge)`
	if got != want {
		t.Errorf("err  = %q\nwant = %q", got, want)
	}
}

// TestOctoErrorFallsBackToTheRawLine: the slog format is a guess worth making, and worth
// abandoning the moment it does not hold. A stderr that carries no error= field is
// printed as it is, rather than swallowed.
func TestOctoErrorFallsBackToTheRawLine(t *testing.T) {
	octo := fakeOcto(t, `echo 'panic: something nobody predicted' >&2; exit 2`)

	report := Run(context.Background(), config(t, octo), targets(t, 1, 1))

	if got := report[0].Results[0].Err.Error(); !strings.Contains(got, "panic: something nobody predicted") {
		t.Errorf("err = %q, want the raw line kept when it is not a slog line", got)
	}
}

// TestCasesStartInOrder: under --parallel 1 a run reads down the file the way it is
// written. This is why cases are handed out through a channel rather than by spawning a
// goroutine each and letting them race for a semaphore — with the race, "the first
// case" meant whichever one the scheduler happened to pick, which also made --fail-fast
// cut the run at an arbitrary point.
func TestCasesStartInOrder(t *testing.T) {
	order := filepath.Join(t.TempDir(), "order")
	octo := fakeOcto(t, fmt.Sprintf(`
for arg in "$@"; do
  case "$arg" in *_test.*.yaml) printf '%%s ' "$(basename "$arg")" >> %s;; esac
done
`, order)+okEnvelope)

	cfg := config(t, octo)
	cfg.Parallel = 1
	Run(context.Background(), cfg, targets(t, 1, 4))

	started, err := os.ReadFile(order) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read order: %v", err)
	}
	want := "s0_test.0.yaml s0_test.1.yaml s0_test.2.yaml s0_test.3.yaml "
	if string(started) != want {
		t.Errorf("cases started as %q,\nwant                %q", started, want)
	}
}

// TestFailFastStopsTheRun: the cases after the failure never run at all — they are
// NotRun, not errored, and certainly not passed. Serialized, so "the first case" is the
// first case.
func TestFailFastStopsTheRun(t *testing.T) {
	octo := fakeOcto(t, okEnvelope)

	cfg := config(t, octo)
	cfg.FailFast = true
	cfg.Parallel = 1
	cfg.Check = func(c suite.Case, _ Outcome) []string {
		if c.Name == "s0-case0" {
			return []string{"nope"}
		}
		return nil
	}

	report := Run(context.Background(), cfg, targets(t, 1, 4))

	got := strings.Join(statuses(report), " ")
	want := "s0-case0:fail s0-case1:notrun s0-case2:notrun s0-case3:notrun"
	if got != want {
		t.Errorf("statuses = %q,\nwant       %q", got, want)
	}
}

// TestACutShortRunStillAnnouncesEverySuite: --fail-fast leaves suites nobody finished.
// They are still part of the report — a case that never ran is something the reader
// needs told, not something to omit.
func TestACutShortRunStillAnnouncesEverySuite(t *testing.T) {
	octo := fakeOcto(t, okEnvelope)

	var mu sync.Mutex
	var announced []string
	cfg := config(t, octo)
	cfg.FailFast = true
	cfg.Parallel = 1
	cfg.Check = func(suite.Case, Outcome) []string { return []string{"nope"} }
	cfg.OnSuiteDone = func(s SuiteResult) {
		mu.Lock()
		defer mu.Unlock()
		announced = append(announced, s.Target.File.Path)
	}

	Run(context.Background(), cfg, targets(t, 3, 2))

	if got := strings.Join(announced, " "); got != "s0_test.yaml s1_test.yaml s2_test.yaml" {
		t.Errorf("announced %q, want every suite even though the run was cut short", got)
	}
}

// TestTheCommandIsRunnable: the reproduce command a failure prints must be a command,
// not a description of one. Block addresses carry brackets, which a shell would glob, so
// they have to come back quoted.
func TestTheCommandIsRunnable(t *testing.T) {
	octo := fakeOcto(t, okEnvelope)

	tg := targets(t, 1, 1)
	tg[0].File.Cases[0].Spies = map[string]suite.SpyExpect{
		"demo.each-order[body].classify-order": {},
	}

	report := Run(context.Background(), config(t, octo), tg)
	cmd := report[0].Results[0].Outcome.Command

	for _, want := range []string{"invoke", "--config flows.yaml", "--flow orders", "--envelope", "--run-debug-config"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q is missing %q", cmd, want)
		}
	}
}

// TestSpiesComeBackKeyedByAddress.
func TestSpiesComeBackKeyedByAddress(t *testing.T) {
	octo := fakeOcto(t,
		emits(`{"result":{"event_id":"e1"},"spies":[{"address":"demo.charge","records":[{"seq":1},{"seq":2}]}]}`))

	report := Run(context.Background(), config(t, octo), targets(t, 1, 1))
	spies := report[0].Results[0].Outcome.Spies

	if len(spies["demo.charge"]) != 2 {
		t.Errorf("spies = %v, want two records for demo.charge", spies)
	}
}

// TestGarbageInTheEnvelopeIsAnError: octo answering with something dolphin cannot read
// must never be mistaken for a passing case.
func TestGarbageInTheEnvelopeIsAnError(t *testing.T) {
	octo := fakeOcto(t, emits(`not json`))

	report := Run(context.Background(), config(t, octo), targets(t, 1, 1))
	result := report[0].Results[0]

	if result.Status != Errored {
		t.Errorf("status = %s, want error", result.Status)
	}
	if !strings.Contains(result.Err.Error(), "read octo's outcome") {
		t.Errorf("err = %v", result.Err)
	}
}

// TestTheFlowsOwnStdoutIsNotTheAnswer is the regression that moved the envelope into a
// file. The fake here does what a real flow with a `log` block does — writes its own JSON
// to stdout — and answers in the file besides. The case must pass: what the flow printed
// is the flow's business, and dolphin must not be reading it.
func TestTheFlowsOwnStdoutIsNotTheAnswer(t *testing.T) {
	octo := fakeOcto(t, `echo '{"time":"2026-07-13T20:44:34Z","level":"INFO","msg":"order A-1"}'`+
		okEnvelope)

	report := Run(context.Background(), config(t, octo), targets(t, 1, 1))
	result := report[0].Results[0]

	if result.Status != Passed {
		t.Errorf("status = %s (err: %v), want pass: a flow that logs to stdout is not a broken flow",
			result.Status, result.Err)
	}
}

// TestTheSuitesEnvBeatsTheAmbientOne.
//
// A suite says `env: {ANTHROPIC_API_KEY: test-key}` because it wants a FAKE key — that is
// the entire point of writing one down. If the developer's real key, exported in their
// shell, won over it, the test would quietly call the live API: it would pass on their
// machine, bill them for it, and fail in CI where no such variable exists. The suite wins.
func TestTheSuitesEnvBeatsTheAmbientOne(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "the-developers-real-key")

	seen := filepath.Join(t.TempDir(), "seen")
	octo := fakeOcto(t, fmt.Sprintf("printf '%%s' \"$ANTHROPIC_API_KEY\" > %s\n", seen)+okEnvelope)

	tg := targets(t, 1, 1)
	tg[0].File.Env = map[string]string{"ANTHROPIC_API_KEY": "test-key"}

	Run(context.Background(), config(t, octo), tg)

	got, err := os.ReadFile(seen) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read what the child saw: %v", err)
	}
	if string(got) != "test-key" {
		t.Errorf("the child saw ANTHROPIC_API_KEY=%q, want the suite's test-key — "+
			"a real key from the shell must never leak into a test", got)
	}
}

// fakeOctoResolvingGreeting stands in for octo just enough to test env precedence: it
// resolves GREETING the way octo does — the real environment first, then whatever
// $OCTO_ENV_FILE holds — and writes what it resolved to seen. So a child that inherited an
// exported GREETING reports that, and one that did not falls through to the --env-file,
// exactly as the real runtime's lookupEnv would.
func fakeOctoResolvingGreeting(t *testing.T, seen string) string {
	t.Helper()
	body := fmt.Sprintf(`
if [ -n "${GREETING+set}" ]; then
  printf '%%s' "$GREETING" > %[1]s
elif [ -n "$OCTO_ENV_FILE" ]; then
  sed -n 's/^GREETING=//p' "$OCTO_ENV_FILE" | head -1 | tr -d '\n' > %[1]s
else
  : > %[1]s
fi
`, seen)
	return fakeOcto(t, body+okEnvelope)
}

// TestTheEnvFileIsStillHandedToOcto: dolphin does not inject the file's values as real
// variables — that would let --env-file outrank a config's own env resources, which octo
// deliberately ranks above its .env chain. Instead the path is still passed as
// $OCTO_ENV_FILE, so octo loads it into that chain and resources keep winning.
func TestTheEnvFileIsStillHandedToOcto(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "probe.env")
	if err := os.WriteFile(envFile, []byte("GREETING=from-env-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	seen := filepath.Join(t.TempDir(), "seen")
	octo := fakeOcto(t, fmt.Sprintf("printf '%%s' \"$OCTO_ENV_FILE\" > %s\n", seen)+okEnvelope)

	cfg := config(t, octo)
	cfg.EnvFile = envFile
	Run(context.Background(), cfg, targets(t, 1, 1))

	got, err := os.ReadFile(seen) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read what the child saw: %v", err)
	}
	if string(got) != envFile {
		t.Errorf("the child saw OCTO_ENV_FILE=%q, want the --env-file path", got)
	}
}

// TestTheEnvFileBeatsAnExportedVariable pins the precedence dolphin documents but did not
// used to honour. octo ranks the OS environment above its .env chain, so an exported
// variable quietly beat the --env-file the run was told to use, and a suite leaning on the
// file for a fake credential could run against the developer's real one. dolphin now drops
// the exported value so the file's reaches octo instead.
func TestTheEnvFileBeatsAnExportedVariable(t *testing.T) {
	t.Setenv("GREETING", "REAL-SHELL-VALUE")

	envFile := filepath.Join(t.TempDir(), "probe.env")
	if err := os.WriteFile(envFile, []byte("GREETING=from-env-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	seen := filepath.Join(t.TempDir(), "seen")
	octo := fakeOctoResolvingGreeting(t, seen)

	cfg := config(t, octo)
	cfg.EnvFile = envFile
	Run(context.Background(), cfg, targets(t, 1, 1))

	got, err := os.ReadFile(seen) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read what the child saw: %v", err)
	}
	if string(got) != "from-env-file" {
		t.Errorf("the child saw GREETING=%q, want the --env-file's value — an exported "+
			"variable must never beat the file a test runner was told to use", got)
	}
}

// TestTheSuitesEnvBeatsTheEnvFile: the suite's own env: block is the last word, above even
// the --env-file, so a suite can always pin a value regardless of what the file carries and
// regardless of the ambient environment.
func TestTheSuitesEnvBeatsTheEnvFile(t *testing.T) {
	t.Setenv("GREETING", "REAL-SHELL-VALUE")

	envFile := filepath.Join(t.TempDir(), "probe.env")
	if err := os.WriteFile(envFile, []byte("GREETING=from-env-file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	seen := filepath.Join(t.TempDir(), "seen")
	octo := fakeOctoResolvingGreeting(t, seen)

	cfg := config(t, octo)
	cfg.EnvFile = envFile
	tg := targets(t, 1, 1)
	tg[0].File.Env = map[string]string{"GREETING": "from-suite-env"}
	Run(context.Background(), cfg, tg)

	got, err := os.ReadFile(seen) //nolint:gosec // G304: a path this test built
	if err != nil {
		t.Fatalf("read what the child saw: %v", err)
	}
	if string(got) != "from-suite-env" {
		t.Errorf("the child saw GREETING=%q, want the suite's env: value", got)
	}
}
