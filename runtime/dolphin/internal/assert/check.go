package assert

// This file holds the whole judgement: what the flow DID, what it returned, and what
// each watched block saw.

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// Check judges an outcome against a case, and returns every expectation that did not
// hold — not the first one.
//
// Reporting them all is the point. A case that got the wrong body has probably also got
// the wrong variables, and telling the user about one, then about the next after they
// fix it, then about the third, turns one debugging session into three.
func Check(c suite.Case, outcome runner.Outcome) []string {
	// What the flow DID comes first, and short-circuits. If it failed when it should
	// have completed, there is no message to check the body against, and "the flow
	// failed" followed by six complaints that its body was missing is noise burying the
	// one line that matters.
	if failures := checkOutcome(c.Expect, outcome); len(failures) > 0 {
		return failures
	}

	failures := message("expect", c.Expect.MessageExpect, outcome.Result)
	return append(failures, checkSpies(c, outcome)...)
}

// checkOutcome checks what the flow did: completed, dropped, or failed. This is the
// assertion every case makes, including the ones that make no others — a case with no
// `expect` at all is a smoke test, and a smoke test that goes green when the flow blew
// up is not a test.
func checkOutcome(want suite.Expectation, outcome runner.Outcome) []string {
	switch {
	case want.Error != "":
		return checkFailed(want.Error, outcome)
	case want.Dropped:
		if !outcome.Dropped {
			return []string{"expect.dropped: the flow did not drop the message, it " + whatItDid(outcome)}
		}
	case outcome.Error != "":
		return []string{"the flow failed: " + outcome.Error}
	case outcome.Dropped:
		return []string{"the flow dropped the message"}
	}
	return nil
}

// checkFailed checks a failure the case expected. The substring has to match: a case
// expecting "card declined" that passes on ANY failure is a test that proves the wrong
// thing, which is worse than no test.
func checkFailed(want string, outcome runner.Outcome) []string {
	if outcome.Error == "" {
		return []string{fmt.Sprintf("expect.error: the flow did not fail, it %s (wanted a failure containing %q)",
			whatItDid(outcome), want)}
	}
	if !strings.Contains(outcome.Error, want) {
		return []string{fmt.Sprintf("expect.error:\n      want a failure containing: %s\n      got:                       %s",
			want, outcome.Error)}
	}
	return nil
}

// whatItDid names the outcome, for an error message that has to say what happened
// instead of what was wanted.
func whatItDid(outcome runner.Outcome) string {
	switch {
	case outcome.Error != "":
		return "failed: " + outcome.Error
	case outcome.Dropped:
		return "dropped the message"
	default:
		return "completed"
	}
}

// checkSpies checks what each watched block saw.
func checkSpies(c suite.Case, outcome runner.Outcome) []string {
	var failures []string
	for _, address := range suite.SpiedBy(c) {
		want := c.Spies[address]
		records := outcome.Spies[address]
		where := fmt.Sprintf("spy %s", address)

		if want.Count != nil && len(records) != *want.Count {
			failures = append(failures, fmt.Sprintf("%s: crossed %s, want %d",
				where, plural(len(records), "time", "times"), *want.Count))
		}

		// A block inside a fork branch or a foreach body runs many times, and a case may
		// assert on only the first few. Naming MORE than happened is a failure, though —
		// the case is describing crossings that never occurred.
		if len(want.Records) > len(records) {
			failures = append(failures, fmt.Sprintf("%s: asserts on %d crossings, but it crossed %s",
				where, len(want.Records), plural(len(records), "time", "times")))
		}

		for i, wantRecord := range want.Records {
			if i >= len(records) {
				break // already reported above; do not repeat it once per missing record
			}
			failures = append(failures, checkRecord(fmt.Sprintf("%s record %d", where, i), wantRecord, records[i])...)
		}
	}
	return failures
}

// checkRecord checks one crossing of a spied block.
func checkRecord(where string, want suite.RecordExpect, got core.SpyRecord) []string {
	var failures []string

	// The input is always there, even for a block that failed: the spy clones the
	// message BEFORE running the block, precisely so a failure still shows what it was
	// carrying when it happened.
	if want.Input != nil {
		failures = append(failures, message(where+" input", *want.Input, got.Input)...)
	}

	return append(failures, checkRecordOutcome(where, want, got)...)
}

// checkRecordOutcome checks how a crossing ended: the block returned a message, dropped
// it, or failed. A record has exactly one of those, as the engine's spy records it.
func checkRecordOutcome(where string, want suite.RecordExpect, got core.SpyRecord) []string {
	switch {
	case want.Error != "":
		if got.Error == "" {
			return []string{fmt.Sprintf("%s: the block did not fail, it %s (wanted a failure containing %q)",
				where, whatTheBlockDid(got), want.Error)}
		}
		if !strings.Contains(got.Error, want.Error) {
			return []string{fmt.Sprintf("%s.error:\n      want a failure containing: %s\n      got: %s",
				where, want.Error, got.Error)}
		}
	case want.Dropped:
		if !got.Dropped {
			return []string{fmt.Sprintf("%s: the block did not drop the message, it %s", where, whatTheBlockDid(got))}
		}
	case want.Output != nil:
		if got.Output == nil {
			return []string{fmt.Sprintf("%s: the block returned no message, it %s", where, whatTheBlockDid(got))}
		}
		return message(where+" output", *want.Output, got.Output)
	}
	return nil
}

// whatTheBlockDid names a crossing's outcome.
func whatTheBlockDid(got core.SpyRecord) string {
	switch {
	case got.Error != "":
		return "failed: " + got.Error
	case got.Dropped:
		return "dropped the message"
	default:
		return "returned a message"
	}
}

// plural renders a count with the right noun, so a report reads like a sentence rather
// than like a struct dump: "crossed 1 time", not "crossed 1 times".
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
