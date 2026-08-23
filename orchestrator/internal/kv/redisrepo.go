package kv

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// The volatile tier's storage, and the orchestrator's half of a two-module contract.
//
// Runtime pods write these keys themselves, straight to Redis, without passing
// through here — the volatile tier has no database and no encryption key, so an
// orchestrator hop would buy nothing and cost a round trip on the one tier whose
// point is being cheap. What the orchestrator still needs Redis for is everything
// the runtime does not do: serving the object browser, and sweeping a deployment's
// keys when it is undeployed.
//
// So the key layout and the two scripts below are duplicated in
// runtime/services/k8s/rediskv.go and must be changed together. They cannot be
// shared: the orchestrator and the runtime do not share a go.mod. It is the same
// hand-synced arrangement as redisx and the trace subject name.
//
// Values here are stored in the clear and may be evicted at any time — the bundled
// Redis runs with maxmemory-policy allkeys-lru and no persistence. That is what
// volatile means, and it is why the service layer refuses a namespace that is both
// secret and volatile.

// redisPrefix, and the hash fields one object is stored in. Kept byte-identical
// with the runtime's copy.
const redisPrefix = "octo:kv"

const (
	fieldValue   = "v"
	fieldVersion = "ver"
	fieldStamp   = "ts" // unix ms; Redis has no per-key mtime and the browser wants one
)

// conflictSentinel is what the scripts return instead of a version when the
// caller's expected version does not match. Versions are positive, so a negative
// number cannot be mistaken for one.
const conflictSentinel = -1

// redisTimeout bounds a single operation, and redisScanCount is how many keys a
// SCAN cursor asks for per round trip — large enough that listing a namespace is a
// handful of calls, small enough that no single call blocks the server.
const (
	redisTimeout   = 5 * time.Second
	redisScanCount = 500
)

// writeScript and deleteScript are the compare-and-swap pair. They are scripts
// because the version check and the write must not be separable: between an HGET
// and an HSET another writer can land its own, and both would think they had won.
// Byte-identical with the runtime's copy.
const writeScript = `
local current = tonumber(redis.call('HGET', KEYS[1], 'ver')) or 0
if current ~= tonumber(ARGV[2]) then return -1 end
local next = current + 1
redis.call('HSET', KEYS[1], 'v', ARGV[1], 'ver', next, 'ts', ARGV[3])
return next
`

const deleteScript = `
local current = tonumber(redis.call('HGET', KEYS[1], 'ver'))
if current == nil then return 0 end
local expected = tonumber(ARGV[1])
if expected ~= 0 and expected ~= current then return -1 end
redis.call('DEL', KEYS[1])
return 1
`

// RedisRepo is the volatile tier's store. It satisfies the same repository
// interface *Repo does, so the service can route to either without knowing which.
type RedisRepo struct {
	client *redis.Client
	write  *redis.Script
	del    *redis.Script
}

// NewRedisRepo returns a repo over client. A nil client returns nil, which the
// service reads as "no volatile backend" and falls back to Postgres.
func NewRedisRepo(client *redis.Client) *RedisRepo {
	if client == nil {
		return nil
	}
	return &RedisRepo{
		client: client,
		write:  redis.NewScript(writeScript),
		del:    redis.NewScript(deleteScript),
	}
}

// keyOf builds "octo:kv:{deployment}:{namespace}:{key}".
//
// The deployment id comes first so a SCAN can sweep exactly one deployment, the
// namespace next so a listing reads one namespace and not the rest, and the object
// key last so it may contain colons. A namespace may NOT contain one — see
// checkVolatileNamespace, which is what makes that true rather than merely hoped.
// Compared against the runtime's copy by its rediskv_contract_test.go.
const volatileKeyLayout = "octo:kv:{deployment}:{namespace}:{key}"

func keyOf(deploymentID, namespace, key string) string {
	return redisPrefix + ":" + deploymentID + ":" + namespace + ":" + key
}

// deploymentPrefix is everything before the namespace, for scanning.
func deploymentPrefix(deploymentID string) string {
	return redisPrefix + ":" + deploymentID + ":"
}

// checkVolatileNamespace refuses a namespace the key layout cannot represent.
//
// A colon is the layout's delimiter, and only the object key is allowed to contain
// one. Without this check, namespace "a:b" with key "k" and namespace "a" with key
// "b:k" produce the same Redis key — two callers who believe they are in separate
// keyspaces silently sharing one, with each other's versions. ListNamespaces would
// also report only the part before the first colon, so the aliasing would not even
// be visible in the browser.
//
// The runtime only ever passes its own namespace constants, but the raw KV route
// takes the namespace from the URL path, so this is reachable from outside.
func checkVolatileNamespace(namespace string) error {
	if strings.ContainsRune(namespace, ':') {
		return ErrInvalidNamespace
	}
	return nil
}

// Get returns the value and version for a key. A key Redis evicted reads as absent,
// which is the whole promise of this tier.
func (r *RedisRepo) Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error) {
	if err := checkVolatileNamespace(namespace); err != nil {
		return nil, 0, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	fields, err := r.client.HMGet(ctx, keyOf(deploymentID, namespace, key), fieldValue, fieldVersion).Result()
	if err != nil {
		return nil, 0, false, fmt.Errorf("kv redis: get: %w", err)
	}
	if len(fields) != 2 || fields[0] == nil || fields[1] == nil {
		return nil, 0, false, nil
	}
	value, ok := fields[0].(string)
	if !ok {
		return nil, 0, false, fmt.Errorf("kv redis: get: value is %T, want string", fields[0])
	}
	version, err := parseVersionField(fields[1])
	if err != nil {
		return nil, 0, false, err
	}
	return []byte(value), version, true, nil
}

// Write stores value under the usual optimistic concurrency and returns the new
// version.
func (r *RedisRepo) Write(
	ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	if err := checkVolatileNamespace(namespace); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	res, err := r.write.Run(ctx, r.client,
		[]string{keyOf(deploymentID, namespace, key)},
		value, expectedVersion, time.Now().UnixMilli(),
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("kv redis: write: %w", err)
	}
	if res == conflictSentinel {
		return 0, ErrVersionConflict
	}
	return res, nil
}

// Delete removes a key. expectedVersion 0 deletes unconditionally; a positive value
// must match. Deleting an absent key is a no-op.
func (r *RedisRepo) Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error {
	if err := checkVolatileNamespace(namespace); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	res, err := r.del.Run(ctx, r.client,
		[]string{keyOf(deploymentID, namespace, key)}, expectedVersion,
	).Int64()
	if err != nil {
		return fmt.Errorf("kv redis: delete: %w", err)
	}
	if res == conflictSentinel {
		return ErrVersionConflict
	}
	return nil
}

// List returns metadata for every key in a namespace.
//
// SCAN rather than KEYS: KEYS blocks the server for the length of the keyspace,
// and this runs against an instance the log aggregator is also using. SCAN's
// guarantee is weaker — a key added or removed while the cursor is open may or may
// not appear — which is the right trade for a troubleshooting browser and would be
// the wrong one for anything on a correctness path. Nothing here is.
func (r *RedisRepo) List(ctx context.Context, deploymentID, namespace string) ([]Entry, error) {
	if err := checkVolatileNamespace(namespace); err != nil {
		return nil, err
	}
	prefix := keyOf(deploymentID, namespace, "")

	// SCAN promises every key present throughout the walk at least once — not at
	// most once — so the same key can arrive on two pages. Listing it twice would
	// show the browser two rows for one object, so keys are deduplicated as they
	// are visited. ListNamespaces below does the same for the same reason.
	var entries []Entry
	seen := make(map[string]struct{})
	err := r.scanPages(ctx, globEscape(prefix)+"*", func(keys []string) error {
		for _, full := range keys {
			if _, dup := seen[full]; dup {
				continue
			}
			seen[full] = struct{}{}
			entry, ok, entryErr := r.entryOf(ctx, full, strings.TrimPrefix(full, prefix))
			if entryErr != nil {
				return entryErr
			}
			if ok {
				entries = append(entries, entry)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// entryOf reads one key's metadata. A key that vanished between the scan and this
// read is reported as absent rather than as an error: this tier is allowed to drop
// things, so a listing racing an eviction is normal, not a failure.
func (r *RedisRepo) entryOf(ctx context.Context, full, key string) (Entry, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	fields, err := r.client.HMGet(ctx, full, fieldValue, fieldVersion, fieldStamp).Result()
	if err != nil {
		return Entry{}, false, fmt.Errorf("kv redis: list: %w", err)
	}
	if len(fields) != 3 || fields[1] == nil {
		return Entry{}, false, nil
	}
	version, err := parseVersionField(fields[1])
	if err != nil {
		return Entry{}, false, err
	}
	entry := Entry{Key: key, Version: version, UpdatedAt: stampOf(fields[2])}
	if value, ok := fields[0].(string); ok {
		entry.Size = len(value)
	}
	return entry, true, nil
}

// ListNamespaces returns the namespaces this deployment holds volatile data in. It
// splits each scanned key at the first colon after the deployment prefix, which is
// well-defined because a namespace contains no colon (see keyOf).
func (r *RedisRepo) ListNamespaces(ctx context.Context, deploymentID string) ([]string, error) {
	prefix := deploymentPrefix(deploymentID)

	seen := make(map[string]struct{})
	var out []string
	err := r.scanPages(ctx, globEscape(prefix)+"*", func(keys []string) error {
		for _, full := range keys {
			rest := strings.TrimPrefix(full, prefix)
			namespace, _, found := strings.Cut(rest, ":")
			if !found || namespace == "" {
				continue // not a key this layout produces; leave it alone
			}
			if _, dup := seen[namespace]; dup {
				continue
			}
			seen[namespace] = struct{}{}
			out = append(out, namespace)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteByDeployment removes every volatile key a deployment holds, for cleanup on
// undeploy. UNLINK rather than DEL so the reclaim happens off the main thread: a
// deployment with many keys should not stall the instance the aggregator is also
// folding traces in.
func (r *RedisRepo) DeleteByDeployment(ctx context.Context, deploymentID string) error {
	return r.scanPages(ctx, globEscape(deploymentPrefix(deploymentID))+"*", func(keys []string) error {
		if len(keys) == 0 {
			return nil
		}
		unlinkCtx, cancel := context.WithTimeout(ctx, redisTimeout)
		defer cancel()
		if err := r.client.Unlink(unlinkCtx, keys...).Err(); err != nil {
			return fmt.Errorf("kv redis: delete deployment: %w", err)
		}
		return nil
	})
}

// scanPages walks the keyspace with a cursor, handing each page to visit as it
// arrives.
//
// A page at a time rather than one slice of every match: a deployment holding many
// small volatile objects would otherwise make the orchestrator retain the whole key
// set — during an object-browser request, or during undeploy — for a store whose
// size nothing here bounds. The bundled Redis has a memory ceiling; an external one
// need not, and the caller's own memory is not what that ceiling protects.
//
// SCAN's guarantee is weaker than a snapshot: a key added or removed while the
// cursor is open may or may not be visited, and a key present throughout may be
// visited more than once — callers that build a list deduplicate. That is the right
// trade for a troubleshooting listing and for a best-effort cleanup, and would be
// the wrong one for anything on a correctness path. Nothing here is.
func (r *RedisRepo) scanPages(ctx context.Context, pattern string, visit func([]string) error) error {
	var cursor uint64
	for {
		pageCtx, cancel := context.WithTimeout(ctx, redisTimeout)
		page, next, err := r.client.Scan(pageCtx, cursor, pattern, redisScanCount).Result()
		cancel()
		if err != nil {
			return fmt.Errorf("kv redis: scan: %w", err)
		}
		if err := visit(page); err != nil {
			return err
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

// globEscape quotes the characters SCAN's MATCH treats as pattern syntax, so a
// deployment id or namespace containing one matches itself rather than acting as a
// wildcard. Ids are uuids today, but a listing that silently widened its scope
// because a name held a bracket would be a bad way to find that out.
func globEscape(s string) string {
	var b strings.Builder
	for i := range len(s) {
		switch s[i] {
		case '*', '?', '[', ']', '^', '-', '\\':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// parseVersionField reads the version, which Redis hands back as a string.
func parseVersionField(field any) (int64, error) {
	raw, ok := field.(string)
	if !ok {
		return 0, fmt.Errorf("kv redis: version field is %T, want string", field)
	}
	version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("kv redis: version %q is not a number: %w", raw, err)
	}
	return version, nil
}

// stampOf reads the write timestamp. A missing or unreadable one yields the zero
// time rather than an error: it is display metadata, and refusing to list a key
// because its clock field is odd would be the wrong trade.
func stampOf(field any) time.Time {
	raw, ok := field.(string)
	if !ok {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
