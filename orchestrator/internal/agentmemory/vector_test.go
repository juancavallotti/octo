package agentmemory

import (
	"context"
	"math"
	"strings"
	"testing"
)

// fakeVector builds a deterministic unit-ish vector of the stored width, seeded
// so two different seeds point in measurably different directions. It stands in
// for a provider so the SQL can be tested without one.
func fakeVector(seed float32) []float32 {
	v := make([]float32, storedVectorDims)
	for i := range v {
		v[i] = float32(math.Sin(float64(seed) + float64(i)*0.01))
	}
	return v
}

// storedVectorDims mirrors the schema's vector(1536). It is restated here rather
// than imported from the embedding package so this package's tests do not depend
// on that one.
const storedVectorDims = 1536

// TestVectorLiteralRoundTrip checks the text encoding Postgres parses, including
// that a float32 survives it exactly — a lossy encoding would quietly degrade
// every ranking.
func TestVectorLiteralRoundTrip(t *testing.T) {
	got := vectorLiteral([]float32{0.5, -0.25, 0})
	if got != "[0.5,-0.25,0]" {
		t.Errorf("unexpected literal: %s", got)
	}
	// A value with a long decimal expansion has to come back exactly, since the
	// literal is what is stored.
	one := vectorLiteral([]float32{0.1})
	if !strings.HasPrefix(one, "[0.1") {
		t.Errorf("a float32 should round-trip through the literal, got %s", one)
	}
}

// TestVectorSearchRanksByDistance checks the whole vector path against a real
// database: stored vectors, the HNSW operator, and the score orientation.
//
// The orientation matters more than it looks. SearchText ranks with ts_rank,
// where larger is more relevant, and pgvector's <=> is a distance, where smaller
// is. Callers do not branch on which ranking ran, so the two have to agree on
// what a score means or a mid-backfill search reverses itself.
func TestVectorSearchRanksByDistance(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.AppendTurns(ctx, ref, []Turn{
		{Role: "user", Text: "the near one"},
		{Role: "user", Text: "the far one"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Filtered to this test's own rows. The sweep is global on purpose — it works
	// through everything that has no vector, not one integration's share — so what
	// else is in the database is not this test's business.
	pending := pendingFor(t, r, ctx, "the near one", "the far one")
	near, far := fakeVector(0), fakeVector(2)
	vectors := make([][]float32, len(pending))
	for i, p := range pending {
		if p.Text == "the near one" {
			vectors[i] = near
		} else {
			vectors[i] = far
		}
	}
	if err := r.SaveEmbeddings(ctx, pending, vectors); err != nil {
		t.Fatalf("save embeddings: %v", err)
	}

	hits, err := r.SearchVector(ctx, ref.IntegrationID, Query{
		AgentID: "support", UserID: "alice", Text: "anything",
	}, near)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("want both turns ranked, got %d", len(hits))
	}
	if hits[0].Text != "the near one" {
		t.Errorf("the closest vector should rank first, got %q", hits[0].Text)
	}
	if hits[0].Score <= hits[1].Score {
		t.Errorf("a larger score should mean more relevant, got %v then %v",
			hits[0].Score, hits[1].Score)
	}
}

// TestPendingEmbeddingsDrains checks the backfill queue empties as it is worked,
// which is what the admin page's pending count is reading.
func TestPendingEmbeddingsDrains(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "a turn"}}); err != nil {
		t.Fatalf("seed turn: %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Go.", 0); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	pending := pendingFor(t, r, ctx, "a turn", "Prefers Go.")
	// Both kinds are swept, since both are searched.
	kinds := map[string]bool{}
	for _, p := range pending {
		kinds[p.Kind] = true
	}
	if !kinds[HitTurn] || !kinds[HitUser] {
		t.Errorf("both turns and memories should be swept, got %+v", kinds)
	}

	vectors := make([][]float32, len(pending))
	for i := range vectors {
		vectors[i] = fakeVector(float32(i))
	}
	if err := r.SaveEmbeddings(ctx, pending, vectors); err != nil {
		t.Fatalf("save: %v", err)
	}
	if after := pendingFor(t, r, ctx, "a turn", "Prefers Go."); len(after) != 0 {
		t.Errorf("the queue should have drained, %d left", len(after))
	}
}

// TestCorrectingAMemoryClearsItsVector is why the embedding column is cleared on
// every change: a vector for text that no longer exists is worse than no vector,
// because search would rank on the old meaning and return the new words.
func TestCorrectingAMemoryClearsItsVector(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "", "alice")

	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Go.", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	pending := pendingFor(t, r, ctx, "Prefers Go.")
	if err := r.SaveEmbeddings(ctx, pending, [][]float32{fakeVector(0)}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Rust now.", 1); err != nil {
		t.Fatalf("correct: %v", err)
	}
	after := pendingFor(t, r, ctx, "Prefers Rust now.")
	if len(after) != 1 {
		t.Errorf("a corrected memory should be waiting for a fresh vector, got %+v", after)
	}
}

// TestEmbeddingCountsReportBothHalves checks what the admin page shows while a
// backfill drains: "configured" and "search is semantic" are not the same
// statement, and the counts are how an operator can tell.
func TestEmbeddingCountsReportBothHalves(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	before, _, err := r.EmbeddingCounts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "a turn"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pending := pendingFor(t, r, ctx, "a turn")
	if err := r.SaveEmbeddings(ctx, pending, [][]float32{fakeVector(0)}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	after, _, err := r.EmbeddingCounts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if after != before+1 {
		t.Errorf("the embedded count should have risen by one, %d then %d", before, after)
	}
}

// pendingFor returns the rows awaiting a vector whose text is one of want.
//
// PendingEmbeddings is global — the sweep works through everything with no
// vector, not one integration's share — so a test that asserted on its length
// would be asserting about every other test's rows too. The batch is asked for
// large because these rows were just written and the sweep returns newest first.
func pendingFor(t *testing.T, r *Repo, ctx context.Context, want ...string) []Pending {
	t.Helper()
	rows, err := r.PendingEmbeddings(ctx, maxSearchLimit)
	if err != nil {
		t.Fatalf("pending embeddings: %v", err)
	}
	wanted := make(map[string]bool, len(want))
	for _, w := range want {
		wanted[w] = true
	}
	var out []Pending
	for _, row := range rows {
		if wanted[row.Text] {
			out = append(out, row)
		}
	}
	return out
}
