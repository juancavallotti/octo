// Package redisx opens the process's Redis connection.
//
// It owns how the URL is parsed and how long the first connection is given. What a
// failure means is left to the caller: New returns a lazy client for one that rides
// out an unreachable server, and Open proves the connection for one that cannot
// work without it.
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// dialTimeout bounds the PING that proves the connection, not the connection
// itself. Generous, because this runs at startup while the rest of the namespace is
// also coming up and a Redis thirty seconds from ready is normal on a cold
// cluster.
const dialTimeout = 30 * time.Second

// New parses a redis:// or rediss:// URL and returns a client for it.
//
// It does not connect. A go-redis client is lazy by design — it dials on the
// first command and reconnects on its own afterwards — so a client built here is
// usable even when the server is down, and becomes useful again when the server
// comes back without anything having to rebuild it.
//
// That is what a caller reporting on Redis wants rather than one relying on it:
// holding a live client is the difference between "unreachable" and "unconfigured".
// A caller that cannot work without Redis wants Open instead.
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
