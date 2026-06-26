package core

import (
	"context"
	"errors"
)

// ErrVersionConflict is returned by a KV write when the caller's expected version
// does not match the object's current version. The caller should re-read the
// object and retry the write against the fresh version, so no concurrent update is
// silently lost.
var ErrVersionConflict = errors.New("kv: object version conflict")

// errNoKV is returned by the Noop KV's writes: with no store configured there is
// nowhere to persist, so failing loudly beats silently dropping the value.
var errNoKV = errors.New("kv: no store configured")

// Leadership is a handle to an ongoing campaign for a leader-election key. Its
// IsLeader reports whether this replica currently holds leadership; Close stops
// campaigning and releases the key (best-effort). A connector typically acquires
// one per unit of exclusive work and gates that work on IsLeader.
type Leadership interface {
	// IsLeader reports whether this replica currently holds the key. It is safe to
	// call concurrently and cheap (it reads cached state, it does not block on the
	// backend).
	IsLeader() bool
	// Close stops campaigning for the key and releases it if held.
	Close() error
}

// LeaderElection lets a connector run work on exactly one replica across a cluster.
// Acquire starts campaigning for key in the background and returns a Leadership
// handle whose IsLeader tracks the current status. In the standalone module every
// Acquire is immediately and permanently the leader (a single process).
type LeaderElection interface {
	//nolint:ireturn // returns the Leadership interface the caller gates work on
	Acquire(ctx context.Context, key string) (Leadership, error)
}

// Entry is a versioned KV value. Version is monotonic per key: a freshly created
// object has version 1, and each successful write increments it. A reader passes
// the version it last saw to the next write to detect concurrent updates.
type Entry struct {
	Value   []byte
	Version int64
}

// KV is a small key/value store available to connectors and blocks. In the k8s
// module it is backed by the orchestrator API and scoped to the deployment; in the
// standalone module it is an in-process map.
//
// Writes use optimistic concurrency: expectedVersion 0 creates the key (and fails
// if it already exists), while a positive expectedVersion must equal the stored
// version. A mismatch returns ErrVersionConflict. Successful writes return the new
// version.
type KV interface {
	// Get returns the entry for key. ok is false when the key is absent.
	Get(ctx context.Context, key string) (entry Entry, ok bool, err error)
	// Set stores value as-is (no encryption) and returns the new version.
	Set(ctx context.Context, key string, value []byte, expectedVersion int64) (newVersion int64, err error)
	// SetSecret stores value encrypted at rest (where the backend supports it) and
	// returns the new version. Get transparently returns the decrypted value.
	SetSecret(ctx context.Context, key string, value []byte, expectedVersion int64) (newVersion int64, err error)
	// Delete removes key. expectedVersion 0 deletes unconditionally; a positive
	// value must match the stored version or Delete returns ErrVersionConflict.
	Delete(ctx context.Context, key string, expectedVersion int64) error
}

// RuntimeServices is the set of generally-available services wired into the runtime
// execution context. The active implementation is chosen at startup by the
// RUNTIME_SERVICES_MODULE environment variable (standalone or k8s). Close releases
// the implementation's resources; the process owner (the CLI) owns its lifecycle,
// not an individual Service generation.
type RuntimeServices interface {
	//nolint:ireturn // returns the LeaderElection interface a connector depends on
	LeaderElection() LeaderElection
	//nolint:ireturn // returns the KV interface a connector depends on
	KV() KV
	Close() error
}

// noopRuntimeServices is the safe fallback used when no services are wired into the
// context: leadership is always granted (single-process semantics) and the KV has
// no store.
type noopRuntimeServices struct{}

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) LeaderElection() LeaderElection { return noopLeaderElection{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) KV() KV { return noopKV{} }

func (noopRuntimeServices) Close() error { return nil }

// noopLeaderElection grants leadership unconditionally, matching single-process
// behavior where there is nothing to elect.
type noopLeaderElection struct{}

//nolint:ireturn // satisfies the LeaderElection interface
func (noopLeaderElection) Acquire(context.Context, string) (Leadership, error) {
	return alwaysLeader{}, nil
}

// alwaysLeader is permanently the leader.
type alwaysLeader struct{}

func (alwaysLeader) IsLeader() bool { return true }
func (alwaysLeader) Close() error   { return nil }

// noopKV has no storage: reads miss and writes fail loudly.
type noopKV struct{}

func (noopKV) Get(context.Context, string) (Entry, bool, error) { return Entry{}, false, nil }

func (noopKV) Set(context.Context, string, []byte, int64) (int64, error) { return 0, errNoKV }

func (noopKV) SetSecret(context.Context, string, []byte, int64) (int64, error) {
	return 0, errNoKV
}

func (noopKV) Delete(context.Context, string, int64) error { return errNoKV }

// noopServices is the shared fallback instance returned when the context carries no
// services.
var noopServices RuntimeServices = noopRuntimeServices{}

// NoopRuntimeServices returns the shared no-op services: an always-leader election
// and a KV with no store. It is the fallback for contexts that were not wired with
// real services (e.g. tests, or a connector started outside the runtime).
//
//nolint:ireturn // returns the RuntimeServices interface intentionally
func NoopRuntimeServices() RuntimeServices { return noopServices }

// servicesKey is the unexported context key under which RuntimeServices are stored.
type servicesKey struct{}

// ContextWithRuntimeServices returns a copy of ctx carrying svc, retrievable with
// RuntimeServicesFromContext. A nil svc stores the no-op services so lookups always
// return a usable value.
func ContextWithRuntimeServices(ctx context.Context, svc RuntimeServices) context.Context {
	if svc == nil {
		svc = noopServices
	}
	return context.WithValue(ctx, servicesKey{}, svc)
}

// RuntimeServicesFromContext returns the services carried by ctx, or the shared
// no-op services when none were wired.
//
//nolint:ireturn // returns the RuntimeServices interface intentionally
func RuntimeServicesFromContext(ctx context.Context) RuntimeServices {
	if svc, ok := ctx.Value(servicesKey{}).(RuntimeServices); ok && svc != nil {
		return svc
	}
	return noopServices
}
