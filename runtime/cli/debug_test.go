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

// printed runs printDebugOutcome and returns the raw line it printed.
func printed(t *testing.T, req invokeRequest, result *types.Message, runErr error) string {
	t.Helper()
	var printErr error
	out := captureStdout(t, func() { printErr = printDebugOutcome(req, result, runErr) })
	if printErr != nil {
		t.Fatalf("printDebugOutcome: %v", printErr)
	}
	return out
}

// envelope runs printDebugOutcome and decodes the envelope it printed.
func envelope(t *testing.T, req invokeRequest, result *types.Message, runErr error) debugOutcome {
	t.Helper()
	out := printed(t, req, result, runErr)

	var outcome debugOutcome
	if err := json.Unmarshal([]byte(out), &outcome); err != nil {
		t.Fatalf("decode envelope %q: %v", out, err)
	}
	return outcome
}

// breakReq is an invoke that was asked to break at a block.
func breakReq(bp *core.Breakpoint) invokeRequest {
	return invokeRequest{breakpoint: bp}
}

// recordedAddress is the block the tests below break at.
const recordedAddress = "orders.charge"

// recorded returns a breakpoint that has captured a message, as the engine's
// breakpoint block leaves it.
func recorded(t *testing.T) *core.Breakpoint {
	t.Helper()
	bp := core.NewBreakpoint(recordedAddress)
	bp.Record(stageMessage(t, "target"))
	return bp
}

// stageMessage is a message carrying a body and a stage variable, so a test can tell
// one point of a flow from another.
func stageMessage(t *testing.T, stage string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"amount": 250.0}
	msg.Variables.Set("stage", stage)
	return msg
}

// mustSpies returns a collector watching the given addresses.
func mustSpies(t *testing.T, addresses ...string) *core.Spies {
	t.Helper()
	spies, err := core.NewSpies(addresses)
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}
	return spies
}

func TestBreakOutcomeReached(t *testing.T) {
	outcome := envelope(t, breakReq(recorded(t)), nil, nil)

	if outcome.Reached == nil || !*outcome.Reached {
		t.Error("reached = false, want true")
	}
	if outcome.Block != recordedAddress {
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
	outcome := envelope(t, breakReq(core.NewBreakpoint("orders.checkHeader[else].charge")), nil, nil)

	if outcome.Reached == nil || *outcome.Reached {
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
	outcome := envelope(t, breakReq(core.NewBreakpoint("orders.after-charge")), nil, runErr)

	if outcome.Reached == nil || *outcome.Reached {
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
		printErr = printDebugOutcome(breakReq(core.NewBreakpoint("orders.nope")), nil, startupErr)
	})

	if printErr == nil {
		t.Fatal("printDebugOutcome returned nil for a startup failure, want the error (non-zero exit)")
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
	out := printed(t, breakReq(recorded(t)), nil, nil)

	if strings.Contains(out, "octoStop") {
		t.Errorf("envelope leaks the internal stop flag: %s", out)
	}
}

// TestBreakEnvelopeStaysCompatible is a compatibility pin, not a behavior test.
//
// run-host decides whether stdout holds a break envelope by sniffing for a *boolean*
// `reached` key (packages/run-host/src/session.ts, parseBreakOutcome), then reads
// block/message/error off it. Growing this envelope for spies must not disturb any of
// that, so this decodes a --break-at envelope the way run-host does and asserts the
// keys it relies on still land.
func TestBreakEnvelopeStaysCompatible(t *testing.T) {
	out := printed(t, breakReq(recorded(t)), nil, nil)

	var probe struct {
		Reached any            `json:"reached"`
		Block   string         `json:"block"`
		Message *types.Message `json:"message"`
		Error   string         `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &probe); err != nil {
		t.Fatalf("decode envelope %q: %v", out, err)
	}
	if _, ok := probe.Reached.(bool); !ok {
		t.Fatalf("reached = %#v (%T), want a bool: run-host identifies the envelope by it",
			probe.Reached, probe.Reached)
	}
	if probe.Block != recordedAddress || probe.Message == nil {
		t.Errorf("the envelope lost a field run-host reads: %s", out)
	}
}

// TestSpiesOnlyEnvelopeOmitsReached is the other half of that contract. A spies-only
// run prints an envelope too, and it must NOT look like a break outcome — otherwise
// run-host would report a breakpoint that was never asked for and never hit.
func TestSpiesOnlyEnvelopeOmitsReached(t *testing.T) {
	spies := mustSpies(t, "orders.charge")
	spies.Record("orders.charge", core.SpyRecord{Input: stageMessage(t, "in"), Output: stageMessage(t, "out")})
	req := invokeRequest{spies: spies}

	out := printed(t, req, stageMessage(t, "done"), nil)

	var raw map[string]any
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("decode envelope %q: %v", out, err)
	}
	if _, present := raw["reached"]; present {
		t.Errorf("a spies-only envelope carries `reached`: run-host would read it as a break outcome: %s", out)
	}

	outcome := envelope(t, req, stageMessage(t, "done"), nil)
	if len(outcome.Spies) != 1 {
		t.Fatalf("got %d spy traces, want 1", len(outcome.Spies))
	}
	if outcome.Spies[0].Address != "orders.charge" || len(outcome.Spies[0].Records) != 1 {
		t.Errorf("spy trace = %+v, want one record for orders.charge", outcome.Spies[0])
	}
	// A spied run is never stopped, so the flow's own result is reported too.
	if outcome.Result == nil {
		t.Error("a spies-only envelope must carry the flow's result")
	}
}

// TestSpyTracesKeepTheOrderAsked: the traces come back in the order the addresses
// were given, and each block's records in the order they happened — so a trace reads
// top to bottom.
func TestSpyTracesKeepTheOrderAsked(t *testing.T) {
	want := []string{"orders.second", "orders.first"} // deliberately not sorted
	spies := mustSpies(t, want...)
	spies.Record("orders.first", core.SpyRecord{Input: stageMessage(t, "a")})
	spies.Record("orders.first", core.SpyRecord{Input: stageMessage(t, "b")})

	outcome := envelope(t, invokeRequest{spies: spies}, stageMessage(t, "done"), nil)

	for i, trace := range outcome.Spies {
		if trace.Address != want[i] {
			t.Errorf("trace %d = %q, want %q", i, trace.Address, want[i])
		}
	}
	// A block the flow never reached reports an empty list, not null: "nothing crossed
	// it" is a result, and null would read like the spy was never installed.
	if outcome.Spies[0].Records == nil {
		t.Error("an unreached spy reports null records, want an empty list")
	}
	first := outcome.Spies[1].Records
	if len(first) != 2 || first[0].Seq >= first[1].Seq {
		t.Errorf("records are not in the order they happened: %+v", first)
	}
}

// TestSpiesAndBreakpointTogether: both were asked for, so the envelope carries both —
// the snapshot at the breakpoint, and everything the spies saw on the way to it.
func TestSpiesAndBreakpointTogether(t *testing.T) {
	spies := mustSpies(t, "orders.validate")
	spies.Record("orders.validate", core.SpyRecord{Input: stageMessage(t, "in")})

	req := invokeRequest{breakpoint: recorded(t), spies: spies}
	outcome := envelope(t, req, nil, nil)

	if outcome.Reached == nil || !*outcome.Reached {
		t.Error("the breakpoint was recorded but the envelope says not reached")
	}
	if len(outcome.Spies) != 1 || len(outcome.Spies[0].Records) != 1 {
		t.Errorf("the spies are missing from the envelope: %+v", outcome.Spies)
	}
	// The flow's own message is not reported under --break-at: it carries the stop
	// flag, so the snapshot is what the user is shown.
	if outcome.Result != nil {
		t.Error("a --break-at envelope must not carry the flow's stopped message")
	}
}

// TestDroppedFlowIsReported: a flow that filtered the message out returns nothing.
// Under a spied run that has to be said out loud, or an envelope with no result would
// read like the flow produced an empty body.
func TestDroppedFlowIsReported(t *testing.T) {
	outcome := envelope(t, invokeRequest{spies: mustSpies(t, "orders.filter")}, nil, nil)

	if !outcome.Dropped {
		t.Error("dropped = false, want true: the flow returned no message")
	}
	if outcome.Result != nil {
		t.Errorf("a dropped flow carries a result: %+v", outcome.Result)
	}
}

func TestParseSpies(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
		err  string
	}{
		{name: "empty means no spies", raw: "", want: nil},
		{name: "one address", raw: "orders.charge", want: []string{"orders.charge"}},
		{
			name: "several, with spaces and brackets",
			raw:  "orders.first, orders.fanout[audit].log-it ",
			want: []string{"orders.first", "orders.fanout[audit].log-it"},
		},
		{name: "duplicate", raw: "orders.a,orders.a", err: "twice"},
		{name: "empty address", raw: "orders.a,,orders.b", err: "empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spies, err := parseSpies(tc.raw)

			if tc.err != "" {
				if err == nil {
					t.Fatalf("parseSpies(%q) succeeded, want an error", tc.raw)
				}
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("error = %q, want it to mention %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSpies(%q): %v", tc.raw, err)
			}
			if tc.want == nil {
				if spies != nil {
					t.Errorf("parseSpies(%q) built a collector, want none", tc.raw)
				}
				return
			}

			got := spies.Addresses()
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("address %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseMocks(t *testing.T) {
	t.Run("a spec with cases and a default", func(t *testing.T) {
		mocks, err := parseMocks(`{"orders.charge":{
			"cases":[{"when":"body.amount > 100","error":"declined"},
			         {"when":"true","body":{"ok":true},"vars":{"status":200}}],
			"default":{"drop":true}}}`)
		if err != nil {
			t.Fatalf("parseMocks: %v", err)
		}

		spec, ok := mocks.For("orders.charge")
		if !ok {
			t.Fatal("no spec for orders.charge")
		}
		if len(spec.Cases) != 2 || spec.Default == nil || !spec.Default.Drop {
			t.Errorf("spec = %+v, want two cases and a dropping default", spec)
		}
		if spec.Cases[0].Error != "declined" || spec.Cases[1].Vars["status"] != 200.0 {
			t.Errorf("cases decoded wrong: %+v", spec.Cases)
		}
	})

	t.Run("empty means no mocks", func(t *testing.T) {
		mocks, err := parseMocks("")
		if err != nil {
			t.Fatalf("parseMocks: %v", err)
		}
		if mocks != nil {
			t.Error("parseMocks(\"\") built a mock set, want none")
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		if _, err := parseMocks(`{"orders.charge":`); err == nil {
			t.Fatal("malformed JSON must be rejected")
		}
	})

	// An invalid spec is caught at the flag, not deep in the flow builder, so the
	// message names the flag the user typed.
	t.Run("an invalid spec is rejected at the flag", func(t *testing.T) {
		_, err := parseMocks(`{"orders.charge":{"cases":[{"when":"true"}]}}`)
		if err == nil {
			t.Fatal("a case with no outcome must be rejected")
		}
		if !strings.Contains(err.Error(), "--mocks") || !strings.Contains(err.Error(), "needs an outcome") {
			t.Errorf("error = %q, want it to blame --mocks and say what is wrong", err)
		}
	})
}
