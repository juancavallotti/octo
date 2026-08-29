package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

func newMemoryFixture(t *testing.T) (*Services, *fake, *memoryBackend) {
	t.Helper()
	f := newFake(t, fullDiscovery())
	b := newMemoryBackend()
	b.install(f)
	return newTestServices(t, f, nil), f, b
}

func testRef() core.MemoryRef {
	return core.MemoryRef{AgentID: "support", ThreadKey: "conv-1", UserID: "person-7"}
}

// Enabled answers without a round trip: it is on the hot path of every agent run
// and the engine branches on it before doing anything else.
func TestMemoryEnabledDoesNotCallOut(t *testing.T) {
	svc, f, _ := newMemoryFixture(t)
	before := len(f.paths())
	if !svc.AgentMemory().Enabled() {
		t.Fatal("Enabled = false against a platform that declared agent memory")
	}
	if after := len(f.paths()); after != before {
		t.Fatal("Enabled made an HTTP call")
	}
}

// Working memory round-trips with its metadata, and its version rules match KV's:
// zero creates, a stale version conflicts.
func TestWorkingMemoryRoundTrip(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	if _, ok, err := mem.LoadWorking(ctx, ref); ok || err != nil {
		t.Fatalf("LoadWorking on a new thread = (ok %v, err %v), want resume-from-nothing", ok, err)
	}

	v, err := mem.SaveWorking(ctx, ref, core.WorkingMemory{
		Payload: []byte("transcript"), Iteration: 3, Tokens: 1200,
	})
	if err != nil || v == 0 {
		t.Fatalf("SaveWorking = (%d, %v)", v, err)
	}

	got, ok, err := mem.LoadWorking(ctx, ref)
	if err != nil || !ok {
		t.Fatalf("LoadWorking = (ok %v, err %v)", ok, err)
	}
	if string(got.Payload) != "transcript" || got.Iteration != 3 || got.Tokens != 1200 || got.Version != v {
		t.Fatalf("working memory = %+v, want the payload and metadata just written", got)
	}

	if _, err := mem.SaveWorking(ctx, ref, core.WorkingMemory{Version: v + 9}); !errors.Is(
		err, core.ErrVersionConflict,
	) {
		t.Fatalf("SaveWorking at a stale version = %v, want ErrVersionConflict", err)
	}
}

// Turns are append-only and the store assigns Seq, so two writers interleave
// rather than collide.
func TestAppendTurnsAndReadBack(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	if _, err := mem.AppendTurns(ctx, ref, []core.Turn{
		{Role: core.LLMRoleUser, Text: "where is my order"},
		{Role: core.LLMRoleAssistant, Text: "let me look"},
	}); err != nil {
		t.Fatalf("AppendTurns: %v", err)
	}

	thread, turns, _, err := mem.ReadThread(ctx, ref, core.Page{})
	if err != nil {
		t.Fatalf("ReadThread: %v", err)
	}
	if len(turns) != 2 || turns[0].Text != "where is my order" {
		t.Fatalf("turns = %+v", turns)
	}
	if turns[0].Seq != 1 || turns[1].Seq != 2 {
		t.Fatalf("seqs = (%d, %d), want the store to have assigned them", turns[0].Seq, turns[1].Seq)
	}
	if thread.TurnCount != 2 {
		t.Fatalf("turnCount = %d, want 2", thread.TurnCount)
	}
}

// A long run is chunked to the platform's declared limit rather than arriving as
// one enormous request.
func TestAppendTurnsChunks(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.AgentMemory.MaxTurnsPerAppend = 2
	f := newFake(t, doc)
	newMemoryBackend().install(f)
	svc := newTestServices(t, f, nil)

	turns := make([]core.Turn, 5)
	for i := range turns {
		turns[i] = core.Turn{Role: core.LLMRoleUser, Text: "t"}
	}
	if _, err := svc.AgentMemory().AppendTurns(t.Context(), testRef(), turns); err != nil {
		t.Fatalf("AppendTurns: %v", err)
	}
	if got := f.count(http.MethodPost, "/turns"); got != 3 {
		t.Fatalf("append calls = %d, want 3 chunks of at most 2", got)
	}
}

// The user rides as a query parameter and must actually be sent: a platform
// records who a conversation is with on the first write that names one, so
// omitting it stores a complete history attributed to nobody.
func TestUserIDIsSentAsAQueryParameter(t *testing.T) {
	svc, f, b := newMemoryFixture(t)
	ref := testRef()

	if _, err := svc.AgentMemory().AppendTurns(t.Context(), ref, []core.Turn{
		{Role: core.LLMRoleUser, Text: "hello"},
	}); err != nil {
		t.Fatal(err)
	}

	req := f.last("/turns")
	if !strings.Contains(req.query, "userId=person-7") {
		t.Fatalf("query = %q, want userId as a query parameter", req.query)
	}
	// And never as a path segment: that would give one conversation two addresses.
	if strings.Contains(req.path, "person-7") {
		t.Fatalf("path = %s, want the user out of the path", req.path)
	}
	if got := b.userOf("support", "conv-1"); got != "person-7" {
		t.Fatalf("the conversation is attributed to %q, want person-7", got)
	}
}

// Listing and reading work here, unlike in the k8s module, because this
// platform is the operator's own system and knows its own tenancy.
func TestListThreads(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx := t.Context()
	mem := svc.AgentMemory()

	for _, key := range []string{"conv-1", "conv-2"} {
		ref := core.MemoryRef{AgentID: "support", ThreadKey: key, UserID: "person-7"}
		if _, err := mem.AppendTurns(ctx, ref, []core.Turn{{Role: core.LLMRoleUser, Text: "x"}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := mem.ListThreads(ctx, "support", "person-7", core.Page{})
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("threads = %d, want 2", len(rows))
	}
}

// A platform that would rather not expose listing says so in discovery, and the
// refusal names the flags to set.
func TestListingRefusedWhenNotOffered(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.AgentMemory.ListThreads = false
	doc.Features.AgentMemory.ReadThread = false
	f := newFake(t, doc)
	newMemoryBackend().install(f)
	svc := newTestServices(t, f, nil)

	if _, _, err := svc.AgentMemory().ListThreads(t.Context(), "support", "", core.Page{}); err == nil {
		t.Fatal("ListThreads err = nil, want a refusal")
	} else if !strings.Contains(err.Error(), "listThreads") {
		t.Fatalf("err = %v, want it to name the discovery flag", err)
	}
	if _, _, _, err := svc.AgentMemory().ReadThread(t.Context(), testRef(), core.Page{}); err == nil {
		t.Fatal("ReadThread err = nil, want a refusal")
	}
}

// Erasure is the one operation that must not report false success — and with
// nothing stored there is no copy left to be wrong about, so a missing thread is
// not an error.
func TestDeleteThreadIsIdempotent(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	if _, err := mem.AppendTurns(ctx, ref, []core.Turn{{Role: core.LLMRoleUser, Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := mem.DeleteThread(ctx, ref); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if err := mem.DeleteThread(ctx, ref); err != nil {
		t.Fatalf("second DeleteThread = %v, want success", err)
	}
}

// Curated memories are versioned like KV, and deleting a missing one succeeds.
func TestUserMemories(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	v, err := mem.PutMemory(ctx, ref, "prefers", "email over phone", 0)
	if err != nil || v == 0 {
		t.Fatalf("PutMemory = (%d, %v)", v, err)
	}
	if _, err := mem.PutMemory(ctx, ref, "prefers", "again", 0); !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("PutMemory create over existing = %v, want ErrVersionConflict", err)
	}
	rows, err := mem.Memories(ctx, ref)
	if err != nil || len(rows) != 1 || rows[0].Value != "email over phone" {
		t.Fatalf("Memories = (%+v, %v)", rows, err)
	}
	if err := mem.DeleteMemory(ctx, ref, "prefers"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if err := mem.DeleteMemory(ctx, ref, "prefers"); err != nil {
		t.Fatalf("second DeleteMemory = %v, want success", err)
	}
}

// SetTitle is separate from the write path because naming a conversation is a
// judgement the runtime does not make.
func TestSetTitle(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	if err := mem.SetTitle(ctx, ref, "Order A-1"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	thread, _, _, err := mem.ReadThread(ctx, ref, core.Page{})
	if err != nil {
		t.Fatal(err)
	}
	if thread.Title != "Order A-1" {
		t.Fatalf("title = %q", thread.Title)
	}
}

func TestSearch(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()
	mem := svc.AgentMemory()

	if _, err := mem.AppendTurns(ctx, ref, []core.Turn{
		{Role: core.LLMRoleUser, Text: "the shipment is late"},
		{Role: core.LLMRoleUser, Text: "unrelated"},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := mem.Search(ctx, core.MemoryQuery{AgentID: "support", Text: "shipment"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Kind != core.MemoryHitTurn {
		t.Fatalf("hits = %+v", hits)
	}
}

// A platform that stores memory but does not search declares so, and Search is
// empty rather than an error — an agent that cannot look back still works.
func TestSearchWithoutTheCapability(t *testing.T) {
	doc := fullDiscovery()
	doc.Features.AgentMemory.Search = false
	f := newFake(t, doc)
	newMemoryBackend().install(f)
	svc := newTestServices(t, f, nil)

	hits, err := svc.AgentMemory().Search(t.Context(), core.MemoryQuery{AgentID: "support", Text: "x"})
	if err != nil || hits != nil {
		t.Fatalf("Search = (%+v, %v), want empty and no error", hits, err)
	}
}

// A 501 on a write latches the store off, so a runtime rolled ahead of its
// platform degrades to keeping no memory rather than failing every agent run.
func TestMemoryLatchesOffOnNotImplemented(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("PUT "+pathMemoryWorking, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not implemented", http.StatusNotImplemented)
	})
	svc := newTestServices(t, f, nil)
	mem := svc.AgentMemory()

	if _, err := mem.SaveWorking(t.Context(), testRef(), core.WorkingMemory{}); !errors.Is(
		err, core.ErrMemoryDisabled,
	) {
		t.Fatalf("SaveWorking = %v, want ErrMemoryDisabled", err)
	}
	if mem.Enabled() {
		t.Fatal("Enabled is still true after the platform answered 501")
	}
}

// Working memory carries the same requirement as KV, for the same reason: a save
// that comes back without a version leaves the caller holding 0, and the next
// save then means "create" and conflicts forever — so the conversation could be
// started and never checkpointed again.
func TestWorkingMemoryRejectsASuccessWithoutAUsableVersion(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("PUT "+pathMemoryWorking, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // no X-Object-Version
	})
	svc := newTestServices(t, f, nil)

	_, err := svc.AgentMemory().SaveWorking(t.Context(), testRef(), core.WorkingMemory{})
	if err == nil {
		t.Fatal("SaveWorking accepted a success with no version")
	}
	if !strings.Contains(err.Error(), headerVersion) {
		t.Fatalf("err = %v, want it to name the header", err)
	}
}

// The counters beside it are not required: an agent re-derives them, and a store
// that drops them costs a metric rather than a conversation.
func TestWorkingMemoryToleratesMissingCounters(t *testing.T) {
	svc, _, _ := newMemoryFixture(t)
	ctx, ref := t.Context(), testRef()

	if _, err := svc.AgentMemory().SaveWorking(ctx, ref, core.WorkingMemory{
		Payload: []byte("transcript"),
	}); err != nil {
		t.Fatalf("SaveWorking: %v", err)
	}
	got, ok, err := svc.AgentMemory().LoadWorking(ctx, ref)
	if err != nil || !ok {
		t.Fatalf("LoadWorking = (ok %v, err %v)", ok, err)
	}
	if got.Iteration != 0 || got.Tokens != 0 {
		t.Fatalf("counters = (%d, %d), want the zero values to round-trip", got.Iteration, got.Tokens)
	}
}
