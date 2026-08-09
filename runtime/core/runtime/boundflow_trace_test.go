package runtime

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// enabledTracer is enabled and keeps nothing: these tests are about what the
// flow boundary does when tracing is on, not about what a sink does with it.
type enabledTracer struct{}

func (enabledTracer) Enabled() bool              { return true }
func (enabledTracer) Publish(_ types.TraceEvent) {}

// traceEnabled turns tracing on for one test and restores the process-wide
// publisher afterwards.
func traceEnabled(t *testing.T) {
	t.Helper()
	previous := core.Tracer()
	core.SetTracer(enabledTracer{})
	t.Cleanup(func() { core.SetTracer(previous) })
}

// runMessage puts one specific message through the flow, where runOne builds its
// own — a test that pre-sets a trace id needs to choose the message.
func runMessage(t *testing.T, bf *boundFlow, msg *types.Message) {
	t.Helper()
	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bf.in <- msg
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// With tracing off the flow boundary mints nothing: a runtime nobody asked to
// trace must not start writing ids onto messages.
func TestFlowMintsNoTraceIDWhenTracingIsOff(t *testing.T) {
	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "pass"}}, nil, rec)

	msg := mustMessage(t)
	runMessage(t, bf, msg)

	if got := msg.TraceID(); got != "" {
		t.Fatalf("message carries a trace id with tracing off: %q", got)
	}
	if got := rec.started(t).TraceID; got != "" {
		t.Fatalf("started event TraceID = %q, want empty", got)
	}
	if got := rec.terminal(t).TraceID; got != "" {
		t.Fatalf("terminal event TraceID = %q, want empty", got)
	}
}

// With tracing on, both events of one invocation report the same id — that
// pairing is what lets a reader bracket the invocation at all.
func TestFlowMintsOneTraceIDPerInvocation(t *testing.T) {
	traceEnabled(t)

	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "pass"}}, nil, rec)

	msg := mustMessage(t)
	runMessage(t, bf, msg)

	id := msg.TraceID()
	if id == "" {
		t.Fatal("the flow boundary minted no trace id")
	}
	if got := rec.started(t).TraceID; got != id {
		t.Fatalf("started event TraceID = %q, want %q", got, id)
	}
	if got := rec.terminal(t).TraceID; got != id {
		t.Fatalf("terminal event TraceID = %q, want %q", got, id)
	}
}

// An id already on the message wins. This is what makes a source able to name
// the trace before the flow sees the message, and what makes a message arriving
// from another process stay in the trace it was already part of.
func TestFlowKeepsAnExistingTraceID(t *testing.T) {
	traceEnabled(t)

	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "pass"}}, nil, rec)

	msg := mustMessage(t)
	msg.SetTraceID("from-upstream")
	runMessage(t, bf, msg)

	if got := msg.TraceID(); got != "from-upstream" {
		t.Fatalf("the flow re-minted the trace id: %q", got)
	}
	if got := rec.started(t).TraceID; got != "from-upstream" {
		t.Fatalf("started event TraceID = %q, want the upstream id", got)
	}
}

// A failed invocation still reports its trace: the runs worth reading are
// usually the ones that went wrong.
func TestFlowReportsTraceIDOnFailure(t *testing.T) {
	traceEnabled(t)

	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "fail"}}, nil, rec)
	runOne(t, bf)

	ev := rec.terminal(t)
	if ev.Kind != types.FlowEventFailed {
		t.Fatalf("terminal kind = %s, want %s", ev.Kind, types.FlowEventFailed)
	}
	if ev.TraceID == "" {
		t.Fatal("a failed flow reported no trace id")
	}
	if got := rec.started(t).TraceID; got != ev.TraceID {
		t.Fatalf("started/terminal disagree on the trace: %q vs %q", got, ev.TraceID)
	}
}

// A resumed tail — the continuation a block that took over the flow hands back —
// is its own invocation with its own EventID, and must report the trace of the
// message it came from. Without this a split's elements would each look like the
// beginning of something unrelated.
func TestResumedTailKeepsTheParentTrace(t *testing.T) {
	traceEnabled(t)

	rec := &recorder{}
	bf := newEventFlow(t, []types.BlockConfig{{Type: "pass"}}, nil, rec)
	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	parent := mustMessage(t)
	if _, err := parent.EnsureTraceID(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	tail := parent.Clone()
	if _, err := tail.Rekey(); err != nil {
		t.Fatalf("rekey: %v", err)
	}
	if err := bf.resume(context.Background(), tail, bf.root); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	ev := rec.started(t)
	if ev.EventID == parent.EventID {
		t.Fatal("the resumed tail reused the parent's EventID")
	}
	if ev.TraceID != parent.TraceID() {
		t.Fatalf("resumed tail TraceID = %q, want the parent's %q", ev.TraceID, parent.TraceID())
	}
}
