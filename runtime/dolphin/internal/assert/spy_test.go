package assert

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/runner"
	"github.com/juancavallotti/octo/runtime/types"
)

// classify is the address the spy tests watch: a block inside a foreach body, which is
// the case that makes spies worth having — it runs once per item, and no other tool
// shows you that.
const classify = "demo.each-order[body].classify-order"

// spied is an outcome where the watched block was crossed with these records.
func spied(records ...core.SpyRecord) runner.Outcome {
	return runner.Outcome{
		Result: &types.Message{},
		Spies:  map[string][]core.SpyRecord{classify: records},
	}
}

// crossing is one record: a message in, a message out.
func crossing(t *testing.T, in, out string) core.SpyRecord {
	t.Helper()
	return core.SpyRecord{Input: msg(t, in, nil), Output: msg(t, out, nil)}
}

func TestSpyCount(t *testing.T) {
	c := caseFrom(t, "name: a\nspies:\n  "+classify+":\n    count: 2\n")

	t.Run("two crossings pass", func(t *testing.T) {
		outcome := spied(crossing(t, `{"id":1}`, `{"id":1}`), crossing(t, `{"id":2}`, `{"id":2}`))
		if got := Check(c, outcome); len(got) > 0 {
			t.Errorf("two crossings failed a case wanting two: %v", got)
		}
	})

	t.Run("one crossing fails, and reads like a sentence", func(t *testing.T) {
		got := Check(c, spied(crossing(t, `{"id":1}`, `{"id":1}`)))
		only(t, got, "crossed 1 time, want 2")
	})
}

// TestCountZeroAssertsTheBlockNeverRan. This is why Count is a *int and not an int: it is
// how a case proves an error path was NOT taken, or that a branch was not entered, and it
// has to be distinguishable from "no count given".
func TestCountZeroAssertsTheBlockNeverRan(t *testing.T) {
	c := caseFrom(t, "name: a\nspies:\n  "+classify+":\n    count: 0\n")

	t.Run("a block that never ran passes", func(t *testing.T) {
		if got := Check(c, spied()); len(got) > 0 {
			t.Errorf("a block that never ran failed a case wanting 0 crossings: %v", got)
		}
	})

	t.Run("a block that ran fails", func(t *testing.T) {
		got := Check(c, spied(crossing(t, `{"id":1}`, `{"id":1}`)))
		only(t, got, "crossed 1 time, want 0")
	})
}

// TestSpyRecordsArePositional: a block in a foreach body runs once per item, and a case
// checks the i-th crossing.
func TestSpyRecordsArePositional(t *testing.T) {
	c := caseFrom(t, `
name: a
spies:
  demo.each-order[body].classify-order:
    count: 2
    records:
      - input: { that: ['body.id == 1', 'body.amount == 50'] }
      - input: { that: ['body.id == 2'] }
        output: { body: { id: 2, amount: 250, tier: high } }
`)
	outcome := spied(
		crossing(t, `{"id":1,"amount":50}`, `{"id":1,"amount":50}`),
		crossing(t, `{"id":2,"amount":250}`, `{"id":2,"amount":250,"tier":"high"}`),
	)

	if got := Check(c, outcome); len(got) > 0 {
		t.Errorf("the crossings matched but were reported as failing:\n  %v", got)
	}
}

// TestAFailingRecordNamesTheCrossing: with a block that ran five times, "the body was
// wrong" is useless unless it says WHICH crossing.
func TestAFailingRecordNamesTheCrossing(t *testing.T) {
	c := caseFrom(t, `
name: a
spies:
  demo.each-order[body].classify-order:
    records:
      - input: { that: ['body.id == 1'] }
      - input: { that: ['body.id == 99'] }
`)
	outcome := spied(
		crossing(t, `{"id":1}`, `{"id":1}`),
		crossing(t, `{"id":2}`, `{"id":2}`),
	)

	got := Check(c, outcome)
	only(t, got, "spy demo.each-order[body].classify-order record 1 input.that: body.id == 99 is false")
}

// TestAssertingOnMoreCrossingsThanHappened is a failure, and it is reported once — not
// once per record the case named beyond the end.
func TestAssertingOnMoreCrossingsThanHappened(t *testing.T) {
	c := caseFrom(t, `
name: a
spies:
  demo.each-order[body].classify-order:
    records:
      - input: { that: ['body.id == 1'] }
      - input: { that: ['body.id == 2'] }
      - input: { that: ['body.id == 3'] }
`)
	got := Check(c, spied(crossing(t, `{"id":1}`, `{"id":1}`)))

	only(t, got, "asserts on 3 crossings, but it crossed 1 time")
}

// TestSpyOnABlockThatFailed: the INPUT is still there, because the spy clones the message
// before running the block — precisely so a failure still shows what it was carrying when
// it happened. That is the whole reason to spy on a block you have mocked into failing.
func TestSpyOnABlockThatFailed(t *testing.T) {
	c := caseFrom(t, `
name: a
spies:
  demo.each-order[body].classify-order:
    count: 1
    records:
      - input: { that: ['body.amount == 500'] }
        error: card declined
`)
	outcome := spied(core.SpyRecord{Input: msg(t, `{"amount":500}`, nil), Error: "card declined"})

	if got := Check(c, outcome); len(got) > 0 {
		t.Errorf("a block that failed as the case said it would was reported as failing:\n  %v", got)
	}
}

// TestSpyOutcomeMismatches: a block that returned a message when the case said it would
// fail, and the reverse.
func TestSpyOutcomeMismatches(t *testing.T) {
	t.Run("the case expected a failure that did not happen", func(t *testing.T) {
		c := caseFrom(t, "name: a\nspies:\n  "+classify+":\n    records:\n      - error: card declined\n")

		got := Check(c, spied(crossing(t, `{"a":1}`, `{"a":1}`)))
		only(t, got, "the block did not fail, it returned a message")
	})

	t.Run("the case expected an output from a block that dropped", func(t *testing.T) {
		c := caseFrom(t, "name: a\nspies:\n  "+classify+":\n    records:\n      - output: { body: { a: 1 } }\n")

		got := Check(c, spied(core.SpyRecord{Input: msg(t, `{"a":1}`, nil), Dropped: true}))
		only(t, got, "the block returned no message, it dropped the message")
	})

	t.Run("the case expected a drop that did not happen", func(t *testing.T) {
		c := caseFrom(t, "name: a\nspies:\n  "+classify+":\n    records:\n      - dropped: true\n")

		got := Check(c, spied(crossing(t, `{"a":1}`, `{"a":1}`)))
		only(t, got, "the block did not drop the message, it returned a message")
	})
}

// TestAnUncrossedSpyIsNotAnError unless the case says so. A spied block on a branch the
// message did not take reports no records, and that is a result — it is exactly what
// `count: 0` is for. A case that asserts nothing about it is asserting nothing about it.
func TestAnUncrossedSpyIsNotAnError(t *testing.T) {
	c := caseFrom(t, "name: a\nspies:\n  "+classify+": {}\n")

	if got := Check(c, spied()); len(got) > 0 {
		t.Errorf("a spy that recorded nothing failed a case that asserted nothing about it: %v", got)
	}
}
