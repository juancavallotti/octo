package standalone

import (
	"context"
	"sync"

	"github.com/juancavallotti/octo/core"
)

// store is an in-memory, versioned KV store with optimistic concurrency. Entries
// are held in a map of namespace -> key -> entry, so each namespace is a separate
// keyspace. A single mutex serializes every operation, so the compare-and-bump on a
// write is atomic and concurrent writers to the same key cannot lose an update.
// SetSecret stores the value like any other: nothing leaves process memory, so
// encryption would add no protection.
type store struct {
	mu sync.Mutex
	ns map[string]map[string]core.Entry
}

func newStore() *store {
	return &store{ns: make(map[string]map[string]core.Entry)}
}

// Get returns a copy of the stored entry so callers cannot mutate the stored bytes.
func (s *store) Get(_ context.Context, namespace, key string) (core.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.ns[namespace][key] // indexing an absent namespace yields the zero entry
	if !ok {
		return core.Entry{}, false, nil
	}
	return core.Entry{Value: cloneBytes(e.Value), Version: e.Version}, true, nil
}

func (s *store) Set(_ context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	return s.write(namespace, key, value, expectedVersion)
}

func (s *store) SetSecret(_ context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	return s.write(namespace, key, value, expectedVersion)
}

// write applies the optimistic-concurrency check and stores value, all under the
// lock. expectedVersion must equal the current version (0 when the key is absent),
// so a create needs version 0 and fails once the key exists.
func (s *store) write(namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.ns[namespace]
	current := keys[key].Version // zero value when the namespace or key is absent
	if expectedVersion != current {
		return 0, core.ErrVersionConflict
	}
	if keys == nil {
		keys = make(map[string]core.Entry)
		s.ns[namespace] = keys
	}
	next := current + 1
	keys[key] = core.Entry{Value: cloneBytes(value), Version: next}
	return next, nil
}

// Delete removes key. expectedVersion 0 deletes unconditionally; a positive value
// must match the stored version. Deleting an absent key is a no-op.
func (s *store) Delete(_ context.Context, namespace, key string, expectedVersion int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.ns[namespace]
	e, ok := keys[key]
	if !ok {
		return nil
	}
	if expectedVersion != 0 && expectedVersion != e.Version {
		return core.ErrVersionConflict
	}
	delete(keys, key)
	return nil
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}
