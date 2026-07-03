package engine

import (
	"context"
	"sync"

	"github.com/juancavallotti/octo/core"
)

// fakeKV is an in-memory, versioned KV with the same optimistic-concurrency
// semantics as the standalone store. It lets the service-backed blocks
// (object-read/write, cache-scope, invalidate-cache) be tested without importing
// the services module, which would be an import cycle: services imports core, and
// this engine lives inside the core module.
type fakeKV struct {
	mu sync.Mutex
	ns map[string]map[string]core.Entry
}

func newFakeKV() *fakeKV { return &fakeKV{ns: make(map[string]map[string]core.Entry)} }

func (s *fakeKV) Get(_ context.Context, namespace, key string) (core.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.ns[namespace][key]
	if !ok {
		return core.Entry{}, false, nil
	}
	return core.Entry{Value: append([]byte(nil), e.Value...), Version: e.Version}, true, nil
}

func (s *fakeKV) Set(_ context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := s.ns[namespace]
	current := keys[key].Version
	if expectedVersion != current {
		return 0, core.ErrVersionConflict
	}
	if keys == nil {
		keys = make(map[string]core.Entry)
		s.ns[namespace] = keys
	}
	next := current + 1
	keys[key] = core.Entry{Value: append([]byte(nil), value...), Version: next}
	return next, nil
}

func (s *fakeKV) Delete(_ context.Context, namespace, key string, expectedVersion int64) error {
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

// fakeServices wires a fakeKV into the RuntimeServices contract.
type fakeServices struct{ kv *fakeKV }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) KV() core.KV { return f.kv }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Secrets() core.SecretStore { return core.NewSecretStore(f.kv) }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

func (f fakeServices) Close() error { return nil }

// withFakeServices returns a context carrying fresh in-memory services along with
// the underlying KV, so a test can both run a block and assert on what it stored.
func withFakeServices(ctx context.Context) (context.Context, *fakeKV) {
	svc := fakeServices{kv: newFakeKV()}
	return core.ContextWithRuntimeServices(ctx, svc), svc.kv
}
