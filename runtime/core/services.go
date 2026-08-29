package core

import (
	"context"
	"errors"
	"strings"
)

// ErrVersionConflict is returned by a KV write when the caller's expected version
// does not match the object's current version. The caller should re-read the
// object and retry the write against the fresh version, so no concurrent update is
// silently lost.
var ErrVersionConflict = errors.New("kv: object version conflict")

// ErrNoKV is returned by the no-op KV's writes: with no store configured there is
// nowhere to persist, so failing loudly beats silently dropping the value.
var ErrNoKV = errors.New("kv: no store configured")

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

// Preset KV namespaces. Keys never cross namespaces, so these partition the single
// store by owner and by secrecy. The "_secrets" namespaces hold sensitive values
// the backend encrypts at rest (the k8s module); the SecretStore writes there so
// secrets share the KV table but never collide with plain keys. More may be added
// over time.
const (
	// NamespaceSystem holds internal runtime and connector state that
	// user-configured blocks must not read or tamper with.
	NamespaceSystem = "system"
	// NamespaceUser holds state owned by user-configured blocks (e.g. a cache or a
	// store block).
	NamespaceUser = "user"
	// NamespaceSystemSecrets holds internal secrets (encrypted at rest by the backend).
	NamespaceSystemSecrets = "system_secrets"
	// NamespaceUserSecrets holds user-owned secrets (encrypted at rest by the backend).
	NamespaceUserSecrets = "user_secrets"
	// NamespaceSystemVolatile holds internal state whose loss is survivable.
	NamespaceSystemVolatile = "system_volatile"
	// NamespaceUserVolatile holds user-owned state whose loss is survivable (e.g. a
	// memoized cache body).
	NamespaceUserVolatile = "user_volatile"
)

// SecretNamespaceSuffix marks the namespaces whose values the backend encrypts at
// rest. VolatileNamespaceSuffix marks the namespaces a backend is allowed to drop.
// Both are suffixes rather than a flag on the call because a namespace is already
// the store's unit of partitioning: a backend routes on the name it was given, and
// nothing else about the API has to change to gain a tier.
const (
	SecretNamespaceSuffix   = "_secrets"
	VolatileNamespaceSuffix = "_volatile"
)

// Entry is a versioned KV value. Version is monotonic per key: a freshly created
// object has version 1, and each successful write increments it. A reader passes
// the version it last saw to the next write to detect concurrent updates.
type Entry struct {
	Value   []byte
	Version int64
}

// KV is a deployment-scoped key/value store available to connectors and blocks.
// Every operation takes a namespace: keys are isolated per namespace, so state an
// internal component writes under its own namespace is invisible to a key read or
// written under another. A component using the store is expected to confine itself
// to a namespace it owns (e.g. a user-facing cache block writes under a "user"
// namespace), keeping internal state out of reach.
//
// A namespace also picks a durability tier. A persistent namespace is backed by
// storage that survives a restart: the orchestrator's database in the k8s module, a
// serialized file in the standalone one. A volatile namespace (see
// VolatileNamespace) makes no durability promise at all — its values live in Redis
// in the k8s module and in process memory in the standalone one, and either may
// drop them on a restart or under memory pressure. So volatile is for state whose
// loss costs a recompute, never for state whose loss costs correctness.
//
// Writes use optimistic concurrency: expectedVersion 0 creates the key (and fails
// if it already exists), while a positive expectedVersion must equal the stored
// version. A mismatch returns ErrVersionConflict. Successful writes return the new
// version.
type KV interface {
	// Get returns the entry for key in namespace. ok is false when the key is absent.
	Get(ctx context.Context, namespace, key string) (entry Entry, ok bool, err error)
	// Set stores value and returns the new version.
	Set(ctx context.Context, namespace, key string, value []byte, expectedVersion int64) (newVersion int64, err error)
	// Delete removes key in namespace. expectedVersion 0 deletes unconditionally; a
	// positive value must match the stored version or Delete returns ErrVersionConflict.
	Delete(ctx context.Context, namespace, key string, expectedVersion int64) error
}

// SecretStore is a store for sensitive values. It has the same namespaced, versioned
// API as KV and shares its backing store, but it routes every operation to a
// dedicated secret namespace (NamespaceSystemSecrets / NamespaceUserSecrets) whose
// values the backend encrypts at rest. So secrets never collide with plain keys and
// ordinary KV traffic pays no encryption cost, without a second table. The caller
// passes the same logical namespaces it uses for KV (system/user); the store maps
// them to their secret counterparts.
//
// A secret counterpart is never a volatile namespace — a volatile namespace must
// not be handed to a SecretStore, because the volatile backends neither encrypt nor
// promise to keep what they are given. How durable the secret counterpart actually
// is remains the backend's decision, and the two differ: the k8s module encrypts
// secrets into the orchestrator's database, while the standalone module keeps them
// in process memory, having no key to encrypt them with.
type SecretStore interface {
	// Get returns the (decrypted) entry for key in namespace; ok is false when absent.
	Get(ctx context.Context, namespace, key string) (entry Entry, ok bool, err error)
	// Set encrypts and stores value, returning the new version.
	Set(ctx context.Context, namespace, key string, value []byte, expectedVersion int64) (newVersion int64, err error)
	// Delete removes key (see KV.Delete for the version semantics).
	Delete(ctx context.Context, namespace, key string, expectedVersion int64) error
}

// NewSecretStore returns a SecretStore backed by kv: it maps each logical namespace
// to its secret counterpart (system -> system_secrets, user -> user_secrets) so
// secrets live in the same store as KV under dedicated namespaces the backend
// encrypts. Every module builds its SecretStore this way over its own KV.
//
//nolint:ireturn // returns the SecretStore interface intentionally
func NewSecretStore(kv KV) SecretStore { return secretStore{kv: kv} }

// secretStore adapts a KV into a SecretStore by rewriting the namespace.
type secretStore struct{ kv KV }

func (s secretStore) Get(ctx context.Context, namespace, key string) (Entry, bool, error) {
	return s.kv.Get(ctx, secretNamespace(namespace), key)
}

func (s secretStore) Set(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	return s.kv.Set(ctx, secretNamespace(namespace), key, value, expectedVersion)
}

func (s secretStore) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	return s.kv.Delete(ctx, secretNamespace(namespace), key, expectedVersion)
}

// secretNamespace maps a logical namespace to the secret namespace whose values the
// backend encrypts. The known namespaces map to their named constants; any other
// gets a "_secrets" suffix, which the backend also recognizes.
func secretNamespace(namespace string) string {
	switch namespace {
	case NamespaceSystem:
		return NamespaceSystemSecrets
	case NamespaceUser:
		return NamespaceUserSecrets
	default:
		return namespace + SecretNamespaceSuffix
	}
}

// IsSecretNamespace reports whether a namespace holds secrets. Backends need this
// to decide what to encrypt — and, in the standalone module, what to keep out of a
// file altogether.
func IsSecretNamespace(namespace string) bool {
	return strings.HasSuffix(namespace, SecretNamespaceSuffix)
}

// VolatileNamespace maps a logical namespace to its volatile counterpart, the way
// secretNamespace maps it to its secret one. A caller that wants the volatile tier
// names the namespace it gets back; nothing else about the call changes.
//
// It must never be composed with the secret store: a secret is exactly the kind of
// value whose loss is not survivable, and the volatile backends do not encrypt.
func VolatileNamespace(namespace string) string {
	switch namespace {
	case NamespaceSystem:
		return NamespaceSystemVolatile
	case NamespaceUser:
		return NamespaceUserVolatile
	default:
		return namespace + VolatileNamespaceSuffix
	}
}

// IsVolatileNamespace reports whether a namespace is one a backend may drop.
func IsVolatileNamespace(namespace string) bool {
	return strings.HasSuffix(namespace, VolatileNamespaceSuffix)
}

// RuntimeServices is the set of generally-available services wired into the runtime
// execution context. The active implementation is chosen at startup by the
// RUNTIME_SERVICES_MODULE environment variable (standalone or k8s). Close releases
// the implementation's resources; the process owner (the CLI) owns its lifecycle,
// not an individual Service generation.
//
//nolint:interfacebloat // one accessor per platform capability (KV, queues, topics, ...)
type RuntimeServices interface {
	//nolint:ireturn // returns the LeaderElection interface a connector depends on
	LeaderElection() LeaderElection
	// Leases returns the module's fail-fast claims on a name. It is an accessor
	// rather than an optional side interface because every module has one: a map
	// under a mutex in the standalone module, a coordination Lease object in the
	// k8s one. See the Leases doc comment for why this is not LeaderElection.
	//
	//nolint:ireturn // returns the Leases interface a caller depends on
	Leases() Leases
	//nolint:ireturn // returns the KV interface a connector depends on
	KV() KV
	//nolint:ireturn // returns the SecretStore interface a connector depends on
	Secrets() SecretStore
	//nolint:ireturn // returns the Queues interface a connector depends on
	Queues() Queues
	//nolint:ireturn // returns the Topics interface a connector depends on
	Topics() Topics
	//nolint:ireturn // returns the ResourceLoader interface blocks and env loading depend on
	Resources() ResourceLoader
	// AgentMemory returns where this module keeps what an agent remembers: a file
	// tree in the standalone module, the orchestrator's database in the k8s one.
	// It is an accessor rather than an optional side interface for the same reason
	// Queues and Traces are — no module can reasonably lack somewhere to put it,
	// only somewhere different. It is never nil: a module without an
	// implementation returns NoopAgentMemory, whose Enabled reports false.
	//
	//nolint:ireturn // returns the AgentMemory interface the engine depends on
	AgentMemory() AgentMemory
	// Traces returns where this module publishes trace records: a file for the
	// standalone module, a broker subject for the k8s one. It is an accessor
	// rather than an optional side interface (cf. LogShipper) because every
	// module has somewhere to put them — what differs is where, exactly as it
	// does for Queues and KV. It is never nil: a module with tracing disabled
	// returns NoopTracer.
	//
	//nolint:ireturn // returns the TracePublisher interface emitters depend on
	Traces() TracePublisher
	Close() error
}

// noopRuntimeServices is the safe fallback used when no services are wired into the
// context: leadership is always granted (single-process semantics) and the KV and
// secret stores have no backing store.
type noopRuntimeServices struct{}

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) LeaderElection() LeaderElection { return noopLeaderElection{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Leases() Leases { return noopLeases{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) KV() KV { return noopKV{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Secrets() SecretStore { return noopKV{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Queues() Queues { return noopQueues{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Topics() Topics { return noopTopics{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Resources() ResourceLoader { return NoopResourceLoader{} }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) Traces() TracePublisher { return NoopTracer() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (noopRuntimeServices) AgentMemory() AgentMemory { return NoopAgentMemory() }

func (noopRuntimeServices) Close() error { return nil }

// NoopLeaderElection returns a leader election that grants leadership
// unconditionally — the single-process semantics the standalone module wants,
// where there is nothing to coordinate. It is also the fallback the no-op services
// expose.
//
//nolint:ireturn // returns the LeaderElection interface intentionally
func NoopLeaderElection() LeaderElection { return noopLeaderElection{} }

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

// NoopKV returns a KV with no storage: reads miss and writes return ErrNoKV. A
// module whose backend does not offer a key-value store uses it so callers get the
// same degraded behavior everywhere instead of a nil store.
//
//nolint:ireturn // returns the KV interface intentionally
func NoopKV() KV { return noopKV{} }

// noopKV has no storage: reads miss and writes fail loudly. Its method set
// satisfies both KV and SecretStore, so the no-op services use it for each.
type noopKV struct{}

func (noopKV) Get(context.Context, string, string) (Entry, bool, error) {
	return Entry{}, false, nil
}

func (noopKV) Set(context.Context, string, string, []byte, int64) (int64, error) {
	return 0, ErrNoKV
}

func (noopKV) Delete(context.Context, string, string, int64) error { return ErrNoKV }

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
