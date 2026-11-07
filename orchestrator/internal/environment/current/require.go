package current

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// Lookup answers whether a name is an environment.
type Lookup interface {
	Exists(ctx context.Context, name string) (bool, error)
}

// Require refuses a request whose environment is not one: 404, naming the
// header, so a client with a stale selection is told what to change.
//
// The environment routes themselves are not checked. They are how a client
// with a stale selection finds out what exists, and refusing them for it would
// leave it nowhere to recover from.
//
// Nor are the routes unchecked names, which the guard serves without
// authenticating anyone: answering them by whether the named environment exists
// would let a caller who has not signed in learn which environments there are.
func Require(lookup Lookup, unchecked func(path string) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		environment := From(r.Context())
		if environment == "" || isEnvironmentsRoute(r.URL.Path) || unchecked(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ok, err := lookup.Exists(r.Context(), environment)
		if err != nil {
			slog.ErrorContext(r.Context(), "look up the request's environment", "environment", environment, "error", err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "the environment cannot be checked right now")
			return
		}
		if !ok {
			httpx.WriteError(w, http.StatusNotFound, "unknown environment (set by the "+Header+" header)")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isEnvironmentsRoute(path string) bool {
	return path == "/environments" || strings.HasPrefix(path, "/environments/")
}

// Cached wraps lookup with a short-lived memory of the environments that exist,
// so a burst of requests in one environment asks the database once. A deleted
// environment is still accepted for up to ttl, which is harmless: its namespace
// and rows are already gone, so a request there finds nothing to act on.
//
// A name that does not exist is not remembered: the header that carries it is
// the caller's to choose, so remembering every refusal would let anyone grow
// this memory without bound. The memory is bounded by the real environments.
func Cached(lookup Lookup, ttl time.Duration) Lookup {
	return &cached{lookup: lookup, ttl: ttl, answers: map[string]time.Time{}}
}

type cached struct {
	lookup  Lookup
	ttl     time.Duration
	mu      sync.Mutex
	answers map[string]time.Time
	now     func() time.Time
}

func (c *cached) Exists(ctx context.Context, name string) (bool, error) {
	now := time.Now()
	if c.now != nil {
		now = c.now()
	}
	c.mu.Lock()
	at, ok := c.answers[name]
	c.mu.Unlock()
	if ok && now.Sub(at) < c.ttl {
		return true, nil
	}
	exists, err := c.lookup.Exists(ctx, name)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	if exists {
		c.answers[name] = now
	} else {
		delete(c.answers, name)
	}
	c.mu.Unlock()
	return exists, nil
}
