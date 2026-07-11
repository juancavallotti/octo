package core

import (
	"sync"

	"github.com/juancavallotti/octo/types"
)

// Breakpoint collects the message a flow was carrying when execution reached an
// addressed block. It is the debug seam behind the CLI's `invoke --break-at`: the
// runtime wraps the addressed block in an implicit breakpoint block, which runs the
// block, records the resulting message here, and halts the flow.
//
// It is a collector rather than a return value because a block can sit inside a
// composite that discards its sub-flow's message — a fork branch runs on a clone,
// an enrich body runs on a clone — so the snapshot has to leave the flow
// out-of-band. Address is carried along so a caller that only holds the Breakpoint
// can report which block it was waiting on.
//
// A flow runs its blocks across several workers, and a fork runs branches
// concurrently, so Record may be called from more than one goroutine. The first
// call wins: a breakpoint reports the first time execution reached the block, and
// later arrivals are ignored rather than overwriting it.
type Breakpoint struct {
	address string

	mu       sync.Mutex
	snapshot *types.Message
}

// NewBreakpoint returns a Breakpoint waiting on the block at address. The address
// is opaque here; the runtime resolves it against the flow config.
func NewBreakpoint(address string) *Breakpoint {
	return &Breakpoint{address: address}
}

// Address returns the block address this breakpoint waits on.
func (b *Breakpoint) Address() string { return b.address }

// Record captures msg as the snapshot, unless one was already recorded. The caller
// passes a message it will not mutate afterwards — the breakpoint block hands over
// a Clone, because the live message keeps travelling (a fork's siblings run on,
// and an enrich folds its result back) after the flow has been asked to stop.
func (b *Breakpoint) Record(msg *types.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.snapshot != nil {
		return
	}
	b.snapshot = msg
}

// Snapshot returns the recorded message and whether execution ever reached the
// block. A false ok is a normal outcome, not a failure: the flow may simply have
// taken a branch that does not contain the block.
func (b *Breakpoint) Snapshot() (*types.Message, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.snapshot == nil {
		return nil, false
	}
	return b.snapshot, true
}
