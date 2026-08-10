package cost

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// defaultRefreshInterval is how often the published card is re-read. Prices move
// on the order of months, so this is about not drifting indefinitely rather than
// about being current to the minute.
const defaultRefreshInterval = 12 * time.Hour

// fetcher is the part of a Catalogue a Refresher uses. Declared here, where it is
// consumed, so the cycle can be driven from a fake without an HTTP server.
type fetcher interface {
	Fetch(ctx context.Context) (Fetched, error)
}

// prices is the storage a Refresher uses, declared here for the same reason: the
// interesting behaviour — serving stored rates before the first fetch, holding
// the previous card when a fetch fails — has to be testable without a database.
type prices interface {
	Current(ctx context.Context, source string) ([]Rate, error)
	Apply(ctx context.Context, source string, changes []Change) error
	RecordSync(ctx context.Context, sync Sync) error
}

// Refresher keeps the rate card current and hands it to whoever is pricing.
//
// The card is swapped as a whole behind an atomic pointer rather than mutated in
// place. Pricing happens on the ingest path, once per model call, and a lock
// there would be contended by every record for the benefit of a writer that runs
// twice a day.
type Refresher struct {
	store     prices
	catalogue fetcher
	source    string
	interval  time.Duration
	card      atomic.Pointer[Table]
}

// NewRefresher returns a refresher for one source. A non-positive interval falls
// back to defaultRefreshInterval.
func NewRefresher(store prices, catalogue fetcher, source string, interval time.Duration) *Refresher {
	if interval <= 0 {
		interval = defaultRefreshInterval
	}
	return &Refresher{store: store, catalogue: catalogue, source: source, interval: interval}
}

// Card returns the rate card in force, which is nil until something publishes
// one. Callers need not check: every method on Table handles a nil receiver by
// reporting that it prices nothing.
func (r *Refresher) Card() *Table {
	return r.card.Load()
}

// Price prices one model call against the card in force. It is the whole of what
// the ingest path needs from this package.
func (r *Refresher) Price(call Call) Priced {
	return r.Card().Price(call)
}

// Load publishes the stored card without contacting the feed.
//
// This is what startup calls, and the ordering is deliberate: a process that
// waited for a fetch before serving would price nothing at all while the feed was
// slow, and price nothing for as long as the feed was down. Rates already in the
// database are the answer to both.
func (r *Refresher) Load(ctx context.Context) error {
	stored, err := r.store.Current(ctx, r.source)
	if err != nil {
		return err
	}
	r.publish(stored)
	slog.Info("loaded stored rate card", "source", r.source, "rates", r.Card().Len())
	return nil
}

// Run refreshes the card until ctx is done.
//
// It refreshes once immediately rather than waiting out the first interval: a
// process that just started has no reason to serve a card half a day older than
// the one it could have.
func (r *Refresher) Run(ctx context.Context) {
	r.cycle(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.cycle(ctx)
		}
	}
}

// cycle runs one refresh and records what happened, including a failure.
func (r *Refresher) cycle(ctx context.Context) {
	sync, err := r.refresh(ctx)
	if err != nil {
		// The previous card stays in force. A feed being unreachable is not a
		// reason to stop pricing with rates that were correct an hour ago.
		slog.Error("rate card refresh failed", "source", r.source, "err", err, "rates", r.Card().Len())
	} else {
		slog.Info("refreshed rate card",
			"source", r.source, "models", sync.Models,
			"added", sync.Added, "changed", sync.Changed, "rates", r.Card().Len())
	}

	if err := r.store.RecordSync(ctx, sync); err != nil {
		slog.Error("recording the refresh outcome failed", "source", r.source, "err", err)
	}
}

// refresh fetches, applies what changed, and republishes the card.
//
// The republish happens even when the apply failed, which matters when several
// replicas refresh at once: the card is uniquely indexed on one open row per key,
// so the second replica to write the same change is rejected — and re-reading is
// what lets it pick up the row the first one just wrote instead of holding a
// stale card until the next tick.
func (r *Refresher) refresh(ctx context.Context) (Sync, error) {
	sync := Sync{Source: r.source}

	fetched, err := r.catalogue.Fetch(ctx)
	if err != nil {
		sync.Err = err
		return sync, err
	}
	sync.Models = len(fetched.Rates)

	stored, err := r.store.Current(ctx, r.source)
	if err != nil {
		sync.Err = err
		return sync, err
	}

	changes := Diff(stored, fetched.Rates)
	for _, change := range changes {
		if change.Supersedes == "" {
			sync.Added++
			continue
		}
		sync.Changed++
	}

	applyErr := r.store.Apply(ctx, r.source, changes)

	// Read back rather than trusting what was sent: the ids assigned by the
	// insert are what every priced record points at, and a rate built from the
	// fetch would carry none.
	current, err := r.store.Current(ctx, r.source)
	if err == nil {
		r.publish(current)
	}

	switch {
	case applyErr != nil:
		sync.Err = applyErr
	case err != nil:
		sync.Err = err
	default:
		return sync, nil
	}
	return sync, sync.Err
}

// publish swaps in a card built from rates. The swap is a single pointer store,
// so a reader sees either the whole previous card or the whole new one.
func (r *Refresher) publish(rates []Rate) {
	r.card.Store(NewTable(rates))
}
