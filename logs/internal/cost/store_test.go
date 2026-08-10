package cost

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestStore opens a pool against TEST_DATABASE_URL, skipping the test when it
// is unset so `go test ./...` stays green without a database — the same shape the
// orchestrator's repo tests use.
//
// Each test gets a source name of its own. Source is part of the card's unique
// key, so scoping by it isolates concurrent tests completely and makes cleanup a
// single delete, with no shared fixture to reset between runs.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run price store tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	source := "test-" + t.Name()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM llm_prices WHERE source = $1`, source)
		_, _ = pool.Exec(ctx, `DELETE FROM llm_price_syncs WHERE source = $1`, source)
		pool.Close()
	})
	return NewStore(pool), source
}

// apply runs a fetched card against the store the way a refresh does: read what
// is open, diff, write, then read back. Reading back is not a test convenience —
// it is how a refresh learns the ids that priced records point at.
func apply(t *testing.T, store *Store, source string, fetched []Rate) []Rate {
	t.Helper()
	ctx := context.Background()

	current, err := store.Current(ctx, source)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if err := store.Apply(ctx, source, Diff(current, fetched)); err != nil {
		t.Fatalf("apply: %v", err)
	}
	reloaded, err := store.Current(ctx, source)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return reloaded
}

func TestStoreRoundTripsACard(t *testing.T) {
	store, source := newTestStore(t)

	rate := Rate{
		Provider: "ANTHROPIC", Pattern: "claude-3-5-sonnet-20241022", Operator: OpEquals,
		InputPer1M: 3, OutputPer1M: 15,
		CacheReadPer1M: ptr(0.3), CacheWritePer1M: ptr(3.75), Preferred: true,
	}
	got := apply(t, store, source, []Rate{rate})

	if len(got) != 1 {
		t.Fatalf("stored %d rates, want 1", len(got))
	}
	stored := got[0]
	switch {
	case stored.ID == "":
		t.Error("the stored rate has no id; nothing could point back at it")
	case stored.Provider != "ANTHROPIC" || stored.Pattern != "claude-3-5-sonnet-20241022":
		t.Errorf("identity = %s/%s, want ANTHROPIC/claude-3-5-sonnet-20241022", stored.Provider, stored.Pattern)
	case stored.Operator != OpEquals:
		t.Errorf("operator = %q, want %q", stored.Operator, OpEquals)
	case stored.InputPer1M != 3 || stored.OutputPer1M != 15:
		t.Errorf("rates = %v/%v, want 3/15", stored.InputPer1M, stored.OutputPer1M)
	case stored.CacheReadPer1M == nil || *stored.CacheReadPer1M != 0.3:
		t.Errorf("cache read = %v, want 0.3", stored.CacheReadPer1M)
	case stored.CacheWritePer1M == nil || *stored.CacheWritePer1M != 3.75:
		t.Errorf("cache write = %v, want 3.75", stored.CacheWritePer1M)
	case !stored.Preferred:
		t.Error("the current-price flag did not survive the round trip")
	}
}

// An absent cache rate must come back absent, not as zero. Pricing charges cached
// tokens at the input rate when it is absent and at nothing when it is zero, so
// collapsing the two here would silently change what every cached call costs.
func TestStoreKeepsAbsentCacheRatesAbsent(t *testing.T) {
	store, source := newTestStore(t)

	got := apply(t, store, source, []Rate{{
		Provider: "OPENAI", Pattern: "text-embedding-3-small", Operator: OpEquals,
		InputPer1M: 0.02, OutputPer1M: 0,
	}})

	if len(got) != 1 {
		t.Fatalf("stored %d rates, want 1", len(got))
	}
	if got[0].CacheReadPer1M != nil || got[0].CacheWritePer1M != nil {
		t.Fatalf("absent cache rates came back as %v/%v, want nil", got[0].CacheReadPer1M, got[0].CacheWritePer1M)
	}
}

// A refresh that changed nothing must not write anything: the ids priced records
// point at have to survive a card being re-fetched unchanged.
func TestStoreRefreshingAnUnchangedCardWritesNothing(t *testing.T) {
	store, source := newTestStore(t)
	card := []Rate{{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10}}

	first := apply(t, store, source, card)
	second := apply(t, store, source, card)

	if len(second) != 1 {
		t.Fatalf("stored %d rates after an unchanged refresh, want 1", len(second))
	}
	if second[0].ID != first[0].ID {
		t.Fatalf("an unchanged refresh replaced row %q with %q", first[0].ID, second[0].ID)
	}
}

// A price change closes the old row and opens a new one, so the cost frozen onto
// a record still resolves to the rate that produced it.
func TestStoreSupersedesRatherThanUpdates(t *testing.T) {
	store, source := newTestStore(t)
	ctx := context.Background()

	was := apply(t, store, source, []Rate{
		{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 5, OutputPer1M: 15},
	})
	now := apply(t, store, source, []Rate{
		{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10},
	})

	if len(now) != 1 {
		t.Fatalf("%d rows are open, want 1", len(now))
	}
	if now[0].ID == was[0].ID {
		t.Fatal("the price was updated in place; the old rate is gone")
	}
	if now[0].InputPer1M != 2.5 {
		t.Fatalf("open rate = %v, want 2.5", now[0].InputPer1M)
	}

	// The superseded row is still there, closed, with its original price.
	var total int
	var oldPrice float64
	// coalesce so the count below is what reports a supersede that stopped
	// closing rows. Without it max() over no rows is NULL, Scan fails, and the
	// test blames itself for a scan error instead.
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*), coalesce(max(input_per_1m), 0) FROM llm_prices
		  WHERE source = $1 AND effective_to IS NOT NULL`, source).Scan(&total, &oldPrice); err != nil {
		t.Fatalf("count closed rows: %v", err)
	}
	if total != 1 {
		t.Fatalf("%d closed rows, want 1", total)
	}
	if oldPrice != 5 {
		t.Fatalf("the closed row holds %v, want the original 5", oldPrice)
	}
}

// A pattern the fetch stopped carrying stays open. A model vanishing from a
// catalogue is a fact about the catalogue, not about the model, and closing the
// row would unprice calls the rate still explains.
func TestStoreLeavesVanishedPatternsOpen(t *testing.T) {
	store, source := newTestStore(t)

	apply(t, store, source, []Rate{
		{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10},
		{Provider: "OPENAI", Pattern: "gpt-legacy", Operator: OpEquals, InputPer1M: 30, OutputPer1M: 60},
	})
	after := apply(t, store, source, []Rate{
		{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10},
	})

	if len(after) != 2 {
		t.Fatalf("%d rows are open, want 2 — the vanished pattern was closed", len(after))
	}
	if _, ok := NewTable(after).Rate("gpt-legacy"); !ok {
		t.Fatal("a pattern the feed stopped carrying is no longer priced")
	}
}

// A rejected apply must leave the previous card exactly as it was, rather than
// half-written: the card is uniquely indexed on one open row per key, and a key
// left with two open rows or none is a state nothing else here can read.
//
// Note what this does and does not prove. It proves the outcome; it does not
// isolate the explicit transaction in Apply, because Postgres already runs a
// single pipelined batch as an implicit transaction and the test passes without
// the BEGIN. See Apply for why the BEGIN is kept anyway.
func TestStoreRejectedApplyLeavesTheCardIntact(t *testing.T) {
	store, source := newTestStore(t)
	ctx := context.Background()

	before := apply(t, store, source, []Rate{
		{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 5, OutputPer1M: 15},
	})

	// A change whose supersedes points at nothing: the close matches no row, so
	// the insert collides with the row that is still open.
	err := store.Apply(ctx, source, []Change{{
		Rate:       Rate{Provider: "OPENAI", Pattern: "gpt-4o", Operator: OpEquals, InputPer1M: 2.5, OutputPer1M: 10},
		Supersedes: "",
	}})
	if err == nil {
		t.Fatal("a colliding apply succeeded, want it rejected")
	}

	after, err := store.Current(ctx, source)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].InputPer1M != 5 {
		t.Fatalf("the rejected apply disturbed the card: %+v", after)
	}
}

func TestStoreRecordsSyncOutcomes(t *testing.T) {
	store, source := newTestStore(t)
	ctx := context.Background()

	if err := store.RecordSync(ctx, Sync{Source: source, Models: 1095, Added: 1095, Changed: 0}); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if err := store.RecordSync(ctx, Sync{Source: source, Err: errors.New("catalogue unreachable")}); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	var ok, failed int
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE ok), count(*) FILTER (WHERE NOT ok)
		   FROM llm_price_syncs WHERE source = $1`, source).Scan(&ok, &failed); err != nil {
		t.Fatalf("count syncs: %v", err)
	}
	if ok != 1 || failed != 1 {
		t.Fatalf("recorded %d ok and %d failed, want 1 and 1", ok, failed)
	}

	var message string
	if err := store.pool.QueryRow(ctx,
		`SELECT error FROM llm_price_syncs WHERE source = $1 AND NOT ok`, source).Scan(&message); err != nil {
		t.Fatalf("read failure reason: %v", err)
	}
	if message != "catalogue unreachable" {
		t.Fatalf("failure reason = %q, want it kept verbatim", message)
	}
}

// The first load is a full card, which is the case where a per-row round trip
// would be felt. It must land in one apply and be readable back in full.
func TestStoreAppliesAWholeCard(t *testing.T) {
	store, source := newTestStore(t)

	fetched, err := serveFixture(t).Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	got := apply(t, store, source, fetched.Rates)

	if len(got) != len(fetched.Rates) {
		t.Fatalf("stored %d of %d fetched rates", len(got), len(fetched.Rates))
	}
	if _, ok := NewTable(got).Rate("claude-3-5-sonnet-20241022"); !ok {
		t.Fatal("the stored card does not price a model the fetched one did")
	}
}
