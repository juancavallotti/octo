package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// captureStdout runs fn with stdout redirected, returning what it printed.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = write

	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(read)
		done <- string(out)
	}()

	fn()

	_ = write.Close()
	os.Stdout = orig
	return <-done
}

// breakEnvelope runs printBreakOutcome and decodes the envelope it printed.
func breakEnvelope(t *testing.T, bp *core.Breakpoint, runErr error) breakOutcome {
	t.Helper()
	var printErr error
	out := captureStdout(t, func() { printErr = printBreakOutcome(bp, runErr) })
	if printErr != nil {
		t.Fatalf("printBreakOutcome: %v", printErr)
	}

	var outcome breakOutcome
	if err := json.Unmarshal([]byte(out), &outcome); err != nil {
		t.Fatalf("decode envelope %q: %v", out, err)
	}
	return outcome
}

// recorded returns a breakpoint that has captured a message, as the engine's
// breakpoint block leaves it.
func recorded(t *testing.T, address string) *core.Breakpoint {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"amount": 250.0}
	msg.Variables.Set("stage", "target")

	bp := core.NewBreakpoint(address)
	bp.Record(msg)
	return bp
}

func TestBreakOutcomeReached(t *testing.T) {
	outcome := breakEnvelope(t, recorded(t, "orders.charge"), nil)

	if !outcome.Reached {
		t.Error("reached = false, want true")
	}
	if outcome.Block != "orders.charge" {
		t.Errorf("block = %q, want %q", outcome.Block, "orders.charge")
	}
	if outcome.Message == nil {
		t.Fatal("envelope carries no message")
	}
	if stage, _ := outcome.Message.Variables.String("stage"); stage != "target" {
		t.Errorf("message stage = %q, want %q", stage, "target")
	}
	if outcome.Error != "" {
		t.Errorf("error = %q, want empty", outcome.Error)
	}
}

// TestBreakOutcomeNotReached: the block was never executed — a normal result, not a
// failure, so the envelope says so and the command exits 0.
func TestBreakOutcomeNotReached(t *testing.T) {
	outcome := breakEnvelope(t, core.NewBreakpoint("orders.checkHeader[else].charge"), nil)

	if outcome.Reached {
		t.Error("reached = true, want false")
	}
	if outcome.Block != "orders.checkHeader[else].charge" {
		t.Errorf("block = %q, want the address", outcome.Block)
	}
	if outcome.Message != nil {
		t.Errorf("envelope carries a message for an unreached block: %+v", outcome.Message)
	}
}

// TestBreakOutcomeFlowFailure: a flow that fails is a debugging result — it is
// reported in the envelope (exit 0) so the caller can see the flow never got to the
// block, and why.
func TestBreakOutcomeFlowFailure(t *testing.T) {
	runErr := &flowCallError{err: errors.New(`block "charge": upstream refused`)}
	outcome := breakEnvelope(t, core.NewBreakpoint("orders.after-charge"), runErr)

	if outcome.Reached {
		t.Error("reached = true, want false: the flow failed before the block")
	}
	if !strings.Contains(outcome.Error, "upstream refused") {
		t.Errorf("error = %q, want the flow's failure", outcome.Error)
	}
}

// TestBreakOutcomeStartupFailureExitsNonZero: an unresolvable address (or any error
// that stopped the service from starting) is a bad request, not a "not reached"
// result. It must surface as a CLI error rather than a clean envelope, so a typo can
// never be mistaken for "the block was never reached".
func TestBreakOutcomeStartupFailureExitsNonZero(t *testing.T) {
	startupErr := errors.New(`breakpoint "orders.nope": no block "nope" in that chain`)

	var printErr error
	out := captureStdout(t, func() {
		printErr = printBreakOutcome(core.NewBreakpoint("orders.nope"), startupErr)
	})

	if printErr == nil {
		t.Fatal("printBreakOutcome returned nil for a startup failure, want the error (non-zero exit)")
	}
	if !errors.Is(printErr, startupErr) {
		t.Errorf("returned %v, want the startup error", printErr)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("printed an envelope for a startup failure: %q", out)
	}
}

// TestBreakOutcomeOmitsInternalStopFlag: the flow engine marks the message to halt
// it, and that bookkeeping must not show up in what the user is shown.
func TestBreakOutcomeOmitsInternalStopFlag(t *testing.T) {
	bp := recorded(t, "orders.charge")
	out := captureStdout(t, func() {
		if err := printBreakOutcome(bp, nil); err != nil {
			t.Errorf("printBreakOutcome: %v", err)
		}
	})

	if strings.Contains(out, "octoStop") {
		t.Errorf("envelope leaks the internal stop flag: %s", out)
	}
}
