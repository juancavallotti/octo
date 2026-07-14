package runner

// This file schedules the cases. The scheduling is the whole of what makes a
// process-per-case affordable, and it rests on one fact about invoke mode:
//
//	a connector used only as a flow SOURCE is never started.
//
// The runtime replaces those with implicit sources (core/runtime: startConnectors),
// so ten concurrent octos over one config do not fight for port 8080 — the http
// connector that would bind it is not running in any of them. Connectors that are NOT
// sources do start, in every child, which is the one real hazard: a suite over a config
// with a database connector opens that database once per case. --parallel 1 is the
// answer, and the reason the flag exists.

import (
	"context"
	"runtime"
	"sync"

	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// Status is what became of a case.
type Status int

const (
	// NotRun means the case never started, because the run was cut short — --fail-fast,
	// or a Ctrl-C. It is deliberately the ZERO value: every result lands in a
	// preallocated slot, and if a bug ever left one unwritten, the safe thing for it to
	// read as is "never ran" rather than "passed".
	NotRun Status = iota
	// Passed means the flow did what the case said it would.
	Passed
	// Failed means it ran and an expectation did not hold. This is the only status that
	// says "the code under test is wrong".
	Failed
	// Errored means the case could not be run at all — a bad block address, a config
	// that does not parse, an octo that would not start. The suite is broken, not the
	// flow, and the two must not be reported as the same thing.
	Errored
	// Skipped means the case said not to run it.
	Skipped
)

// Result is one case, run.
type Result struct {
	Case    suite.Case
	Status  Status
	Outcome Outcome
	// Failures are the expectations that did not hold, in the order they were checked.
	// They are values rather than printed text: the worker that produced them never
	// touches stdout, which is what keeps a parallel run's output from interleaving
	// into mush.
	Failures []string
	// Err is why an Errored case could not run.
	Err error
}

// SuiteResult is one _test.yaml, run.
type SuiteResult struct {
	Target  suite.Target
	Results []Result
}

// Failed reports whether any case in the suite failed or errored.
func (s SuiteResult) Failed() bool {
	for _, r := range s.Results {
		if r.Status == Failed || r.Status == Errored {
			return true
		}
	}
	return false
}

// Checker decides whether an outcome is what a case expected. The runner takes it as a
// function rather than importing the assert package, so that running a case and judging
// it stay separable — and so this package can be tested without either.
type Checker func(suite.Case, Outcome) []string

// Config is a whole run.
type Config struct {
	Options
	// Parallel is how many cases may run at once. Zero means one per CPU.
	Parallel int
	// FailFast abandons the run after the first failing case.
	FailFast bool
	// Check judges an outcome against a case.
	Check Checker
	// OnSuiteDone is called once per suite, in file order, as soon as that suite is
	// complete and every suite before it has been reported. It is what gives a run live
	// per-file feedback without giving up a deterministic report.
	OnSuiteDone func(SuiteResult)
}

// work is one case, addressed by where its result belongs.
type work struct {
	suite, index int
}

// Run runs every case in every target and returns the results, in file order and then
// case order — whatever order they actually finished in.
//
// Determinism is not a nicety here. A test runner whose output reshuffles between runs
// is one nobody trusts, and a JUnit report that reorders is one that cannot be diffed.
// So results land in a slot addressed by (suite, case): each worker writes a distinct
// slot, which needs no mutex and no result channel, and the order falls out of the
// slots rather than out of the scheduling.
func Run(ctx context.Context, cfg Config, targets []suite.Target) []SuiteResult {
	results := make([][]Result, len(targets))
	queue := make([]work, 0)
	for s, target := range targets {
		// Every slot is preallocated, and its zero value is NotRun — so a case the run
		// never reached reads as never reached, rather than as a pass.
		results[s] = make([]Result, len(target.File.Cases))
		for i := range target.File.Cases {
			results[s][i] = Result{Case: target.File.Cases[i], Status: NotRun}
			queue = append(queue, work{suite: s, index: i})
		}
	}

	// A cancellable context of our own, so --fail-fast can stop this run without
	// cancelling the caller's.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	reporter := newReporter(targets, results, cfg.OnSuiteDone)
	var stop sync.Once

	jobs := dispatch(runCtx, queue)

	var wg sync.WaitGroup
	for range parallelism(cfg.Parallel) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runCases(runCtx, cfg, targets, results, jobs, reporter, func() { stop.Do(cancel) })
		}()
	}
	wg.Wait()

	// A run that was cut short leaves suites nobody announced. They are still part of
	// the report — a case that never ran is something the reader needs told, not
	// something to omit.
	reporter.flush()
	return collect(targets, results)
}

// dispatch hands the cases out, in order.
//
// They go through a channel rather than a goroutine each racing for a semaphore, and
// the difference is that they START in order: under --parallel 1 a run reads down the
// file the way it is written, and --fail-fast stops after the first failing case rather
// than after whichever case the scheduler happened to pick first.
func dispatch(ctx context.Context, queue []work) <-chan work {
	jobs := make(chan work)
	go func() {
		defer close(jobs)
		for _, item := range queue {
			select {
			case jobs <- item:
			case <-ctx.Done():
				return
			}
		}
	}()
	return jobs
}

// runCases takes cases off the queue until it is empty or the run is cut short. It
// writes each result into its own slot — one worker, one slot, so no two ever contend
// and no lock is needed.
func runCases(
	ctx context.Context,
	cfg Config,
	targets []suite.Target,
	results [][]Result,
	jobs <-chan work,
	reporter *reporter,
	failFast func(),
) {
	for item := range jobs {
		if ctx.Err() != nil {
			return // cut short: the cases left stay NotRun
		}

		target := targets[item.suite]
		c := target.File.Cases[item.index]
		result := runCase(ctx, cfg, target, item.index, c)
		results[item.suite][item.index] = result

		if cfg.FailFast && (result.Status == Failed || result.Status == Errored) {
			failFast()
		}
		reporter.done(item.suite)
	}
}

// runCase runs one case, or reports why it could not be.
func runCase(ctx context.Context, cfg Config, target suite.Target, index int, c suite.Case) Result {
	if c.Skip != "" {
		return Result{Case: c, Status: Skipped}
	}

	outcome, err := invoke(ctx, cfg.Options, target, index, c)
	if err != nil {
		return Result{Case: c, Status: Errored, Outcome: outcome, Err: err}
	}

	failures := cfg.Check(c, outcome)
	if len(failures) > 0 {
		return Result{Case: c, Status: Failed, Outcome: outcome, Failures: failures}
	}
	return Result{Case: c, Status: Passed, Outcome: outcome}
}

// reporter releases finished suites to the caller in file order.
//
// A suite is announced as soon as it is complete AND every suite before it has been
// announced — so a run gives live per-file feedback the way `go test` does, while the
// report still reads in the order the files were named rather than the order the
// scheduler happened to finish them in.
type reporter struct {
	mu        sync.Mutex
	targets   []suite.Target
	results   [][]Result
	remaining []int
	next      int
	notify    func(SuiteResult)
}

func newReporter(targets []suite.Target, results [][]Result, notify func(SuiteResult)) *reporter {
	remaining := make([]int, len(targets))
	for i, target := range targets {
		remaining[i] = len(target.File.Cases)
	}
	return &reporter{targets: targets, results: results, remaining: remaining, notify: notify}
}

// done records that one case of suite s has finished, and announces whatever that makes
// reportable.
func (r *reporter) done(s int) {
	if r.notify == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.remaining[s]--
	for r.next < len(r.targets) && r.remaining[r.next] == 0 {
		r.announce()
	}
}

// flush announces whatever is left, for a run that was cut short: those suites hold
// cases that never ran, and a reader needs to see them rather than have them omitted.
func (r *reporter) flush() {
	if r.notify == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for r.next < len(r.targets) {
		r.announce()
	}
}

// announce reports the next suite. The caller holds the lock.
func (r *reporter) announce() {
	r.notify(SuiteResult{Target: r.targets[r.next], Results: r.results[r.next]})
	r.next++
}

// collect assembles the final report.
func collect(targets []suite.Target, results [][]Result) []SuiteResult {
	report := make([]SuiteResult, len(targets))
	for i, target := range targets {
		report[i] = SuiteResult{Target: target, Results: results[i]}
	}
	return report
}

// parallelism is how many cases run at once: what was asked for, else one per CPU.
func parallelism(requested int) int {
	if requested > 0 {
		return requested
	}
	if cpus := runtime.NumCPU(); cpus > 0 {
		return cpus
	}
	return 1
}
