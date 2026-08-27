package agentmemory

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestRepo returns a Repo against TEST_DATABASE_URL, skipping when it is not
// set so `go test ./...` stays green without a live database.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run integration repo tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewRepo(pool)
}

// testRef makes a Ref under a fresh integration id, so tests do not see each
// other's rows and cleanup is one delete.
func testRef(t *testing.T, r *Repo, agentID, threadKey, userID string) Ref {
	t.Helper()
	var id string
	err := r.pool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&id)
	if err != nil {
		t.Fatalf("mint integration id: %v", err)
	}
	t.Cleanup(func() {
		_ = r.DeleteForIntegration(context.Background(), id)
	})
	return Ref{IntegrationID: id, AgentID: agentID, ThreadKey: threadKey, UserID: userID}
}

// TestRepoWorkingRoundTrip covers the store-and-resume path, including the
// version a caller has to send back.
func TestRepoWorkingRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, ok, err := r.LoadWorking(ctx, ref); err != nil || ok {
		t.Fatalf("a new conversation has no working memory (ok=%v err=%v)", ok, err)
	}
	v, err := r.SaveWorking(ctx, ref, Working{Payload: []byte(`{"m":1}`), Tokens: 9, Iteration: 3})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if v != 1 {
		t.Errorf("a first write is version 1, got %d", v)
	}
	got, ok, err := r.LoadWorking(ctx, ref)
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if string(got.Payload) != `{"m":1}` || got.Tokens != 9 || got.Iteration != 3 {
		t.Errorf("round trip lost something: %+v", got)
	}
}

// TestRepoWorkingVersionConflict is the optimistic-concurrency contract: a write
// against a version something else has moved past is refused, and re-reading and
// retrying is the recovery.
func TestRepoWorkingVersionConflict(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "")

	if _, err := r.SaveWorking(ctx, ref, Working{Payload: []byte("a")}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := r.SaveWorking(ctx, ref, Working{Version: 0, Payload: []byte("b")}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("want ErrVersionConflict, got %v", err)
	}
	current, _, err := r.LoadWorking(ctx, ref)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, err := r.SaveWorking(ctx, ref, Working{Version: current.Version, Payload: []byte("b")}); err != nil {
		t.Fatalf("retry against the fresh version: %v", err)
	}
}

// TestRepoAppendTurnsAssignsSequence checks that the server numbers the turns,
// which is what lets two replicas append to one conversation without a version.
func TestRepoAppendTurnsAssignsSequence(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.AppendTurns(ctx, ref, []Turn{
		{Role: "user", Text: "first question"},
		{Role: "assistant", Text: "first answer"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "second question"}}); err != nil {
		t.Fatalf("second append: %v", err)
	}
	thread, turns, _, err := r.ReadThread(ctx, ref, Page{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(turns) != 3 {
		t.Fatalf("want 3 turns, got %d", len(turns))
	}
	for i, turn := range turns {
		if turn.Seq != int64(i+1) {
			t.Errorf("turn %d has sequence %d", i, turn.Seq)
		}
	}
	if thread.TurnCount != 3 {
		t.Errorf("thread should count its turns, got %d", thread.TurnCount)
	}
	if thread.UserID != "alice" {
		t.Errorf("thread should carry the user, got %q", thread.UserID)
	}
}

// TestRepoTurnAttrsRoundTrip checks the opaque per-turn JSON survives, since the
// runtime reads its own fields back out of it.
func TestRepoTurnAttrsRoundTrip(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "")

	if _, err := r.AppendTurns(ctx, ref, []Turn{{
		Role: "user", Text: "q", Attrs: map[string]any{"unanswered": true},
	}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	_, turns, _, err := r.ReadThread(ctx, ref, Page{})
	if err != nil || len(turns) != 1 {
		t.Fatalf("read: %d turns err=%v", len(turns), err)
	}
	if turns[0].Attrs["unanswered"] != true {
		t.Errorf("attrs lost a field: %v", turns[0].Attrs)
	}
}

// TestRepoListThreadsPagesWithoutRepeating is the reason the cursor is a keyset
// and not an offset: writing to a conversation reorders this listing, so an
// offset would skip rows constantly.
func TestRepoListThreadsPagesWithoutRepeating(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	base := testRef(t, r, "support", "", "alice")

	const total = 7
	for i := range total {
		ref := base
		ref.ThreadKey = string(rune('a' + i))
		if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "hi"}}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for range total + 2 {
		rows, next, err := r.ListThreads(ctx, base.IntegrationID, "support", "", Page{Limit: 3, Cursor: cursor})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		for _, row := range rows {
			if seen[row.ThreadKey] {
				t.Fatalf("cursor repeated %q", row.ThreadKey)
			}
			seen[row.ThreadKey] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != total {
		t.Errorf("paging should visit every conversation once, saw %d of %d", len(seen), total)
	}
}

// TestRepoListThreadsFiltersByUser checks the scoping the platform's thread list
// depends on.
func TestRepoListThreadsFiltersByUser(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	base := testRef(t, r, "support", "", "")

	for _, pair := range [][2]string{{"t1", "alice"}, {"t2", "bob"}} {
		ref := base
		ref.ThreadKey, ref.UserID = pair[0], pair[1]
		if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "hi"}}); err != nil {
			t.Fatalf("seed %s: %v", pair[0], err)
		}
	}
	rows, _, err := r.ListThreads(ctx, base.IntegrationID, "support", "alice", Page{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ThreadKey != "t1" {
		t.Errorf("want only alice's conversation, got %+v", rows)
	}
}

// TestRepoDeleteThreadCascades is the erasure guarantee: working memory and the
// transcript go with the conversation, so a clear cannot report success over a
// readable copy.
func TestRepoDeleteThreadCascades(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.SaveWorking(ctx, ref, Working{Payload: []byte("x")}); err != nil {
		t.Fatalf("seed working: %v", err)
	}
	if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "secret"}}); err != nil {
		t.Fatalf("seed turns: %v", err)
	}
	if err := r.DeleteThread(ctx, ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := r.LoadWorking(ctx, ref); ok {
		t.Error("working memory survived the delete")
	}
	if _, _, _, err := r.ReadThread(ctx, ref, Page{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("the conversation should be gone, got %v", err)
	}
}

// TestRepoUserMemories covers store, correct and forget, with the version check
// that makes a correction distinguishable from a duplicate.
func TestRepoUserMemories(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "", "alice")

	v, err := r.PutMemory(ctx, ref, "lang", "Prefers Go.", 0)
	if err != nil || v != 1 {
		t.Fatalf("put: v=%d err=%v", v, err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Rust.", 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("re-creating an existing memory should conflict, got %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Rust.", 1); err != nil {
		t.Fatalf("correct at the right version: %v", err)
	}
	got, err := r.Memories(ctx, ref)
	if err != nil || len(got) != 1 {
		t.Fatalf("want one memory, got %d (err=%v)", len(got), err)
	}
	if got[0].Value != "Prefers Rust." {
		t.Errorf("the correction should have stuck, got %q", got[0].Value)
	}
	if err := r.DeleteMemory(ctx, ref, "lang"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.DeleteMemory(ctx, ref, "lang"); err != nil {
		t.Errorf("deleting what is already gone should be a no-op, got %v", err)
	}
	if after, _ := r.Memories(ctx, ref); len(after) != 0 {
		t.Errorf("the memory should be gone, got %d", len(after))
	}
}

// TestRepoSearchTextFindsBothKinds checks the fallback search every deployment
// gets, with no embedding provider involved.
func TestRepoSearchTextFindsBothKinds(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.AppendTurns(ctx, ref, []Turn{
		{Role: "user", Text: "my refund never arrived"},
	}); err != nil {
		t.Fatalf("seed turns: %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "billing", "Had a refund dispute in March.", 0); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	hits, err := r.SearchText(ctx, ref.IntegrationID, Query{
		AgentID: "support", UserID: "alice", Text: "refund",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	kinds := map[string]bool{}
	for _, h := range hits {
		kinds[h.Kind] = true
	}
	if !kinds[HitTurn] || !kinds[HitUser] {
		t.Errorf("search should reach both conversations and memories, got %+v", hits)
	}
}

// TestRepoSearchIsScopedToTheAgent checks that one agent cannot read another's
// conversations, which is the boundary agentId exists to draw.
func TestRepoSearchIsScopedToTheAgent(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	mine := testRef(t, r, "support", "thread-1", "alice")
	theirs := mine
	theirs.AgentID = "other"

	if _, err := r.AppendTurns(ctx, theirs, []Turn{
		{Role: "user", Text: "a distinctive secret sentence"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hits, err := r.SearchText(ctx, mine.IntegrationID, Query{AgentID: "support", Text: "distinctive"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("one agent must not see another's conversations, got %+v", hits)
	}
}

// TestRepoDeleteForIntegrationSweepsEverything covers what integration deletion
// calls. It is explicit rather than a cascade, so it is also the thing that can
// be forgotten — and rows that outlive their integration are not orphans here,
// they are somebody's history with no owner.
func TestRepoDeleteForIntegrationSweepsEverything(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "alice")

	if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "hi"}}); err != nil {
		t.Fatalf("seed turns: %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Go.", 0); err != nil {
		t.Fatalf("seed memory: %v", err)
	}
	if err := r.DeleteForIntegration(ctx, ref.IntegrationID); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	rows, _, err := r.ListThreads(ctx, ref.IntegrationID, "support", "", Page{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("conversations survived the sweep: %d", len(rows))
	}
	memories, err := r.Memories(ctx, ref)
	if err != nil {
		t.Fatalf("memories: %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("user memories survived the sweep: %d", len(memories))
	}
}

// TestRepoListAgentsSummarizes checks the picker the admin viewer opens with.
func TestRepoListAgentsSummarizes(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	base := testRef(t, r, "support", "", "")

	for _, pair := range [][2]string{{"support", "t1"}, {"support", "t2"}, {"other", "t3"}} {
		ref := base
		ref.AgentID, ref.ThreadKey = pair[0], pair[1]
		if _, err := r.AppendTurns(ctx, ref, []Turn{{Role: "user", Text: "hi"}}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	agents, err := r.ListAgents(ctx, base.IntegrationID)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	counts := map[string]int{}
	for _, a := range agents {
		counts[a.AgentID] = a.ThreadCount
	}
	if counts["support"] != 2 || counts["other"] != 1 {
		t.Errorf("agent summary is wrong: %+v", counts)
	}
}

// TestRepoWorkingConflictsAfterErasure is the case a bare upsert gets wrong: a
// run holding version 5 for working memory that has since been erased must be
// told its write is stale, not silently given a fresh row at version 1.
func TestRepoWorkingConflictsAfterErasure(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "")

	if _, err := r.SaveWorking(ctx, ref, Working{Payload: []byte("a")}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := r.DeleteThread(ctx, ref); err != nil {
		t.Fatalf("erase: %v", err)
	}
	if _, err := r.SaveWorking(ctx, ref, Working{Version: 1, Payload: []byte("b")}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a write against an erased object should conflict, got %v", err)
	}
	// Creating afresh is still allowed, which is how the run recovers.
	if _, err := r.SaveWorking(ctx, ref, Working{Version: 0, Payload: []byte("b")}); err != nil {
		t.Fatalf("creating afresh after an erasure: %v", err)
	}
}

// TestRepoMemoryConflictsAfterForgetting is the user-memory twin of the erasure
// case: a correction stating a version that was forgotten in between is stale,
// not a fresh memory.
func TestRepoMemoryConflictsAfterForgetting(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "", "alice")

	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Go.", 0); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := r.DeleteMemory(ctx, ref, "lang"); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Rust.", 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("correcting a forgotten memory should conflict, got %v", err)
	}
	if _, err := r.PutMemory(ctx, ref, "lang", "Prefers Rust.", 0); err != nil {
		t.Fatalf("storing it afresh should work: %v", err)
	}
}

// TestSearchTextOrsItsTerms is the difference between a recall aid and a cliff.
//
// websearch_to_tsquery ANDs bare words, so a query naming two things no single
// turn contains matched nothing — which is most natural-language queries. Found
// on a live store: "rollout deployment" matched two obviously relevant turns and
// returned neither. ts_rank still ranks, so a turn containing both terms sorts
// above one containing either.
func TestSearchTextOrsItsTerms(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "")

	if _, err := r.AppendTurns(ctx, ref, []Turn{
		{Role: "user", Text: "how do I roll out a deployment?"},
		{Role: "assistant", Text: "use the rollout button on the integration"},
		{Role: "assistant", Text: "a rollout replaces the running deployment"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hits, err := r.SearchText(ctx, ref.IntegrationID, Query{
		AgentID: "support", Text: "rollout deployment",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) < 2 {
		t.Fatalf("a two-term query should reach turns matching either, got %d", len(hits))
	}
	// And ranking still prefers the turn that has both terms over one with either.
	if !strings.Contains(hits[0].Text, "rollout") || !strings.Contains(hits[0].Text, "deployment") {
		t.Errorf("the turn matching both terms should rank first, got %q", hits[0].Text)
	}
}

// TestSearchTextKeepsQuotedPhrases checks the rewrite only relaxes the joins
// between terms: a quoted phrase is still a phrase, not two OR'd words.
func TestSearchTextKeepsQuotedPhrases(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	ref := testRef(t, r, "support", "thread-1", "")

	if _, err := r.AppendTurns(ctx, ref, []Turn{
		{Role: "user", Text: "roll out the change"},
		{Role: "assistant", Text: "out of the roll there is nothing"},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	hits, err := r.SearchText(ctx, ref.IntegrationID, Query{
		AgentID: "support", Text: `"roll out"`,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || !strings.Contains(hits[0].Text, "roll out the change") {
		t.Errorf("a quoted phrase should match only the phrase, got %+v", hits)
	}
}
