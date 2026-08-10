package ingest

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
)

// dropWarnInterval rate-limits the overflow warning. The condition that produces
// it produces it thousands of times a second, and a log line per dropped record
// turns shedding load into a second, worse flood.
const dropWarnInterval = 10 * time.Second

// shedder is how a consumer hands a delivered record to its writer without ever
// blocking, and what it does when there is no room.
//
// Blocking in a NATS callback does not stall the other subscriptions — the client
// runs a delivery goroutine per subscription — but it backs records up in this
// one's pending queue until the client hits its own limit and drops them as a
// slow consumer. That is the same loss, arriving as a client-side error with no
// count of what went or which app lost it. Shedding here loses the same records
// and keeps the accounting.
//
// Both consumers embed it so the two cannot drift into answering the same
// question differently — what to do under pressure should be a decision, not a
// side effect of how each one happened to size its buffer.
type shedder struct {
	// what names the records being shed, for the warning. "trace" or "log".
	what string

	dropped atomic.Int64

	warnMu   sync.Mutex
	nextWarn time.Time
}

// Dropped is the running total of records shed because the writer could not keep
// up. It is the number the overflow warning names, exposed so a caller can report
// it without parsing logs.
func (s *shedder) Dropped() int64 {
	return s.dropped.Load()
}

// offer hands a delivered record to the writer, shedding it if there is no room.
// It never blocks.
func (s *shedder) offer(work chan<- *nats.Msg, m *nats.Msg) {
	select {
	case work <- m:
	default:
		s.overflow()
	}
}

// overflow counts a shed record and warns about it at most occasionally, naming
// the running total so one line says how bad it has got rather than how recently.
func (s *shedder) overflow() {
	total := s.dropped.Add(1)

	s.warnMu.Lock()
	defer s.warnMu.Unlock()
	now := time.Now()
	if now.Before(s.nextWarn) {
		return
	}
	s.nextWarn = now.Add(dropWarnInterval)
	slog.Warn("ingest: dropping records, the writer is behind", "kind", s.what, "dropped", total)
}
