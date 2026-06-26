// Caching built on the runtime KV store: the cache-scope composite memoizes the
// body its wrapped flow produces, and the invalidate-cache leaf evicts an entry.
// Both key the user namespace (core.NamespaceUser) by the SHA-256 of an evaluated
// CEL key, so an invalidate-cache with the same key expression targets the same
// entry a cache-scope wrote.
//
// The KV store has no native TTL, so cache-scope encodes the expiry inside the
// stored value (cacheEnvelope) and checks it on read; an expired entry is treated
// as a miss and overwritten on the next run. Only the message body is cached:
// variables the wrapped flow sets are not restored on a hit.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

// defaultCacheTTL bounds a cache-scope entry when its settings name no ttl.
const defaultCacheTTL = 60 * time.Second

func init() {
	core.MustRegisterBlock("invalidate-cache", newInvalidateCache)
}

// cacheEnvelope is the value a cache-scope stores: the cached body plus its expiry.
// ExpiresAt is a unix-nanosecond deadline; 0 means the entry never expires.
type cacheEnvelope struct {
	ExpiresAt int64           `json:"expiresAt"`
	Body      json.RawMessage `json:"body"`
}

// cacheKey hashes the evaluated key expression so the stored key is bounded and
// safe for backends that put it in a URL path (the k8s KV API).
func cacheKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// cacheScope memoizes the body its wrapped flow produces, keyed by an evaluated
// expression. On a fresh hit it restores the cached body and skips the flow.
type cacheScope struct {
	body *Flow
	key  *expr.Program
	ttl  time.Duration
	env  map[string]any
}

// cacheScope builds the composite from the block's body slot and its key/ttl
// fields, compiling the key expression once so a bad expression fails at startup.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) cacheScope(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if cfg.Body == nil {
		return nil, errors.New("cache-scope block requires a body flow")
	}
	if err := allowSlots(cfg, blockKindCacheScope, "body", "key", "ttl"); err != nil {
		return nil, err
	}
	if cfg.Key == "" {
		return nil, errors.New("cache-scope block requires a key expression")
	}
	key, err := expr.Compile(cfg.Key, exprVarNames...)
	if err != nil {
		return nil, err
	}
	ttl, err := resolveCacheTTL(cfg.TTL)
	if err != nil {
		return nil, err
	}

	body, err := b.subFlow(*cfg.Body)
	if err != nil {
		return nil, fmt.Errorf("cache-scope body: %w", err)
	}
	return &cacheScope{body: body, key: key, ttl: ttl, env: envActivation(b.deps.Env)}, nil
}

// resolveCacheTTL parses the ttl setting: empty uses the default, otherwise a Go
// duration string ("0" disables expiry).
func resolveCacheTTL(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultCacheTTL, nil
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("cache-scope ttl %q: %w", raw, err)
	}
	if ttl < 0 {
		return 0, fmt.Errorf("cache-scope ttl %q must not be negative", raw)
	}
	return ttl, nil
}

// Process returns the cached body on a fresh hit, otherwise runs the wrapped flow
// and stores its body for next time. Storing is best-effort: a version conflict
// (another worker cached first) or an absent store leaves the result correct, just
// uncached.
func (c *cacheScope) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	keyValue, err := c.key.EvalString(messageActivation(msg, c.env))
	if err != nil {
		return nil, fmt.Errorf("cache-scope key: %w", err)
	}
	key := cacheKey(keyValue)
	kv := core.RuntimeServicesFromContext(ctx).KV()

	entry, ok, err := kv.Get(ctx, core.NamespaceUser, key)
	if err == nil && ok {
		if cached, hit := decodeFreshEnvelope(entry.Value); hit {
			if setErr := msg.SetBodyJSON(cached); setErr == nil {
				return msg, nil // fresh cache hit: skip the wrapped flow
			}
		}
	}

	out, err := c.body.Process(ctx, msg)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil // the flow dropped the message; nothing to cache
	}

	c.store(ctx, kv, key, entry.Version, out)
	return out, nil
}

// decodeFreshEnvelope returns the cached body when value is a cache envelope that
// has not expired; hit is false on a decode error or an expired entry, so the
// caller treats it as a miss.
func decodeFreshEnvelope(value []byte) (json.RawMessage, bool) {
	var env cacheEnvelope
	if err := json.Unmarshal(value, &env); err != nil {
		return nil, false
	}
	if env.ExpiresAt != 0 && time.Now().UnixNano() >= env.ExpiresAt {
		return nil, false
	}
	return env.Body, true
}

// store writes the flow's body into the cache under key, stamping the expiry from
// the scope's ttl. expectedVersion is the version read on the way in, so an
// expired entry is overwritten and a fresh key is created. Any error is swallowed:
// caching is an optimization, not part of the result.
func (c *cacheScope) store(ctx context.Context, kv core.KV, key string, expectedVersion int64, out *types.Message) {
	body, err := out.BodyJSON()
	if err != nil {
		return
	}
	env := cacheEnvelope{Body: body}
	if c.ttl > 0 {
		env.ExpiresAt = time.Now().Add(c.ttl).UnixNano()
	}
	encoded, err := json.Marshal(env)
	if err != nil {
		return
	}
	_, _ = kv.Set(ctx, core.NamespaceUser, key, encoded, expectedVersion)
}

// invalidateCacheSettings configures the invalidate-cache leaf.
type invalidateCacheSettings struct {
	// Key is a CEL expression evaluated to the cache key to evict (required).
	Key string `json:"key"`
}

// invalidateCache evicts the cache entry for an evaluated key, so a later
// cache-scope with the same key recomputes.
type invalidateCache struct {
	key *expr.Program
	env map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newInvalidateCache(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg invalidateCacheSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Key == "" {
		return nil, errors.New("invalidate-cache requires a key expression")
	}
	key, err := expr.Compile(cfg.Key, exprVarNames...)
	if err != nil {
		return nil, err
	}
	return &invalidateCache{key: key, env: envActivation(deps.Env)}, nil
}

// Process evicts the entry for the evaluated key (unconditionally, ignoring the
// version) and forwards the message unchanged.
func (p *invalidateCache) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	keyValue, err := p.key.EvalString(messageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("invalidate-cache key: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	if delErr := kv.Delete(ctx, core.NamespaceUser, cacheKey(keyValue), 0); delErr != nil {
		return nil, fmt.Errorf("invalidate-cache %q: %w", keyValue, delErr)
	}
	return msg, nil
}
