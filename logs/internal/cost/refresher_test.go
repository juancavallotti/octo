package cost

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeStore is an in-memory stand-in for the price tables, so the refresh cycle
// is exercised without a database. It applies changes the way the real store
// does — close then open — because the id a rate comes back with is what the
// refresher is being tested for.
type fakeStore struct {
	mu      sync.Mutex
	open    []Rate
	syncs   []Sync
	nextID  int
	failOn  string // "current" or "apply", when that call should fail
	applied int
}

var errStoreUnavailable = errors.New("store unavailable")

func (f *fakeStore) Current(_ context.Context, _ string) ([]Rate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == "current" {
		return nil, errStoreUnavailable
	}
	return append([]Rate(nil), f.open...), nil
}

func (f *fakeStore) Apply(_ context.Context, _ string, changes []Change) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == "apply" {
		return errStoreUnavailable
	}
	f.applied++
	for _, change := range changes {
		if change.Supersedes != "" {
			f.open = deleteRate(f.open, change.Supersedes)
		}
		f.nextID++
		rate := change.Rate
		rate.ID = "stored-" + string(rune('a'+f.nextID-1))
		f.open = append(f.open, rate)
	}
	return nil
}

func (f *fakeStore) RecordSync(_ context.Context, sync Sync) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs = append(f.syncs, sync)
	return nil
}

func (f *fakeStore) outcomes() []Sync {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Sync(nil), f.syncs...)
}

func deleteRate(rates []Rate, id string) []Rate {
	out := rates[:0]
	for _, rate := range rates {
		if rate.ID != id {
			out = append(out, rate)
		}
	}
	return out
}

// fakeCatalogue serves a card, or an error, and counts how often it was asked.
type fakeCatalogue struct {
	mu    sync.Mutex
	rates []Rate
	err   error
	calls int
}

func (f *fakeCatalogue) Fetch(_ context.Context) (Fetched, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return Fetched{}, f.err
	}
	return Fetched{Rates: append([]Rate(nil), f.rates...)}, nil
}

func (f *fakeCatalogue) serve(rates []Rate, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rates, f.err = rates, err
}

func (f *fakeCatalogue) fetches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func gpt4o(in, out float64) Rate {
	return Rate{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: in, OutputPer1M: out}
}

// Startup must price from what the database already holds rather than waiting on
// the feed: a process that waited would price nothing while the feed was slow,
// and nothing at all for as long as it was down.
func TestLoadServesStoredRatesWithoutFetching(t *testing.T) {
	store := &fakeStore{open: []Rate{{
		ID: "row-1", Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals,
		InputPer1M: 2.5, OutputPer1M: 10,
	}}}
	catalogue := &fakeCatalogue{err: errors.New("the feed must not be contacted")}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)

	if err := refresher.Load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	if catalogue.fetches() != 0 {
		t.Fatalf("load contacted the feed %d times, want 0", catalogue.fetches())
	}

	rate, ok := refresher.Card().Rate("gpt-4o")
	if !ok {
		t.Fatal("the stored card is not pricing after load")
	}
	if rate.ID != "row-1" {
		t.Fatalf("rate id = %q, want the stored row's id", rate.ID)
	}
}

// Before anything is published there is no card. Pricing must report that
// honestly rather than panic or answer zero.
func TestUnloadedRefresherPricesNothing(t *testing.T) {
	refresher := NewRefresher(&fakeStore{}, &fakeCatalogue{}, SourceHelicone, time.Hour)

	if refresher.Card() != nil {
		t.Fatal("a card exists before anything published one")
	}
	got := refresher.Price(Call{Model: "gpt-4o", Usage: &Usage{InputTokens: 100}})
	if got.Status != StatusUnpricedModel {
		t.Fatalf("status = %q, want %q", got.Status, StatusUnpricedModel)
	}
	if _, known := got.CostUSD(); known {
		t.Fatal("a refresher with no card produced a known cost")
	}
}

// A refresh publishes rates carrying the ids the store assigned, since those are
// what every priced record points back at.
func TestRefreshPublishesStoredIdentities(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)

	sync, err := refresher.refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if sync.Added != 1 || sync.Changed != 0 || sync.Models != 1 {
		t.Fatalf("sync = %+v, want 1 model added", sync)
	}

	rate, ok := refresher.Card().Rate("gpt-4o")
	if !ok {
		t.Fatal("the refreshed card is not pricing")
	}
	if rate.ID == "" {
		t.Fatal("the published rate carries no id; nothing could point back at it")
	}
}

// A feed being unreachable is not a reason to stop pricing with rates that were
// correct an hour ago.
func TestFailedFetchKeepsThePreviousCard(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)

	if _, err := refresher.refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	before, _ := refresher.Card().Rate("gpt-4o")

	catalogue.serve(nil, errors.New("catalogue unreachable"))
	if _, err := refresher.refresh(context.Background()); err == nil {
		t.Fatal("a failed fetch reported success")
	}

	after, ok := refresher.Card().Rate("gpt-4o")
	if !ok {
		t.Fatal("a failed fetch left nothing pricing")
	}
	if after.ID != before.ID || after.InputPer1M != before.InputPer1M {
		t.Fatalf("a failed fetch disturbed the card: %+v, want %+v", after, before)
	}
}

// Several replicas refresh independently, and the card is uniquely indexed on one
// open row per key, so the second to write the same change is rejected. Reading
// back regardless is what lets the loser pick up the winner's row now instead of
// holding a stale card until the next tick.
func TestRejectedApplyStillRepublishes(t *testing.T) {
	store := &fakeStore{open: []Rate{{
		ID: "written-by-another-replica", Provider: "OPENAI", Pattern: "gpt-4o",
		Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10,
	}}, failOn: "apply"}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(1.25, 5)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)

	if _, err := refresher.refresh(context.Background()); err == nil {
		t.Fatal("a rejected apply reported success")
	}

	rate, ok := refresher.Card().Rate("gpt-4o")
	if !ok {
		t.Fatal("a rejected apply left nothing pricing")
	}
	if rate.ID != "written-by-another-replica" {
		t.Fatalf("rate id = %q, want the row the other replica wrote", rate.ID)
	}
}

// Every attempt is recorded, successful or not. A feed that quietly stopped
// answering looks exactly like a feed whose prices stopped changing, and only one
// of those means stored costs are drifting away from reality.
func TestCycleRecordsBothOutcomes(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)
	ctx := context.Background()

	refresher.cycle(ctx)
	catalogue.serve(nil, errors.New("catalogue unreachable"))
	refresher.cycle(ctx)

	got := store.outcomes()
	if len(got) != 2 {
		t.Fatalf("recorded %d outcomes, want 2", len(got))
	}
	if got[0].Err != nil {
		t.Errorf("the successful cycle recorded %v, want no error", got[0].Err)
	}
	if got[0].Added != 1 {
		t.Errorf("the successful cycle recorded %d added, want 1", got[0].Added)
	}
	if got[1].Err == nil {
		t.Error("the failed cycle recorded no error")
	}
}

// An unchanged refresh must write nothing, so the ids priced records point at
// survive a card being re-fetched.
func TestRefreshingAnUnchangedCardAppliesNothing(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)
	ctx := context.Background()

	if _, err := refresher.refresh(ctx); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	first, _ := refresher.Card().Rate("gpt-4o")

	sync, err := refresher.refresh(ctx)
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if sync.Added != 0 || sync.Changed != 0 {
		t.Fatalf("an unchanged refresh reported %d added and %d changed, want none", sync.Added, sync.Changed)
	}

	second, _ := refresher.Card().Rate("gpt-4o")
	if second.ID != first.ID {
		t.Fatalf("an unchanged refresh replaced row %q with %q", first.ID, second.ID)
	}
}

// Pricing happens on the ingest path while refreshes happen behind it. Run this
// under -race: the point is that a reader sees either the whole previous card or
// the whole new one, never a card mid-rebuild.
func TestCardSwapsUnderConcurrentPricing(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)
	ctx := context.Background()
	if _, err := refresher.refresh(ctx); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				got := refresher.Price(Call{Model: "gpt-4o", Usage: &Usage{InputTokens: 1000}})
				if _, known := got.CostUSD(); !known {
					// A swap must never expose a window where the model is
					// unpriced; the card is replaced whole.
					t.Errorf("gpt-4o was unpriced during a swap")
					return
				}
			}
		}()
	}

	for i := range 50 {
		catalogue.serve([]Rate{gpt4o(2.5+float64(i), 10)}, nil)
		if _, err := refresher.refresh(ctx); err != nil {
			t.Errorf("refresh %d: %v", i, err)
			break
		}
	}
	close(done)
	readers.Wait()
}

// Run refreshes immediately rather than waiting out the first interval, and stops
// when its context is cancelled.
func TestRunRefreshesAtOnceAndStopsOnCancel(t *testing.T) {
	store := &fakeStore{}
	catalogue := &fakeCatalogue{rates: []Rate{gpt4o(2.5, 10)}}
	refresher := NewRefresher(store, catalogue, SourceHelicone, time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		refresher.Run(ctx)
		close(stopped)
	}()

	deadline := time.After(2 * time.Second)
	for catalogue.fetches() == 0 {
		select {
		case <-deadline:
			t.Fatal("Run did not refresh before its first interval elapsed")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop when its context was cancelled")
	}
}

func TestNewRefresherDefaultsTheInterval(t *testing.T) {
	for _, given := range []time.Duration{0, -time.Second} {
		got := NewRefresher(&fakeStore{}, &fakeCatalogue{}, SourceHelicone, given)
		if got.interval != defaultRefreshInterval {
			t.Errorf("interval for %v = %v, want %v", given, got.interval, defaultRefreshInterval)
		}
	}
}
