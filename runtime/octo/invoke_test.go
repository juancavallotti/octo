package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// resultEnvelope runs printFlowResult and decodes the message it printed.
func resultEnvelope(t *testing.T, result *types.Message) map[string]any {
	t.Helper()
	var printErr error
	out := captureStdout(t, func() { printErr = printFlowResult("demo", result) })
	if printErr != nil {
		t.Fatalf("printFlowResult: %v", printErr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode result %q: %v", out, err)
	}
	return envelope
}

// TestPlainInvokePrintsTheMessage: a plain invoke reports the whole result message —
// its variables as well as its body. A flow builds variables up as deliberately as it
// builds its body, and a run that finished must not report less than the same run
// stopped early at a breakpoint.
func TestPlainInvokePrintsTheMessage(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.Variables.Set("tier", "gold")
	if err := msg.SetBodyJSON([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("set body: %v", err)
	}

	envelope := resultEnvelope(t, msg)

	if envelope["event_id"] != msg.EventID {
		t.Errorf("event_id = %v, want %q", envelope["event_id"], msg.EventID)
	}
	vars, ok := envelope["variables"].(map[string]any)
	if !ok || vars["tier"] != "gold" {
		t.Errorf("variables = %v, want tier=gold", envelope["variables"])
	}
	body, ok := envelope["body"].(map[string]any)
	if !ok || body["ok"] != true {
		t.Errorf("body = %v, want ok=true", envelope["body"])
	}
}

// TestPlainInvokeHidesTheStopFlag: a flow a filter block terminated returns a message
// carrying the engine's internal stop flag. It is bookkeeping, not something the flow
// set, so it must not surface among the variables the user is shown.
func TestPlainInvokeHidesTheStopFlag(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.Variables.Set("tier", "gold")
	msg.RequestStop()

	vars, ok := resultEnvelope(t, msg)["variables"].(map[string]any)
	if !ok {
		t.Fatalf("no variables reported")
	}
	if _, leaked := vars["__octoStop"]; leaked {
		t.Errorf("variables leaked the internal stop flag: %v", vars)
	}
	if vars["tier"] != "gold" {
		t.Errorf("variables = %v, want the flow's own tier=gold kept", vars)
	}
	if !msg.StopRequested() {
		t.Error("printing the result cleared the stop flag on the message itself")
	}
}

// TestDroppedFlowPrintsNothing: a filtered message leaves no result, and a plain
// invoke says so on stderr rather than printing an empty envelope on stdout — the
// run-host tells a drop from a result by exactly this.
func TestDroppedFlowPrintsNothing(t *testing.T) {
	var err error
	out := captureStdout(t, func() { err = printFlowResult("demo", nil) })
	if err != nil {
		t.Fatalf("printFlowResult: %v", err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want nothing printed for a dropped message", out)
	}
}

// envelopeReq is an invoke that asked for the envelope and nothing else — no
// breakpoint, no spies. It is what a program driving invoke sends.
func envelopeReq() invokeRequest { return invokeRequest{envelope: true} }

// TestEnvelopeReportsEveryOutcomeInOneShape is the reason --envelope exists.
//
// Without it, a plain invoke says what happened three different ways: a completed flow
// prints its message on stdout, a dropped one prints NOTHING on stdout (see
// TestDroppedFlowPrintsNothing), and a failed one prints nothing and exits non-zero
// with the error in a log line. A caller reading stdout cannot tell a drop from a
// result, and would have to scrape failures out of logs. The envelope already models
// all three; --envelope is what asks for it when the run observed nothing.
func TestEnvelopeReportsEveryOutcomeInOneShape(t *testing.T) {
	t.Run("a completed flow reports its result", func(t *testing.T) {
		outcome := envelope(t, envelopeReq(), stageMessage(t, "done"), nil)

		if outcome.Result == nil {
			t.Fatal("no result reported for a flow that completed")
		}
		if outcome.Dropped {
			t.Error("dropped = true, want false: the flow produced a message")
		}
		if outcome.Error != "" {
			t.Errorf("error = %q, want empty", outcome.Error)
		}
	})

	t.Run("a dropped flow says so, rather than printing nothing", func(t *testing.T) {
		outcome := envelope(t, envelopeReq(), nil, nil)

		if !outcome.Dropped {
			t.Error("dropped = false, want true: a filtered message is not a silent success")
		}
		if outcome.Result != nil {
			t.Errorf("result = %v, want none: the flow produced no message", outcome.Result)
		}
	})

	t.Run("a failed flow reports the failure and exits 0", func(t *testing.T) {
		runErr := &flowCallError{err: errors.New("card declined")}

		// printed() fails the test if printDebugOutcome returned an error, which is what
		// the CLI exits non-zero on — so reaching the assertions is itself the exit-0 check.
		outcome := envelope(t, envelopeReq(), nil, runErr)

		if !strings.Contains(outcome.Error, "card declined") {
			t.Errorf("error = %q, want it to carry the flow's failure", outcome.Error)
		}
		// A failure is not a drop. Reporting one as the other would tell a test runner the
		// flow filtered the message out on purpose.
		if outcome.Dropped {
			t.Error("dropped = true, want false: the flow failed, it did not filter")
		}
	})
}

// TestEnvelopeStillExitsNonZeroOnABadRequest: the exit-0 rule above is for a flow that
// FAILED, not for a run that could not start. An unresolvable block address or a broken
// config is a bad request — there is no test result to report — so it must still exit
// non-zero rather than being buried in an envelope a caller would read as "ran fine".
func TestEnvelopeStillExitsNonZeroOnABadRequest(t *testing.T) {
	startupErr := errors.New(`spy "orders.nope": no block "nope" in that chain`)

	var err error
	captureStdout(t, func() { err = printDebugOutcome(envelopeReq(), nil, startupErr) })

	if err == nil {
		t.Fatal("a service that would not start must be returned, so the CLI exits non-zero")
	}
	if !errors.Is(err, startupErr) {
		t.Errorf("err = %v, want the startup error itself", err)
	}
}

// TestEnvelopeOmitsReached pins the compatibility rule --envelope could most easily
// break. run-host identifies a break envelope by sniffing for a BOOLEAN `reached`
// (packages/run-host/src/session.ts). An --envelope run sets no breakpoint, so `reached`
// must be absent — marshalling it as `false` would make every enveloped run look like a
// breakpoint that never fired.
func TestEnvelopeOmitsReached(t *testing.T) {
	out := printed(t, envelopeReq(), stageMessage(t, "done"), nil)

	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("decode envelope %q: %v", out, err)
	}
	if _, present := raw["reached"]; present {
		t.Errorf("envelope carries `reached` with no breakpoint set: %s", out)
	}
}

// TestEnvelopeFlagParses: the flag is off by default, so nothing that drives invoke
// today changes shape.
func TestEnvelopeFlagParses(t *testing.T) {
	base := []string{"--config", "x.yaml", "--flow", "demo"}

	off, err := parseInvokeFlags(base)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if off.envelope {
		t.Error("envelope = true by default, want false: a plain invoke must keep printing its message")
	}

	on, err := parseInvokeFlags(append(base, "--envelope"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !on.envelope {
		t.Error("--envelope did not set the flag")
	}
}
