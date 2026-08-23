// Package redisx opens the process's Redis connection.
//
// It is a byte-for-byte twin of logs/internal/redisx, and the two must be kept in
// sync by hand: the orchestrator and the aggregator do not share a go.mod, so
// neither can import the other's copy. The same arrangement holds for the trace
// subject name (see the note on TraceSubject in logs/internal/ingest/trace.go).
//
// It is a package rather than a few lines in main because both callers need the
// same three decisions — how the URL is parsed, how long the first connection is
// given, and what a failure means — and because they disagree about the last one.
// The aggregator cannot work without Redis; the orchestrator only reports on it.
// Both use Open, and each decides for itself what to do with the error.
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

// Open parses a redis:// or rediss:// URL, connects, and proves the connection
// with a PING before returning.
//
// The PING is the point. A go-redis client is lazy — NewClient never fails and
// the first real command is what discovers an unreachable server — so without it
// a misconfigured URL becomes an error much later, in whatever code path happened
// to run first, rather than at the one moment somebody is watching.
func Open(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		// The URL may carry a password, and go-redis includes the string it was
		// given in its parse errors. Say only that it did not parse.
		return nil, fmt.Errorf("redis: REDIS_URL is not a valid redis:// url: %w", redactURL(err, url))
	}

	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		// opts.Addr rather than the URL, for the same reason: the host and port are
		// what an operator needs to see, and they are the half that carries no
		// credential.
		return nil, fmt.Errorf("redis: connect %s: %w", opts.Addr, err)
	}
	return client, nil
}
