package core_test

import (
	"sync"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// concurrentRecorders is how many goroutines TestBreakpointFirstRecordWins races
// into Record, enough for -race to see an unguarded write.
const concurrentRecorders = 8

func message(t *testing.T, marker string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.Variables.Set("marker", marker)
	return msg
}

func TestBreakpointNotReachedUntilRecorded(t *testing.T) {
	bp := core.NewBreakpoint("orders.charge")

	if got := bp.Address(); got != "orders.charge" {
		t.Errorf("Address() = %q, want %q", got, "orders.charge")
	}
	if snapshot, ok := bp.Snapshot(); ok || snapshot != nil {
		t.Errorf("Snapshot() = (%v, %v), want (nil, false) before the block is reached", snapshot, ok)
	}
}

func TestBreakpointRecordsSnapshot(t *testing.T) {
	bp := core.NewBreakpoint("orders.charge")
	bp.Record(message(t, "first"))

	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("Snapshot() reports not reached after Record")
	}
	if marker, _ := snapshot.Variables.String("marker"); marker != "first" {
		t.Errorf("snapshot marker = %q, want %q", marker, "first")
	}
}

// TestBreakpointFirstRecordWins pins both halves of the contract: a breakpoint
// reports the first time execution reached the block, and Record is safe to call
// from the several goroutines a flow runs (worker pool, fork branches). Under
// -race an unguarded snapshot field fails here.
func TestBreakpointFirstRecordWins(t *testing.T) {
	bp := core.NewBreakpoint("orders.charge")
	bp.Record(message(t, "first"))

	var wg sync.WaitGroup
	wg.Add(concurrentRecorders)
	for range concurrentRecorders {
		go func() {
			defer wg.Done()
			bp.Record(message(t, "later"))
		}()
	}
	wg.Wait()

	snapshot, ok := bp.Snapshot()
	if !ok {
		t.Fatal("Snapshot() reports not reached after Record")
	}
	if marker, _ := snapshot.Variables.String("marker"); marker != "first" {
		t.Errorf("snapshot marker = %q, want %q: a later Record must not overwrite the first", marker, "first")
	}
}
