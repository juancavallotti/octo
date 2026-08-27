package engine

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// fakeMemory is an in-memory core.AgentMemory with the same versioning and
// append semantics the real stores promise, so the engine can be tested against
// the contract rather than against one implementation of it.
//
// It lives here rather than in a services package for the same reason fakeKV
// does: services imports core, and this engine is inside the core module, so
// reaching for a real store would be an import cycle.
type fakeMemory struct {
	mu       sync.Mutex
	threads  map[string]*fakeThread
	memories map[string]map[string]core.UserMemory
	// failWorking makes SaveWorking fail, so a test can assert that a store the
	// runtime cannot write to costs the record and not the conversation.
	failWorking bool
	// semantic is what Capabilities reports.
	semantic bool
}

type fakeThread struct {
	meta    core.Thread
	working core.WorkingMemory
	hasWM   bool
	turns   []core.Turn
}

func newFakeMemory() *fakeMemory {
	return &fakeMemory{
		threads:  map[string]*fakeThread{},
		memories: map[string]map[string]core.UserMemory{},
	}
}

func threadID(ref core.MemoryRef) string  { return ref.AgentID + "\x00" + ref.ThreadKey }
func userScope(ref core.MemoryRef) string { return ref.AgentID + "\x00" + ref.UserID }

func (m *fakeMemory) Enabled() bool { return true }

func (m *fakeMemory) Capabilities() core.MemoryCapabilities {
	return core.MemoryCapabilities{Semantic: m.semantic}
}

// thread returns the thread for ref, creating it as the real stores do on any
// write. The caller holds the lock.
func (m *fakeMemory) thread(ref core.MemoryRef) *fakeThread {
	key := threadID(ref)
	t, ok := m.threads[key]
	if !ok {
		t = &fakeThread{meta: core.Thread{
			AgentID:   ref.AgentID,
			ThreadKey: ref.ThreadKey,
			UserID:    ref.UserID,
			CreatedAt: time.Now(),
		}}
		m.threads[key] = t
	}
	return t
}

func (m *fakeMemory) LoadWorking(_ context.Context, ref core.MemoryRef) (core.WorkingMemory, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.threads[threadID(ref)]
	if !ok || !t.hasWM {
		return core.WorkingMemory{}, false, nil
	}
	wm := t.working
	wm.Payload = append([]byte(nil), t.working.Payload...)
	return wm, true, nil
}

func (m *fakeMemory) SaveWorking(_ context.Context, ref core.MemoryRef, wm core.WorkingMemory) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWorking {
		return 0, errFakeMemory
	}
	t := m.thread(ref)
	current := int64(0)
	if t.hasWM {
		current = t.working.Version
	}
	if wm.Version != current {
		return 0, core.ErrVersionConflict
	}
	wm.Version = current + 1
	wm.Payload = append([]byte(nil), wm.Payload...)
	wm.UpdatedAt = time.Now()
	t.working, t.hasWM = wm, true
	t.meta.LastActivityAt = wm.UpdatedAt
	return wm.Version, nil
}

func (m *fakeMemory) AppendTurns(_ context.Context, ref core.MemoryRef, turns []core.Turn) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.thread(ref)
	for _, turn := range turns {
		turn.Seq = int64(len(t.turns)) + 1
		turn.CreatedAt = time.Now()
		t.turns = append(t.turns, turn)
	}
	t.meta.TurnCount = len(t.turns)
	t.meta.Version++
	t.meta.LastActivityAt = time.Now()
	return t.meta.Version, nil
}

func (m *fakeMemory) ListThreads(
	_ context.Context, agentID, userID string, _ core.Page,
) ([]core.Thread, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []core.Thread
	for _, t := range m.threads {
		if t.meta.AgentID != agentID || (userID != "" && t.meta.UserID != userID) {
			continue
		}
		rows = append(rows, t.meta)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ThreadKey < rows[j].ThreadKey })
	return rows, "", nil
}

func (m *fakeMemory) ReadThread(
	_ context.Context, ref core.MemoryRef, _ core.Page,
) (core.Thread, []core.Turn, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.threads[threadID(ref)]
	if !ok {
		return core.Thread{}, nil, "", nil
	}
	return t.meta, append([]core.Turn(nil), t.turns...), "", nil
}

func (m *fakeMemory) DeleteThread(_ context.Context, ref core.MemoryRef) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.threads, threadID(ref))
	return nil
}

func (m *fakeMemory) SetTitle(_ context.Context, ref core.MemoryRef, title string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.thread(ref).meta.Title = title
	return nil
}

func (m *fakeMemory) Memories(_ context.Context, ref core.MemoryRef) ([]core.UserMemory, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.UserMemory
	for _, mem := range m.memories[userScope(ref)] {
		out = append(out, mem)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *fakeMemory) PutMemory(
	_ context.Context, ref core.MemoryRef, name, value string, expectedVersion int64,
) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	scope := userScope(ref)
	if m.memories[scope] == nil {
		m.memories[scope] = map[string]core.UserMemory{}
	}
	current := m.memories[scope][name]
	if current.Version != expectedVersion {
		return 0, core.ErrVersionConflict
	}
	current.Name, current.Value = name, value
	current.Version++
	current.UpdatedAt = time.Now()
	m.memories[scope][name] = current
	return current.Version, nil
}

func (m *fakeMemory) DeleteMemory(_ context.Context, ref core.MemoryRef, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.memories[userScope(ref)], name)
	return nil
}

// Search is the keyword fallback every store owes, so the engine's tool path can
// be tested without an embedding provider.
func (m *fakeMemory) Search(_ context.Context, q core.MemoryQuery) ([]core.MemoryHit, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	needle := strings.ToLower(q.Text)
	var hits []core.MemoryHit
	if q.Scope != core.MemoryScopeTurns {
		for _, mem := range m.memories[q.AgentID+"\x00"+q.UserID] {
			if strings.Contains(strings.ToLower(mem.Value), needle) {
				hits = append(hits, core.MemoryHit{
					Kind: core.MemoryHitUser, Name: mem.Name, Text: mem.Value, Score: 1,
				})
			}
		}
	}
	if q.Scope != core.MemoryScopeUser {
		for _, t := range m.threads {
			if t.meta.AgentID != q.AgentID {
				continue
			}
			for _, turn := range t.turns {
				if strings.Contains(strings.ToLower(turn.Text), needle) {
					hits = append(hits, core.MemoryHit{
						Kind: core.MemoryHitTurn, ThreadKey: t.meta.ThreadKey,
						Text: turn.Text, Seq: turn.Seq, Score: 0.5,
					})
				}
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits, nil
}

// turnsFor returns a thread's recorded turns, for assertions.
func (m *fakeMemory) turnsFor(agentID, thread string) []core.Turn {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.threads[agentID+"\x00"+thread]
	if !ok {
		return nil
	}
	return append([]core.Turn(nil), t.turns...)
}

// titleFor returns a thread's stored title, for assertions.
func (m *fakeMemory) titleOf(agentID, thread string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.threads[agentID+"\x00"+thread]; ok {
		return t.meta.Title
	}
	return ""
}

// threadCount is how many conversations the store holds, for assertions about
// the paths that must not create one.
func (m *fakeMemory) threadCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.threads)
}

// errFakeMemory is what a store told to fail returns.
var errFakeMemory = errors.New("fake memory: write refused")
