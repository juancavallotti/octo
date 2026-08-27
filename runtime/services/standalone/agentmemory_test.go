package standalone

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// bothStores runs a test against the on-disk store and the in-memory one, so the
// two cannot drift: the difference between them is meant to be where the bytes
// live and nothing else.
func bothStores(t *testing.T, run func(t *testing.T, m *agentMemory)) {
	t.Helper()
	t.Run("on disk", func(t *testing.T) { run(t, newAgentMemory(t.TempDir())) })
	t.Run("in memory", func(t *testing.T) { run(t, newAgentMemory("")) })
}

func ref(agent, thread, user string) core.MemoryRef {
	return core.MemoryRef{AgentID: agent, ThreadKey: thread, UserID: user}
}

// TestAgentMemoryWorkingRoundTrip is the basic promise: what a run saved is what
// the next run loads.
func TestAgentMemoryWorkingRoundTrip(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "alice")

		if _, ok, err := m.LoadWorking(ctx, r); err != nil || ok {
			t.Fatalf("a new conversation should have no working memory (ok=%v err=%v)", ok, err)
		}
		v, err := m.SaveWorking(ctx, r, core.WorkingMemory{Payload: []byte(`{"m":1}`), Tokens: 12})
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if v != 1 {
			t.Errorf("a first write is version 1, got %d", v)
		}
		got, ok, err := m.LoadWorking(ctx, r)
		if err != nil || !ok {
			t.Fatalf("load: ok=%v err=%v", ok, err)
		}
		if string(got.Payload) != `{"m":1}` || got.Tokens != 12 || got.Version != 1 {
			t.Errorf("round trip lost something: %+v", got)
		}
	})
}

// TestAgentMemoryWorkingVersionConflict checks the optimistic-concurrency
// contract: a write against a version somebody else has moved on from is refused
// rather than silently winning.
func TestAgentMemoryWorkingVersionConflict(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "")

		if _, err := m.SaveWorking(ctx, r, core.WorkingMemory{Payload: []byte("a")}); err != nil {
			t.Fatalf("first save: %v", err)
		}
		// A second writer that still believes the object is new.
		_, err := m.SaveWorking(ctx, r, core.WorkingMemory{Version: 0, Payload: []byte("b")})
		if !errors.Is(err, core.ErrVersionConflict) {
			t.Fatalf("want ErrVersionConflict, got %v", err)
		}
		// Re-reading and retrying is the documented recovery, and it must work.
		current, _, err := m.LoadWorking(ctx, r)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if _, err := m.SaveWorking(ctx, r,
			core.WorkingMemory{Version: current.Version, Payload: []byte("b")}); err != nil {
			t.Fatalf("retry against the fresh version: %v", err)
		}
	})
}

// TestAgentMemoryTurnsAppend checks that turns accumulate in order with
// store-assigned sequence numbers, across separate calls.
func TestAgentMemoryTurnsAppend(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "alice")

		if _, err := m.AppendTurns(ctx, r, []core.Turn{
			{Role: core.LLMRoleUser, Text: "first question"},
			{Role: core.LLMRoleAssistant, Text: "first answer"},
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
		if _, err := m.AppendTurns(ctx, r, []core.Turn{
			{Role: core.LLMRoleUser, Text: "second question"},
		}); err != nil {
			t.Fatalf("second append: %v", err)
		}

		thread, turns, _, err := m.ReadThread(ctx, r, core.Page{})
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
			t.Errorf("thread should carry the user it was opened for, got %q", thread.UserID)
		}
	})
}

// TestAgentMemoryListThreadsByAgent checks the listing the platform needs, and
// that one agent cannot see another's conversations.
func TestAgentMemoryListThreadsByAgent(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		for _, r := range []core.MemoryRef{
			ref("support", "t1", "alice"),
			ref("support", "t2", "bob"),
			ref("other", "t3", "alice"),
		} {
			if _, err := m.AppendTurns(ctx, r, []core.Turn{{Role: core.LLMRoleUser, Text: "hi"}}); err != nil {
				t.Fatalf("seed %s: %v", r.ThreadKey, err)
			}
		}

		rows, _, err := m.ListThreads(ctx, "support", "", core.Page{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("want support's two conversations, got %d", len(rows))
		}
		byUser, _, err := m.ListThreads(ctx, "support", "alice", core.Page{})
		if err != nil {
			t.Fatalf("list by user: %v", err)
		}
		if len(byUser) != 1 || byUser[0].ThreadKey != "t1" {
			t.Errorf("filtering by user should leave one conversation, got %+v", byUser)
		}
	})
}

// TestAgentMemoryListThreadsPages checks that a cursor walks the whole listing
// exactly once — no repeats, nothing skipped.
func TestAgentMemoryListThreadsPages(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		const total = 7
		for i := range total {
			r := ref("support", string(rune('a'+i)), "")
			if _, err := m.AppendTurns(ctx, r, []core.Turn{{Role: core.LLMRoleUser, Text: "hi"}}); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		seen := map[string]bool{}
		cursor := ""
		for range total + 2 {
			rows, next, err := m.ListThreads(ctx, "support", "", core.Page{Limit: 3, Cursor: cursor})
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
	})
}

// TestAgentMemoryUserMemories covers the curated half: store, correct, list,
// forget.
func TestAgentMemoryUserMemories(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "", "alice")

		v, err := m.PutMemory(ctx, r, "lang", "Prefers Go.", 0)
		if err != nil || v != 1 {
			t.Fatalf("put: v=%d err=%v", v, err)
		}
		if _, err := m.PutMemory(ctx, r, "lang", "Prefers Rust.", 0); !errors.Is(err, core.ErrVersionConflict) {
			t.Fatalf("re-creating an existing memory should conflict, got %v", err)
		}
		if _, err := m.PutMemory(ctx, r, "lang", "Prefers Rust.", 1); err != nil {
			t.Fatalf("correct at the right version: %v", err)
		}

		got, err := m.Memories(ctx, r)
		if err != nil || len(got) != 1 {
			t.Fatalf("want one memory, got %d (err=%v)", len(got), err)
		}
		if got[0].Value != "Prefers Rust." {
			t.Errorf("the correction should have stuck, got %q", got[0].Value)
		}

		if err := m.DeleteMemory(ctx, r, "lang"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := m.DeleteMemory(ctx, r, "lang"); err != nil {
			t.Errorf("deleting what is already gone should be a no-op, got %v", err)
		}
		if after, _ := m.Memories(ctx, r); len(after) != 0 {
			t.Errorf("the memory should be gone, got %d", len(after))
		}
	})
}

// TestAgentMemoryDeleteThreadRemovesEverything is the erasure guarantee. A delete
// that left the transcript behind would report success over a readable copy of
// the conversation somebody asked to be rid of.
func TestAgentMemoryDeleteThreadRemovesEverything(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "alice")
		if _, err := m.SaveWorking(ctx, r, core.WorkingMemory{Payload: []byte("x")}); err != nil {
			t.Fatalf("seed working: %v", err)
		}
		if _, err := m.AppendTurns(ctx, r, []core.Turn{{Role: core.LLMRoleUser, Text: "secret"}}); err != nil {
			t.Fatalf("seed turns: %v", err)
		}

		if err := m.DeleteThread(ctx, r); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, ok, _ := m.LoadWorking(ctx, r); ok {
			t.Error("working memory survived the delete")
		}
		if _, turns, _, _ := m.ReadThread(ctx, r, core.Page{}); len(turns) != 0 {
			t.Error("the transcript survived the delete")
		}
		if rows, _, _ := m.ListThreads(ctx, "support", "", core.Page{}); len(rows) != 0 {
			t.Error("the conversation is still listed after being deleted")
		}
		if hits, _ := m.Search(ctx, core.MemoryQuery{AgentID: "support", Text: "secret"}); len(hits) != 0 {
			t.Error("search still finds a deleted conversation")
		}
	})
}

// TestAgentMemorySearchFindsBothKinds checks the fallback search reaches
// conversations and curated memories, and reports which is which.
func TestAgentMemorySearchFindsBothKinds(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "alice")
		if _, err := m.AppendTurns(ctx, r, []core.Turn{
			{Role: core.LLMRoleUser, Text: "my refund never arrived"},
		}); err != nil {
			t.Fatalf("seed turns: %v", err)
		}
		if _, err := m.PutMemory(ctx, r, "billing", "Had a refund dispute in March.", 0); err != nil {
			t.Fatalf("seed memory: %v", err)
		}

		hits, err := m.Search(ctx, core.MemoryQuery{AgentID: "support", UserID: "alice", Text: "refund"})
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		kinds := map[string]bool{}
		for _, h := range hits {
			kinds[h.Kind] = true
		}
		if !kinds[core.MemoryHitTurn] || !kinds[core.MemoryHitUser] {
			t.Errorf("search should reach both conversations and memories, got %+v", hits)
		}
	})
}

// TestAgentMemorySearchIsKeywordOnly states the capability plainly: this module
// has no embedding provider and does not pretend to.
func TestAgentMemorySearchIsKeywordOnly(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		if m.Capabilities().Semantic {
			t.Error("the standalone module has no embeddings and must not claim semantic search")
		}
		if !m.Enabled() {
			t.Error("the standalone store is always enabled")
		}
	})
}

// TestAgentMemoryFileLayout pins the on-disk shape, because it is the addressing
// and an operator reads it directly.
func TestAgentMemoryFileLayout(t *testing.T) {
	dir := t.TempDir()
	m := newAgentMemory(dir)
	ctx := context.Background()
	r := ref("dr-octo", "thread/1", "alice")

	if _, err := m.SaveWorking(ctx, r, core.WorkingMemory{Payload: []byte("x")}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := m.AppendTurns(ctx, r, []core.Turn{{Role: core.LLMRoleUser, Text: "hi"}}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := m.PutMemory(ctx, r, "lang", "Go.", 0); err != nil {
		t.Fatalf("put memory: %v", err)
	}

	base := filepath.Join(dir, memoryRootDir, encodeName("dr-octo"))
	for _, want := range []string{
		filepath.Join(base, threadsDir, encodeName("thread/1"), threadFile),
		filepath.Join(base, threadsDir, encodeName("thread/1"), workingFile),
		filepath.Join(base, threadsDir, encodeName("thread/1"), turnsFile),
		filepath.Join(base, usersDir, encodeName("alice")+userMemoryFile),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist: %v", want, err)
		}
	}
}

// TestAgentMemoryTolerantOfATruncatedTurn covers what an interrupted append
// leaves behind. One damaged line must not make a conversation unreadable — the
// same bargain the snapshot loader makes with a corrupt namespace file.
func TestAgentMemoryTolerantOfATruncatedTurn(t *testing.T) {
	dir := t.TempDir()
	m := newAgentMemory(dir)
	ctx := context.Background()
	r := ref("support", "thread-1", "")

	if _, err := m.AppendTurns(ctx, r, []core.Turn{
		{Role: core.LLMRoleUser, Text: "a complete turn"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	path := filepath.Join(m.threadDir(r), turnsFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, filePerm)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"seq":2,"role":"user","text":"cut off mid-`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, turns, _, err := m.ReadThread(ctx, r, core.Page{})
	if err != nil {
		t.Fatalf("a truncated trailing line must not fail the read: %v", err)
	}
	if len(turns) != 1 || turns[0].Text != "a complete turn" {
		t.Errorf("the complete turns should still be readable, got %+v", turns)
	}
}

// TestAgentMemoryTurnAttrsSurvive checks that the opaque per-turn JSON comes back
// byte-identical, since the engine reads its own fields out of it.
func TestAgentMemoryTurnAttrsSurvive(t *testing.T) {
	bothStores(t, func(t *testing.T, m *agentMemory) {
		ctx := context.Background()
		r := ref("support", "thread-1", "")
		attrs, err := json.Marshal(map[string]any{"unanswered": true, "iterations": 4})
		if err != nil {
			t.Fatalf("encode attrs: %v", err)
		}
		if _, err := m.AppendTurns(ctx, r, []core.Turn{
			{Role: core.LLMRoleUser, Text: "q", Attrs: attrs},
		}); err != nil {
			t.Fatalf("append: %v", err)
		}
		_, turns, _, err := m.ReadThread(ctx, r, core.Page{})
		if err != nil || len(turns) != 1 {
			t.Fatalf("read: %d turns, err=%v", len(turns), err)
		}
		var got map[string]any
		if err := json.Unmarshal(turns[0].Attrs, &got); err != nil {
			t.Fatalf("stored attrs should decode: %v", err)
		}
		if got["unanswered"] != true {
			t.Errorf("attrs lost a field: %v", got)
		}
	})
}

// TestAgentMemorySurvivesRestart checks that a new store over the same directory
// sees what the last one wrote — the whole point of the on-disk tier.
func TestAgentMemorySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	r := ref("support", "thread-1", "alice")

	first := newAgentMemory(dir)
	if _, err := first.AppendTurns(ctx, r, []core.Turn{
		{Role: core.LLMRoleUser, Text: "before the restart"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := first.SetTitle(ctx, r, "a named conversation"); err != nil {
		t.Fatalf("title: %v", err)
	}

	second := newAgentMemory(dir)
	rows, _, err := second.ListThreads(ctx, "support", "", core.Page{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("want the conversation back, got %d (err=%v)", len(rows), err)
	}
	if rows[0].Title != "a named conversation" {
		t.Errorf("title did not survive, got %q", rows[0].Title)
	}
	_, turns, _, err := second.ReadThread(ctx, r, core.Page{})
	if err != nil || len(turns) != 1 || !strings.Contains(turns[0].Text, "before the restart") {
		t.Errorf("the transcript did not survive: %+v (err=%v)", turns, err)
	}
}

// TestAgentMemoryConcurrentWorkingWrites checks the per-thread lock: concurrent
// savers must either succeed or see a conflict, never corrupt the file.
func TestAgentMemoryConcurrentWorkingWrites(t *testing.T) {
	m := newAgentMemory(t.TempDir())
	ctx := context.Background()
	r := ref("support", "thread-1", "")

	const writers = 8
	done := make(chan struct{})
	for i := range writers {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			current, _, err := m.LoadWorking(ctx, r)
			if err != nil {
				return
			}
			// A conflict is a legitimate outcome; a corrupt file is not.
			_, _ = m.SaveWorking(ctx, r, core.WorkingMemory{
				Version: current.Version, Payload: []byte{byte('0' + i)},
			})
		}(i)
	}
	for range writers {
		<-done
	}
	got, ok, err := m.LoadWorking(ctx, r)
	if err != nil || !ok {
		t.Fatalf("the object should still be readable: ok=%v err=%v", ok, err)
	}
	if len(got.Payload) != 1 {
		t.Errorf("the stored payload is not one writer's: %q", got.Payload)
	}
}
