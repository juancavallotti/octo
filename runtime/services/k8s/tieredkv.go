package k8s

import (
	"context"

	"github.com/juancavallotti/octo/runtime/core"
)

// tieredStore routes each KV call to the backend its namespace names: volatile
// namespaces to Redis, everything else to the orchestrator.
//
// Dispatching on the namespace rather than on a flag is what keeps the two tiers
// from being two APIs. A caller names the namespace it wants and gets the same
// versioned, compare-and-swap semantics either way; nothing above this type knows
// there is more than one store, which is why the object blocks, the cache and the
// secret store all reached their tier without changing shape.
//
// volatile is nil when the pod was given no REDIS_URL. Then everything falls
// through to the orchestrator: a volatile object costs what a persistent one
// costs, which is a worse deal but not a broken one. Refusing volatile writes
// because an optional dependency is absent would take down flows over a tier whose
// entire promise is that losing it is survivable.
type tieredStore struct {
	persistent *httpStore
	volatile   *redisStore // nil falls back to persistent
}

// storeFor picks the backend for a namespace.
//
//nolint:ireturn // returns core.KV so the two backends are interchangeable here
func (t *tieredStore) storeFor(namespace string) core.KV {
	if t.volatile != nil && core.IsVolatileNamespace(namespace) {
		return t.volatile
	}
	return t.persistent
}

func (t *tieredStore) Get(ctx context.Context, namespace, key string) (core.Entry, bool, error) {
	return t.storeFor(namespace).Get(ctx, namespace, key)
}

func (t *tieredStore) Set(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	return t.storeFor(namespace).Set(ctx, namespace, key, value, expectedVersion)
}

func (t *tieredStore) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	return t.storeFor(namespace).Delete(ctx, namespace, key, expectedVersion)
}

// close releases both backends. The Redis error is returned and the orchestrator
// client's is not because only the former holds a socket; see httpStore.close.
func (t *tieredStore) close() error {
	t.persistent.close()
	if t.volatile == nil {
		return nil
	}
	return t.volatile.close()
}
