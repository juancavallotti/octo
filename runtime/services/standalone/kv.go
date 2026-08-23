package standalone

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// flushDelay coalesces writes to the same namespace. A flow that writes a hundred
// objects in a burst should cost one file write, not a hundred; half a second is
// long enough to absorb a burst and short enough that a kill -9 loses almost
// nothing. A graceful stop does not wait for it — close flushes synchronously.
const flushDelay = 500 * time.Millisecond

// store is an in-memory, versioned store with optimistic concurrency, used for both
// the KV and secret stores in the standalone module (separate keyspaces in one
// instance). Entries are held in a map of namespace -> key -> entry, so each
// namespace is a separate keyspace. A single mutex serializes every operation, so
// the compare-and-bump on a write is atomic and concurrent writers to the same key
// cannot lose an update.
//
// A namespace that persists is also serialized to a directory (see snapshot), so a
// standalone runtime's objects survive a restart the way a deployed one's do. Two
// kinds of namespace stay in memory:
//
//   - Volatile namespaces, because that is what volatile means. The k8s module puts
//     them in Redis; here there is nothing to put them in but the process.
//   - Secret namespaces, because this module has no encryption key. The k8s module's
//     backend encrypts secrets at rest; writing an OAuth refresh token to the working
//     directory in cleartext to buy restart-survival is the wrong trade, so secrets
//     keep the memory-only lifetime they have always had here.
type store struct {
	mu    sync.Mutex
	ns    map[string]map[string]core.Entry
	dirty map[string]struct{}

	snap    *snapshot     // nil keeps everything in memory
	wake    chan struct{} // buffered(1): a nudge that there is dirty state
	done    chan struct{}
	wg      sync.WaitGroup
	closing sync.Once
}

// newStore returns a store backed by the directory dir, loading whatever a previous
// run left there. An empty dir keeps everything in memory, which is what tests and
// one-shot invocations want.
func newStore(dir string) *store {
	s := &store{
		ns:    make(map[string]map[string]core.Entry),
		dirty: make(map[string]struct{}),
		snap:  newSnapshot(dir),
		wake:  make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	if s.snap == nil {
		return s
	}
	s.ns = s.snap.load()
	s.wg.Add(1)
	go s.flusher()
	return s
}

// persists reports whether a namespace's contents belong on disk. It is the one
// place the tiering is decided; see the type comment for why each exclusion is
// there. If the standalone module ever gains an encryption key, the secret half of
// this condition is what changes.
func (s *store) persists(namespace string) bool {
	return s.snap != nil &&
		!core.IsVolatileNamespace(namespace) &&
		!core.IsSecretNamespace(namespace)
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
	s.touch(namespace)
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
	s.touch(namespace)
	return nil
}

// touch records that a namespace needs writing and nudges the flusher. Called with
// the lock held; the nudge is non-blocking, so a flusher that is already awake or
// already has a pending nudge costs nothing. The flusher takes the lock only after
// receiving, so signalling under it cannot deadlock.
func (s *store) touch(namespace string) {
	if !s.persists(namespace) {
		return
	}
	s.dirty[namespace] = struct{}{}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// flusher writes dirty namespaces, waiting flushDelay after the first change so a
// burst of writes collapses into one file write per namespace. It does not flush on
// its way out: close does that synchronously, so a graceful stop cannot return
// before the last write has landed.
func (s *store) flusher() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		}

		timer := time.NewTimer(flushDelay)
		select {
		case <-timer.C:
		case <-s.done:
			timer.Stop()
			return
		}
		s.flush()
	}
}

// flush writes every dirty namespace. The maps are copied under the lock and the
// files written outside it, so a slow disk does not block flows mid-write. A
// namespace that fails to write is logged and left dirty, so the next flush — and
// the one close performs — tries again.
func (s *store) flush() {
	s.mu.Lock()
	pending := make(map[string]map[string]core.Entry, len(s.dirty))
	for namespace := range s.dirty {
		pending[namespace] = cloneNamespace(s.ns[namespace])
		delete(s.dirty, namespace)
	}
	s.mu.Unlock()

	for namespace, keys := range pending {
		if err := s.snap.write(namespace, keys); err != nil {
			slog.Error("standalone: could not persist a namespace, retrying on the next flush",
				"namespace", namespace, "error", err)
			// touch rather than a bare re-mark: it also re-arms the flusher. Without
			// the nudge a transient disk error would park the namespace as dirty
			// until the next unrelated write, so a failure would be silently
			// deferred to process exit — which is the one moment it might not run.
			s.mu.Lock()
			s.touch(namespace)
			s.mu.Unlock()
		}
	}
}

// close stops the flusher and writes whatever is still dirty, so a graceful stop
// leaves a complete store rather than one missing the last half-second of writes.
// It is safe to call more than once.
func (s *store) close() {
	if s.snap == nil {
		return
	}
	s.closing.Do(func() {
		close(s.done)
		s.wg.Wait()
		s.flush()
	})
}

// cloneNamespace copies a namespace's entries, values included, so the flusher can
// encode them without holding the lock.
func cloneNamespace(keys map[string]core.Entry) map[string]core.Entry {
	out := make(map[string]core.Entry, len(keys))
	for k, e := range keys {
		out[k] = core.Entry{Value: cloneBytes(e.Value), Version: e.Version}
	}
	return out
}

func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte(nil), b...)
}
