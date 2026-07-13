package core

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// SpyRecord is one execution of a spied block: the message that went in, and what
// came out — including the two outcomes that are not a message, a drop and a
// failure. Exactly one of Output, Dropped and Error is set.
//
// Input is the message as the block received it, captured before the block ran:
// blocks mutate the message in place and hand the same pointer back, so a record
// taken afterwards would show the input already overwritten by the output.
type SpyRecord struct {
	// Seq orders records across every spy, not just this one. It is what makes a
	// multi-spy trace readable: block A ran before block B, this fork branch before
	// that one.
	Seq     int64          `json:"seq"`
	At      time.Time      `json:"at"`
	Input   *types.Message `json:"input"`
	Output  *types.Message `json:"output,omitempty"`
	Dropped bool           `json:"dropped,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// Spies collects what crossed each addressed block. It is the debug seam behind the
// CLI's `invoke --spies`: the runtime wraps each addressed block in an implicit spy
// block, which records every message through it and otherwise leaves the flow alone.
//
// Like Breakpoint it is a collector rather than a return value, because a spied
// block can sit inside a composite that discards its sub-flow's message — a fork
// branch runs on a clone, an enrich body runs on a clone — so the records have to
// leave the flow out-of-band.
//
// Unlike Breakpoint it appends rather than keeping the first write: a block inside a
// fork branch, a foreach body or an enrich runs many times, and every crossing is
// worth seeing. Those runs are concurrent, so Record is safe for concurrent use.
type Spies struct {
	// order is the addresses as the user gave them, so a trace reads back in the
	// order it was asked for rather than in map order.
	order []string
	spies map[string]*spy

	// seq is shared across every address: one counter, one total order.
	seq atomic.Int64
}

// spy is the records collected for one address.
type spy struct {
	mu      sync.Mutex
	records []SpyRecord
}

// NewSpies returns a collector watching each of the given block addresses. The
// addresses are opaque here; the runtime resolves them against the flow config.
func NewSpies(addresses []string) (*Spies, error) {
	s := &Spies{
		order: make([]string, 0, len(addresses)),
		spies: make(map[string]*spy, len(addresses)),
	}
	for _, address := range addresses {
		if address == "" {
			return nil, fmt.Errorf("spy address is empty")
		}
		if _, dup := s.spies[address]; dup {
			return nil, fmt.Errorf("spy %q is listed twice", address)
		}
		s.order = append(s.order, address)
		s.spies[address] = &spy{}
	}
	return s, nil
}

// Addresses returns the spied block addresses, in the order they were given.
func (s *Spies) Addresses() []string { return s.order }

// Record appends rec to the spy at address, stamping it with the next sequence
// number. An address that is not spied is ignored rather than an error: only the
// injected spy blocks call this, and each was built for an address that is here.
//
// The caller passes messages it will not mutate afterwards — the spy block hands
// over Clones, because the live message keeps travelling.
func (s *Spies) Record(address string, rec SpyRecord) {
	target, ok := s.spies[address]
	if !ok {
		return
	}

	// Stamp under the lock, not before it: two concurrent branches could otherwise
	// take their numbers in one order and append in the other, leaving this address's
	// records out of sequence.
	target.mu.Lock()
	defer target.mu.Unlock()
	rec.Seq = s.seq.Add(1)
	target.records = append(target.records, rec)
}

// Records returns what crossed the block at address, in the order it crossed. An
// empty result is a normal outcome, not a failure: the flow may simply have taken a
// branch that does not contain the block.
func (s *Spies) Records(address string) []SpyRecord {
	target, ok := s.spies[address]
	if !ok {
		return nil
	}

	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]SpyRecord(nil), target.records...)
}
