package standalone

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// memoryOnlyStore is agent memory with no storage directory: everything in
// process, nothing on disk.
//
// It exists for the same reason newStore keeps an in-memory tier — a one-shot
// `octo invoke` should behave coherently while it runs and leave nothing behind.
// Returning a disabled store instead would be worse than it sounds: the engine
// takes an entirely different path when memory is unavailable, so an agent under
// invoke would not exercise the code it will run under `octo run`.
type memoryOnlyStore struct {
	mu      sync.Mutex
	threads map[string]*memThread
	userMem map[string]map[string]core.UserMemory
}

type memThread struct {
	meta    core.Thread
	working core.WorkingMemory
	hasWM   bool
	turns   []core.Turn
}

func newMemoryOnlyStore() *memoryOnlyStore {
	return &memoryOnlyStore{
		threads: map[string]*memThread{},
		userMem: map[string]map[string]core.UserMemory{},
	}
}

func memThreadKey(ref core.MemoryRef) string { return ref.AgentID + "\x00" + ref.ThreadKey }
func memUserKey(agentID, userID string) string {
	return agentID + "\x00" + userID
}

// thread returns the conversation for ref, creating it as any write does. The
// caller holds the lock.
func (s *memoryOnlyStore) thread(ref core.MemoryRef, at time.Time) *memThread {
	key := memThreadKey(ref)
	t, ok := s.threads[key]
	if !ok {
		t = &memThread{meta: core.Thread{
			AgentID: ref.AgentID, ThreadKey: ref.ThreadKey, UserID: ref.UserID, CreatedAt: at,
		}}
		s.threads[key] = t
	}
	if t.meta.UserID == "" {
		t.meta.UserID = ref.UserID
	}
	t.meta.LastActivityAt = at
	return t
}

func (s *memoryOnlyStore) loadWorking(ref core.MemoryRef) (core.WorkingMemory, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[memThreadKey(ref)]
	if !ok || !t.hasWM {
		return core.WorkingMemory{}, false, nil
	}
	wm := t.working
	wm.Payload = append([]byte(nil), t.working.Payload...)
	return wm, true, nil
}

func (s *memoryOnlyStore) saveWorking(ref core.MemoryRef, wm core.WorkingMemory) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	t := s.thread(ref, now)
	current := int64(0)
	if t.hasWM {
		current = t.working.Version
	}
	if wm.Version != current {
		return 0, core.ErrVersionConflict
	}
	wm.Version = current + 1
	wm.UpdatedAt = now
	wm.Payload = append([]byte(nil), wm.Payload...)
	t.working, t.hasWM = wm, true
	t.meta.Version++
	return wm.Version, nil
}

func (s *memoryOnlyStore) appendTurns(ref core.MemoryRef, turns []core.Turn) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	t := s.thread(ref, now)
	for _, turn := range turns {
		turn.Seq = int64(len(t.turns)) + 1
		turn.CreatedAt = now
		t.turns = append(t.turns, turn)
	}
	t.meta.TurnCount = len(t.turns)
	t.meta.Version++
	return t.meta.Version, nil
}

func (s *memoryOnlyStore) listThreads(
	agentID, userID string, page core.Page,
) ([]core.Thread, string, error) {
	s.mu.Lock()
	rows := s.allThreads(agentID, userID)
	s.mu.Unlock()
	return pageThreads(rows, page)
}

// allThreads returns every one of an agent's conversations in listing order. The
// caller holds the lock. See agentMemory.allThreads for why this is separate.
func (s *memoryOnlyStore) allThreads(agentID, userID string) []core.Thread {
	rows := make([]core.Thread, 0, len(s.threads))
	for _, t := range s.threads {
		if t.meta.AgentID != agentID || (userID != "" && t.meta.UserID != userID) {
			continue
		}
		rows = append(rows, t.meta)
	}
	sortThreads(rows)
	return rows
}

func (s *memoryOnlyStore) readThread(
	ref core.MemoryRef, page core.Page,
) (core.Thread, []core.Turn, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.threads[memThreadKey(ref)]
	if !ok {
		return core.Thread{}, nil, "", nil
	}
	turns, next := pageTurns(append([]core.Turn(nil), t.turns...), page)
	return t.meta, turns, next, nil
}

func (s *memoryOnlyStore) deleteThread(ref core.MemoryRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.threads, memThreadKey(ref))
	return nil
}

func (s *memoryOnlyStore) setTitle(ref core.MemoryRef, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.thread(ref, time.Now().UTC()).meta.Title = title
	return nil
}

func (s *memoryOnlyStore) memories(ref core.MemoryRef) ([]core.UserMemory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotMemories(ref.AgentID, ref.UserID), nil
}

// snapshotMemories copies one person's memories into a stable order. The caller
// holds the lock.
func (s *memoryOnlyStore) snapshotMemories(agentID, userID string) []core.UserMemory {
	items := s.userMem[memUserKey(agentID, userID)]
	out := make([]core.UserMemory, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *memoryOnlyStore) putMemory(
	ref core.MemoryRef, name, value string, expectedVersion int64,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	scope := memUserKey(ref.AgentID, ref.UserID)
	if s.userMem[scope] == nil {
		s.userMem[scope] = map[string]core.UserMemory{}
	}
	current := s.userMem[scope][name]
	if current.Version != expectedVersion {
		return 0, core.ErrVersionConflict
	}
	now := time.Now().UTC()
	if current.CreatedAt.IsZero() {
		current.CreatedAt = now
	}
	current.Name, current.Value = name, value
	current.Version++
	current.UpdatedAt = now
	s.userMem[scope][name] = current
	return current.Version, nil
}

func (s *memoryOnlyStore) deleteMemory(ref core.MemoryRef, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.userMem[memUserKey(ref.AgentID, ref.UserID)], name)
	return nil
}

// search is the same keyword scoring the on-disk store uses, over the same
// scopes, so behaviour does not change with the presence of a storage directory.
func (s *memoryOnlyStore) search(q core.MemoryQuery) ([]core.MemoryHit, error) {
	words := queryWords(q.Text)
	if len(words) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var hits []core.MemoryHit
	if q.Scope != core.MemoryScopeTurns && q.UserID != "" {
		for _, m := range s.snapshotMemories(q.AgentID, q.UserID) {
			if score := scoreText(m.Value+" "+m.Name, words); score > 0 {
				hits = append(hits, core.MemoryHit{
					Kind: core.MemoryHitUser, Name: m.Name, Text: m.Value, Score: score,
				})
			}
		}
	}
	if q.Scope != core.MemoryScopeUser {
		for _, t := range s.threads {
			if t.meta.AgentID != q.AgentID {
				continue
			}
			if q.UserID != "" && t.meta.UserID != q.UserID {
				continue
			}
			if q.ThreadKey != "" && t.meta.ThreadKey != q.ThreadKey {
				continue
			}
			for _, turn := range t.turns {
				if score := scoreText(turn.Text, words); score > 0 {
					hits = append(hits, core.MemoryHit{
						Kind: core.MemoryHitTurn, ThreadKey: t.meta.ThreadKey,
						Text: turn.Text, Seq: turn.Seq, Score: score,
					})
				}
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		return strings.Compare(hits[i].Text, hits[j].Text) < 0
	})
	limit := q.Limit
	if limit <= 0 || limit > len(hits) {
		limit = len(hits)
	}
	return hits[:limit], nil
}
