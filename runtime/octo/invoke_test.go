package main

import (
	"encoding/json"
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
