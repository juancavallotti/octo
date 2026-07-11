package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// This file holds the CLI's debug surface: the flags that observe a run are turned
// into the collectors the runtime takes, and what they observed is printed as one
// JSON envelope.

// breakOutcome is the JSON envelope `octo invoke --break-at` prints on stdout.
// Reached distinguishes "the flow ran through the block, here is the message it
// produced" from "the flow never got there" — the latter is a normal result, not a
// failure: the message may simply have taken a branch the block is not on. Error
// carries a flow failure, which is likewise a debugging result rather than a CLI
// error, so both cases exit 0 and a consumer can parse the envelope instead of
// inspecting exit codes. An unresolvable address is not reported here at all; it is
// a bad request and exits non-zero.
type breakOutcome struct {
	Reached bool           `json:"reached"`
	Block   string         `json:"block"`
	Message *types.Message `json:"message,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// printBreakOutcome prints the envelope for a --break-at run. runErr is whatever
// invokeFlow returned: a flow failure becomes part of the envelope (the flow may
// have failed before ever reaching the block, which is worth seeing), while a
// service that would not start — an unresolvable address, a bad config — is a bad
// request and is returned so the CLI exits non-zero.
func printBreakOutcome(bp *core.Breakpoint, runErr error) error {
	var callErr *flowCallError
	if runErr != nil && !errors.As(runErr, &callErr) {
		return runErr
	}

	outcome := breakOutcome{Block: bp.Address()}
	if callErr != nil {
		outcome.Error = callErr.Error()
	}
	if snapshot, ok := bp.Snapshot(); ok {
		outcome.Reached = true
		outcome.Message = snapshot
	}

	encoded, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("encode breakpoint outcome: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}
