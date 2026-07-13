package main

// This file holds the CLI's debug surface: the --break-at / --spies / --mocks flags
// are parsed into the collectors the runtime takes, and what they observed is printed
// as one JSON envelope.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// debugOutcome is the JSON envelope `octo invoke` prints on stdout when it was asked
// to observe the run — with --break-at, with --spies, or both.
//
// Error carries a flow failure, which is a debugging result rather than a CLI error,
// so it exits 0 and a consumer can parse the envelope instead of inspecting exit
// codes. An unresolvable address is not reported here at all; it is a bad request and
// exits non-zero.
//
// Reached is a *bool, and that is load-bearing. It is what identifies a break outcome
// to a consumer — run-host sniffs for a boolean `reached` to tell the envelope from a
// plain result body — so it must marshal as `false` on a breakpoint that was never
// hit (a normal result: the message may simply have taken a branch the block is not
// on), and be *absent* when there was no breakpoint at all, so a spies-only envelope
// is not mistaken for one. A plain bool could not say both.
type debugOutcome struct {
	Reached *bool          `json:"reached,omitempty"`
	Block   string         `json:"block,omitempty"`
	Message *types.Message `json:"message,omitempty"`
	// Result is the flow's own result, for a run that was not stopped at a
	// breakpoint. It is deliberately absent under --break-at: the message the flow
	// returns there carries the internal stop flag, which is bookkeeping the user
	// should not see, so the snapshot in Message is the message to report.
	Result  *types.Message `json:"result,omitempty"`
	Dropped bool           `json:"dropped,omitempty"`
	Spies   []spyTrace     `json:"spies,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// spyTrace is what one spied block saw, in the order it saw it.
type spyTrace struct {
	Address string           `json:"address"`
	Records []core.SpyRecord `json:"records"`
}

// parseSpies builds the spy collector from the comma-separated --spies flag. An
// address cannot contain a comma under the address grammar, so splitting on one is
// unambiguous.
func parseSpies(raw string) (*core.Spies, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil //nolint:nilnil // no --spies is not an error: there is simply no collector
	}

	fields := strings.Split(raw, ",")
	addresses := make([]string, 0, len(fields))
	for _, field := range fields {
		addresses = append(addresses, strings.TrimSpace(field))
	}

	spies, err := core.NewSpies(addresses)
	if err != nil {
		return nil, fmt.Errorf("parse --spies: %w", err)
	}
	return spies, nil
}

// parseMocks builds the mock set from the --mocks flag, one JSON object keyed by
// block address. It is a single JSON blob rather than a repeated flag because a mock
// spec is a nested thing — a list of cases, each with an expression and an outcome —
// and there is no honest way to flatten that onto argv.
func parseMocks(raw string) (*core.Mocks, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil //nolint:nilnil // no --mocks is not an error: there is simply nothing to mock
	}

	var specs map[string]core.MockSpec
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		return nil, fmt.Errorf("parse --mocks: %w", err)
	}
	mocks, err := core.NewMocks(specs)
	if err != nil {
		return nil, fmt.Errorf("parse --mocks: %w", err)
	}
	return mocks, nil
}

// printDebugOutcome prints the envelope for a run that was asked to observe itself.
// runErr is whatever invokeFlow returned: a flow failure becomes part of the envelope
// (the flow may have failed before ever reaching the block, which is worth seeing,
// and a spy will have recorded what it was carrying when it did), while a service
// that would not start — an unresolvable address, a bad mock spec, a bad config — is
// a bad request and is returned so the CLI exits non-zero.
func printDebugOutcome(req invokeRequest, result *types.Message, runErr error) error {
	var callErr *flowCallError
	if runErr != nil && !errors.As(runErr, &callErr) {
		return runErr
	}

	var outcome debugOutcome
	if callErr != nil {
		outcome.Error = callErr.Error()
	}

	if req.breakpoint != nil {
		reached := false
		outcome.Block = req.breakpoint.Address()
		if snapshot, ok := req.breakpoint.Snapshot(); ok {
			reached = true
			outcome.Message = snapshot
		}
		outcome.Reached = &reached
	} else if callErr == nil {
		// No breakpoint, so the flow ran to its own end: report what it produced, the
		// same message a plain invoke prints. (Under --break-at the flow's message
		// carries the internal stop flag the breakpoint itself set, which is why the
		// snapshot is reported there instead — see debugOutcome.)
		if result != nil {
			outcome.Result = result.Reported()
		}
		outcome.Dropped = result == nil
	}

	if req.spies != nil {
		outcome.Spies = collectSpyTraces(req.spies)
	}

	encoded, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("encode debug outcome: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

// collectSpyTraces reads what each spy saw, in the order the addresses were given —
// a trace the user can read top to bottom in the order they asked for it, with each
// block's records in the order they happened.
func collectSpyTraces(spies *core.Spies) []spyTrace {
	addresses := spies.Addresses()
	traces := make([]spyTrace, 0, len(addresses))
	for _, address := range addresses {
		records := spies.Records(address)
		if records == nil {
			// An empty trace is a result, not a gap: the flow may simply have taken a
			// branch the block is not on. Report it as an empty list rather than null.
			records = []core.SpyRecord{}
		}
		traces = append(traces, spyTrace{Address: address, Records: records})
	}
	return traces
}
