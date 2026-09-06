package testkit

import (
	"context"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
)

// FakeKV is an in-memory, versioned KV with the same optimistic-concurrency
// semantics as the standalone store. It lets the service-backed blocks
// (object-read/write, cache-scope, invalidate-cache) be tested without importing
// the services module, which imports the blocks' own dependencies back.
type FakeKV struct {
	mu sync.Mutex
	ns map[string]map[string]core.Entry
}

func NewFakeKV() *FakeKV { return &FakeKV{ns: make(map[string]map[string]core.Entry)} }

func (s *FakeKV) Get(_ context.Context, namespace, key string) (core.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.ns[namespace][key]
	if !ok {
		return core.Entry{}, false, nil
	}
	return core.Entry{Value: append([]byte(nil), e.Value...), Version: e.Version}, true, nil
}

func (s *FakeKV) Set(_ context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
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

func (s *FakeKV) Delete(_ context.Context, namespace, key string, expectedVersion int64) error {
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

// FakeServices wires a FakeKV into the RuntimeServices contract.
type FakeServices struct {
	Store  *FakeKV
	Memory core.AgentMemory
}

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) Leases() core.Leases { return core.NoopLeases() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) KV() core.KV { return f.Store }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) Secrets() core.SecretStore { return core.NewSecretStore(f.Store) }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f FakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (f FakeServices) Traces() core.TracePublisher { return core.NoopTracer() }

//nolint:ireturn // implements core.RuntimeServices
func (f FakeServices) AgentMemory() core.AgentMemory {
	if f.Memory == nil {
		return core.NoopAgentMemory()
	}
	return f.Memory
}

func (f FakeServices) Close() error { return nil }

// WithFakeServices returns a context carrying fresh in-memory services along with
// the underlying KV, so a test can both run a block and assert on what it stored.
func WithFakeServices(ctx context.Context) (context.Context, *FakeKV) {
	svc := FakeServices{Store: NewFakeKV()}
	return core.ContextWithRuntimeServices(ctx, svc), svc.Store
}

// WithFakeMemory returns a context whose agent-memory store is a real in-memory
// one rather than the no-op, so a test can exercise the first-class path and read
// back what the run stored. The KV is real too, so the legacy fallback and the
// migration off it can be exercised in the same context.
func WithFakeMemory(ctx context.Context) (context.Context, *FakeMemory, *FakeKV) {
	mem, kv := NewFakeMemory(), NewFakeKV()
	svc := FakeServices{Store: kv, Memory: mem}
	return core.ContextWithRuntimeServices(ctx, svc), mem, kv
}
