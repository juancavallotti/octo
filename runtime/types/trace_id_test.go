package types

import "testing"

// The trace id has to survive every way a message is copied or re-keyed, because
// that is the whole reason it exists: EventID is deliberately re-minted whenever
// work crosses into a sub-invocation, so something else has to be the thread.

func TestEnsureTraceIDMintsOnceAndKeeps(t *testing.T) {
	msg, err := NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if got := msg.TraceID(); got != "" {
		t.Fatalf("a fresh message already has a trace id: %q", got)
	}

	first, err := msg.EnsureTraceID()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if first == "" {
		t.Fatal("EnsureTraceID returned an empty id")
	}

	// Idempotent: whoever saw the work first names the trace, and every later
	// caller joins it rather than starting its own.
	second, err := msg.EnsureTraceID()
	if err != nil {
		t.Fatalf("ensure again: %v", err)
	}
	if second != first {
		t.Fatalf("EnsureTraceID re-minted: %q then %q", first, second)
	}
	if got := msg.TraceID(); got != first {
		t.Fatalf("TraceID() = %q, want %q", got, first)
	}
}

func TestTraceIDSurvivesCopiesAndRekey(t *testing.T) {
	msg, err := NewMessage("corr-1")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.Body = map[string]any{"n": float64(1)}
	id, err := msg.EnsureTraceID()
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}

	t.Run("clone", func(t *testing.T) {
		if got := msg.Clone().TraceID(); got != id {
			t.Fatalf("clone trace id = %q, want %q", got, id)
		}
	})
	t.Run("scoped", func(t *testing.T) {
		if got := msg.Scoped().TraceID(); got != id {
			t.Fatalf("scoped trace id = %q, want %q", got, id)
		}
	})
	t.Run("rekey keeps the trace but changes the event", func(t *testing.T) {
		sub := msg.Clone()
		before := sub.EventID
		if _, err := sub.Rekey(); err != nil {
			t.Fatalf("rekey: %v", err)
		}
		if sub.EventID == before {
			t.Fatal("Rekey did not change the EventID")
		}
		if got := sub.TraceID(); got != id {
			t.Fatalf("trace id after Rekey = %q, want %q", got, id)
		}
	})
}

// SetTraceID is for a caller joining work to a trace that started elsewhere; an
// empty id is a no-op rather than a way to erase one.
func TestSetTraceIDIgnoresEmpty(t *testing.T) {
	msg, err := NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.SetTraceID("from-upstream")
	msg.SetTraceID("")
	if got := msg.TraceID(); got != "from-upstream" {
		t.Fatalf("trace id = %q, want it untouched by the empty set", got)
	}
}

// Reported is the shape shown to a user, and no internal variable belongs in it.
// It strips by prefix so a variable added later is covered the day it is named,
// rather than leaking until someone remembers this function exists.
func TestReportedStripsEveryInternalVariable(t *testing.T) {
	msg, err := NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if _, err := msg.EnsureTraceID(); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	msg.RequestStop()
	msg.Variables.Set("__somethingAddedLater", 1)
	msg.Variables.Set("httpStatus", 201)

	reported := msg.Reported()
	for name := range reported.Variables {
		if IsInternalVar(name) {
			t.Fatalf("Reported kept the internal variable %q", name)
		}
	}
	if got, ok := reported.Variables.Int("httpStatus"); !ok || got != 201 {
		t.Fatalf("Reported dropped a flow's own variable: httpStatus = (%d, %v)", got, ok)
	}

	// Reported scopes rather than clones, but it must not edit the original's
	// variables on the way: the flow is still using them.
	if msg.TraceID() == "" || !msg.StopRequested() {
		t.Fatal("Reported stripped the internal variables from the original message")
	}
}
