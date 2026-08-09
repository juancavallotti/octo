package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/engine"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// defaultWorkers and defaultBuffer are floors, not the value used outright:
	// resolveWorkers derives from GOMAXPROCS above this floor, and
	// resolveBuffer's floor tracks the resolved worker count instead.
	defaultWorkers = 8
	defaultBuffer  = 64
	// workersPerProc scales the derived worker default: max(8, GOMAXPROCS*4)
	// gives 32 workers on an 8-core host — headroom for a blocking flow without
	// being wildly oversubscribed for a compute-bound one.
	workersPerProc = 4
)

// boundFlow is a runnable top-level pipeline: a source feeding a root Flow, run
// by a dedicated pool of workers all reading the same channel. It is assembled
// by the Service.
type boundFlow struct {
	name   string
	source core.MessageSource
	root   *engine.Flow
	// errorPath is the flow-level error chain, run when root fails. It is nil when
	// the flow declares no error path. On success its output becomes the result
	// (recovery); the failing error is exposed to it as vars.error.
	errorPath *engine.Flow
	workers   int
	// workersDerived is true when workers came from the GOMAXPROCS-derived
	// default rather than the flow's own config — surfaced only for the
	// startup log's "derived"/"configured" marker.
	workersDerived bool
	in             chan *types.Message
	bus            *core.EventBus
	pool           *pool.Pool
	wg             sync.WaitGroup
	// inflight counts the resumed tails that have not finished. A tail may resume
	// again — a split whose elements reach an aggregate is the ordinary case — so
	// draining the source workers says nothing about whether the continuation tree
	// below them is done. Shutdown waits on this before the pool is torn down.
	inflight sync.WaitGroup
	// implicit is true when the flow is driven by an implicit source (it is
	// callable by name and acquires no external resources). Implicit flows start
	// before source-backed ones so they are registered before any real source
	// begins admitting traffic that may flow-ref them.
	implicit bool
	// sourceDesc is a human-readable description of the flow's source, for logs.
	sourceDesc string
	// ctx is the flow's own context, captured at start. Resumed tails run under it
	// (with cancellation stripped) so they carry the runtime services and are not
	// tied to the lifetime of whichever message scheduled them.
	ctx context.Context //nolint:containedctx // captured at start for resumed tails
}

// resolveWorkers returns the configured worker count, or — when unconfigured —
// max(defaultWorkers, gomaxprocs*workersPerProc), so a flow scales with the
// host/container instead of a flat constant. gomaxprocs is a parameter rather
// than read from the stdlib runtime package here, so this stays a pure,
// table-testable function; the one real call site passes the live value.
func resolveWorkers(configured, gomaxprocs int) int {
	if configured > 0 {
		return configured
	}
	return max(defaultWorkers, gomaxprocs*workersPerProc)
}

// resolveBuffer returns the configured channel depth, or — when unconfigured —
// max(defaultBuffer, workers), so the source channel is never shallower than
// the pool of workers reading it.
func resolveBuffer(configured, workers int) int {
	if configured > 0 {
		return configured
	}
	return max(defaultBuffer, workers)
}

// workersOrigin labels the startup log with whether a flow's worker count came
// from its own config or from the GOMAXPROCS-derived default.
func workersOrigin(derived bool) string {
	if derived {
		return "derived"
	}
	return "configured"
}

// start starts the shared pool, spawns the worker pool, starts any block that
// owns background work, and then starts the source. Everything a message can
// touch is ready before the source produces one.
func (bf *boundFlow) start(ctx context.Context) error {
	// Captured so a resumed tail can run under the flow's own context — and so
	// the runtime services on it — rather than under the context of whichever
	// message happened to schedule it.
	bf.ctx = ctx
	bf.pool.Start()
	bf.wg.Add(bf.workers)
	for range bf.workers {
		go bf.worker(ctx)
	}
	if err := bf.startProcessors(ctx); err != nil {
		return err
	}
	return bf.source.Start(ctx)
}

// stop shuts the flow down from the front: nothing new arrives, then everything
// already in flight is allowed to finish, and only then is the pool torn down.
//
// Every step is load-bearing, and the middle two are the ones that are easy to
// get wrong. Draining the source workers is not enough, because a block that
// took the flow over has scheduled work that outlives the message it came from —
// and that work schedules more of its own, which is exactly what a split feeding
// an aggregate does. Quieting the blocks first stops any *new* work appearing
// from a reaper's timer; waiting on inflight then lets the existing tree finish.
//
// No step is skipped because an earlier one failed. Errors are collected and the
// shutdown runs to the end: a source that fails to stop is a reason to report a
// problem, not a reason to strand the workers on a channel nobody will close,
// leave a reaper firing at a torn-down flow, and never reclaim the pool.
func (bf *boundFlow) stop(ctx context.Context) error {
	sourceErr := bf.source.Stop(ctx)
	close(bf.in)
	bf.wg.Wait()
	blockErr := bf.stopProcessors(ctx)
	bf.inflight.Wait()
	bf.pool.Stop()
	return errors.Join(sourceErr, blockErr)
}

// startProcessors starts every root-level block that owns background work, in
// both the process and the error chain.
func (bf *boundFlow) startProcessors(ctx context.Context) error {
	for _, proc := range bf.lifecycleProcessors() {
		if err := proc.Start(ctx); err != nil {
			return fmt.Errorf("flow %q: start block: %w", bf.name, err)
		}
	}
	return nil
}

// stopProcessors stops them all, collecting rather than short-circuiting on the
// first error: one block failing to stop must not leave the others running.
func (bf *boundFlow) stopProcessors(ctx context.Context) error {
	var errs []error
	for _, proc := range bf.lifecycleProcessors() {
		if err := proc.Stop(ctx); err != nil {
			errs = append(errs, fmt.Errorf("flow %q: stop block: %w", bf.name, err))
		}
	}
	return errors.Join(errs...)
}

// lifecycleProcessors collects the blocks with background work from both root
// chains. Only root-level blocks are visited: a composite's sub-flows are its
// own business, and the blocks that need a lifecycle today are exactly the ones
// that take over the flow, which may only appear in a root chain anyway.
func (bf *boundFlow) lifecycleProcessors() []core.LifecycleProcessor {
	procs := bf.root.LifecycleProcessors()
	if bf.errorPath != nil {
		procs = append(procs, bf.errorPath.LifecycleProcessors()...)
	}
	return procs
}

// worker processes messages until the channel is closed and drained.
func (bf *boundFlow) worker(ctx context.Context) {
	defer bf.wg.Done()
	for msg := range bf.in {
		bf.handle(ctx, msg)
	}
}

// handle runs one message through the whole root flow.
func (bf *boundFlow) handle(ctx context.Context, msg *types.Message) {
	bf.run(ctx, msg, bf.root)
}

// resume runs msg through tail — the rest of a flow, handed to a block that took
// over execution. It is the hook the engine's continuations call, installed on
// each root chain at build time.
//
// The work is detached from the context that scheduled it. A block that takes
// the flow over has already accepted responsibility for the message and told the
// caller so; abandoning it because the originating request went away would make
// the acceptance a lie. What bounds it instead is the flow's own shutdown, which
// waits for the whole continuation tree before the pool is torn down.
//
// Scheduling is caller-runs: onto the pool when there is room, otherwise on this
// goroutine. That is both the backpressure — a split fanning out 50,000 elements
// ends up pricing them itself once the pool is saturated, so it can never
// outrun the flow — and the reason the fan-out cannot deadlock. Parking on the
// pool instead would let a tail that resumes again (a split feeding an
// aggregate, the canonical pairing) wait on a queue only it could drain.
func (bf *boundFlow) resume(_ context.Context, msg *types.Message, tail core.MessageProcessor) error {
	ctx := bf.runCtx()

	// Counted before the work starts and released only when the tail has run. A
	// tail that resumes again registers its own work before this one is released,
	// so the count cannot reach zero while the tree is still growing.
	bf.inflight.Add(1)
	run := func() {
		defer bf.inflight.Done()
		bf.run(ctx, msg, tail)
	}
	if !bf.pool.TrySubmit(run) {
		run()
	}
	return nil
}

// runCtx returns the flow's own context, carrying the runtime services a block
// reaches for, with cancellation stripped. See resume for why the work outlives
// the message that scheduled it.
func (bf *boundFlow) runCtx() context.Context {
	if bf.ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(bf.ctx)
}

// run puts one message through proc and publishes its outcome. proc is the whole
// root flow for a message off the source, or the tail of it for a message a
// block resumed — and that is the only difference between the two. A resumed
// tail is an invocation in its own right: it publishes its own started and
// terminal events, keyed on its own EventID, and the flow's error chain recovers
// it the same way. That is what gives a split per-element error isolation, since
// one element failing is one failed invocation among many.
//
// All events key on the inbound EventID (stable for the message's life, so it
// equals out.EventID) to keep correlation consistent for request/response
// sources.
//
// The terminal event carries how long the message took, measured here rather than
// left to a subscriber to derive by pairing: the clock covers the error path too,
// because a message the error path recovered still cost what the error path spent.
func (bf *boundFlow) run(ctx context.Context, msg *types.Message, proc core.MessageProcessor) {
	traceID := bf.traceID(msg)
	bf.publish(flowOutcome{kind: types.FlowEventStarted, eventID: msg.EventID, traceID: traceID})
	started := time.Now()

	out, err := proc.Process(ctx, msg)
	if err != nil && bf.errorPath != nil {
		// Recovery: expose the failure as vars.error and run the error path. On
		// success its output replaces the result; if it also fails, that error
		// stands and the flow is reported failed.
		//
		// Error handling is optional: a flow with no error path (errorPath nil)
		// behaves like an empty error handler — the error propagates and the flow
		// is reported failed, the default below.
		engine.SetErrorVariable(msg, bf.name, err)
		if recovered, altErr := bf.errorPath.Process(ctx, msg); altErr != nil {
			err = fmt.Errorf("error path: %w", altErr)
		} else {
			out, err = recovered, nil
		}
	}
	elapsed := time.Since(started)

	outcome := flowOutcome{eventID: msg.EventID, traceID: traceID, elapsed: elapsed}
	switch {
	case err != nil:
		// A recovered failure is not reported here: the error path replaced the
		// error with a result above, so the flow completes. Only an error nothing
		// handled reaches this branch, which is what makes the failed event mean
		// "unhandled".
		slog.Error("flow processing failed", "flow", bf.name, "event_id", msg.EventID, "error", err)
		outcome.kind, outcome.err, outcome.result = types.FlowEventFailed, err, msg
	case out == nil:
		outcome.kind, outcome.result = types.FlowEventDropped, msg
	default:
		outcome.kind, outcome.result = types.FlowEventCompleted, out
	}
	bf.publish(outcome)
}

// traceID names the end-to-end invocation this message belongs to.
//
// It is minted here, at the one point every message passes through — off a
// source, from `octo invoke`, or resumed as the tail of a flow a block took over
// — so no source has to remember to do it and none of them can disagree about
// how. A message that already carries an id keeps it: the id is set by whoever
// saw the work first, which may be a source that wanted its own record to join
// the trace, or another process entirely, since the id travels as an ordinary
// message variable.
//
// When tracing is off nothing is minted, but an id already on the message is
// still reported: a runtime with tracing off can still be a hop in a trace
// something upstream started.
func (bf *boundFlow) traceID(msg *types.Message) string {
	if !core.Tracer().Enabled() {
		return msg.TraceID()
	}
	id, err := msg.EnsureTraceID()
	if err != nil {
		// Losing the id costs the trace its grouping, not the message its run.
		slog.Warn("could not generate a trace id",
			"flow", bf.name, "event_id", msg.EventID, "error", err)
		return ""
	}
	return id
}

// flowOutcome is everything publish needs to describe one event. It is a struct
// rather than a parameter list because the list had reached six, half of them
// optional and three of them adjacent strings — the shape where an argument ends
// up in the wrong slot and nothing complains.
type flowOutcome struct {
	kind    types.FlowEventKind
	eventID string
	traceID string
	elapsed time.Duration
	err     error
	result  *types.Message
}

// publish emits a flow event if a bus is configured. result is the message to
// attach to the event (nil for the started event), and elapsed is zero for it.
func (bf *boundFlow) publish(outcome flowOutcome) {
	if bf.bus == nil {
		return
	}
	bf.bus.Publish(types.FlowEvent{
		Kind:       outcome.kind,
		Flow:       bf.name,
		EventID:    outcome.eventID,
		TraceID:    outcome.traceID,
		OccurredAt: time.Now(),
		Duration:   outcome.elapsed,
		Err:        outcome.err,
		// Naming the block a failure came from is what turns "this flow is erroring"
		// into "this block is erroring". It is empty for anything that did not
		// originate in a block, and for every non-failed event.
		Block:  engine.FailingBlock(outcome.err),
		Result: outcome.result,
	})
}
