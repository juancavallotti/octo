// Package redisx opens the process's Redis connection.
//
// It is the third copy of this package: orchestrator/internal/redisx and
// observability/internal/redisx are the others, and all three must be kept in sync by
// hand. The modules do not share a go.mod and the package is internal, so no
// copy can import another.
//
// It is a package rather than a few lines in main because every caller needs
// the same three decisions — how the URL is parsed, how long the first
// connection is given, and what a failure means — and because the callers
// disagree about the last one. That is why the decision is left to them rather
// than baked in here.
//
// The aggregator cannot work without Redis and fails to start. The orchestrator
// only reports on it and degrades. This sidecar does neither: it takes the lazy
// client from New and rides out an unreachable Redis indefinitely. Failing
// startup here would turn one cache outage into a restart storm across every
// production pod at once, and a stats sidecar that cannot store is one that has
// lost some history — not a pod that should be cycled.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// dialTimeout bounds the PING that proves the connection, not the connection
// itself. Generous, because this runs at startup while the rest of the namespace
// is also coming up: a Redis that is thirty seconds from ready is normal on a
// cold cluster, and a client that gave up in one would make the pod's own restart
// backoff the thing gating the install.
const dialTimeout = 30 * time.Second

// New parses a redis:// or rediss:// URL and returns a client for it.
//
// It does not connect. A go-redis client is lazy by design — it dials on the
// first command and reconnects on its own afterwards — so a client built here is
// usable even when the server is down, and becomes useful again when the server
// comes back without anything having to rebuild it.
//
// That is what a caller wants when it is *reporting* on Redis rather than relying
// on it: holding a live client is the difference between "unreachable" and
// "unconfigured", and between a health page that recovers and one that keeps
// reporting the failure it saw at boot. Callers that cannot work without Redis
// want Open instead.
func New(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		// The URL may carry a password, and go-redis includes the string it was
		// given in its parse errors. Say only that it did not parse.
		return nil, fmt.Errorf("redis: REDIS_URL is not a valid redis:// url: %w", redactURL(err, url))
	}
	return redis.NewClient(opts), nil
}

// Open is New plus a PING that proves the connection before returning.
//
// The PING is for callers that cannot work without Redis. Without it a
// misconfigured URL surfaces much later, in whatever code path happened to run
// first, rather than at the one moment somebody is watching the process start.
func Open(ctx context.Context, url string) (*redis.Client, error) {
	client, err := New(url)
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		addr := client.Options().Addr
		_ = client.Close()
		// The address rather than the URL, for the same reason the parse error is
		// redacted: the host and port are what an operator needs to see, and they are
		// the half that carries no credential.
		return nil, fmt.Errorf("redis: connect %s: %w", addr, err)
	}
	return client, nil
}
