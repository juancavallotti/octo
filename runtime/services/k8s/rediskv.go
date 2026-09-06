package k8s

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/redis/go-redis/v9"
)

// The volatile half of the cluster KV store, spoken directly to Redis.
//
// Runtime pods reach Redis the way they reach NATS: their own connection, from
// REDIS_URL, with no orchestrator in the path. The persistent half still goes
// through the orchestrator, because that is where the database and the encryption
// key are; the volatile half has neither, so a hop through another service would
// buy nothing and cost a round trip on the one tier whose whole point is being
// cheap.
//
// That makes the layout below a contract between two Go modules — this one and the
// orchestrator's internal/kv, which serves the same keys to the object browser and
// deletes them on undeploy. The two copies must be changed together. It is the same
// hand-synced arrangement as redisx and the trace subject name; see the note on
// redisStore.keyOf for the shape.

// redisPrefix namespaces octo's keys inside a Redis that other things may share
// (the observability service folds trace runs in the same instance).
const redisPrefix = "octo:kv"

// The hash fields one object is stored in. Short names because they are repeated
// once per key and never read by a person.
const (
	fieldValue   = "v"
	fieldVersion = "ver"
	fieldStamp   = "ts" // unix ms; Redis has no per-key mtime and the browser wants one
)

// conflictSentinel is what the scripts return instead of a new version when the
// caller's expected version does not match. Versions are always positive, so a
// negative number cannot be mistaken for one.
const conflictSentinel = -1

// redisTimeout bounds a single operation. Short, because this tier is on the hot
// path of a flow: a Redis that is not answering promptly should fail the read and
// let the caller carry on, not hold a message.
const redisTimeout = 5 * time.Second

// writeScript is the compare-and-swap. It has to be a script because the version
// check and the write must not be separable: between an HGET and an HSET another
// replica can land its own write, and both would think they had won.
//
// KEYS: 1 the object's hash. ARGV: 1 value, 2 expected version, 3 now (unix ms).
// Returns the new version, or conflictSentinel.
const writeScript = `
local current = tonumber(redis.call('HGET', KEYS[1], 'ver')) or 0
if current ~= tonumber(ARGV[2]) then return -1 end
local next = current + 1
redis.call('HSET', KEYS[1], 'v', ARGV[1], 'ver', next, 'ts', ARGV[3])
return next
`

// deleteScript removes an object, honoring the expected version. Expected 0 deletes
// unconditionally, and deleting an absent key is a no-op — the same semantics as
// core.KV.Delete, so a caller cannot tell the backends apart.
//
// KEYS: 1 the object's hash. ARGV: 1 expected version.
// Returns 1 when it deleted, 0 when the key was absent, conflictSentinel on a
// version mismatch.
const deleteScript = `
local current = tonumber(redis.call('HGET', KEYS[1], 'ver'))
if current == nil then return 0 end
local expected = tonumber(ARGV[1])
if expected ~= 0 and expected ~= current then return -1 end
redis.call('DEL', KEYS[1])
return 1
`

// redisStore is the volatile KV tier. It implements core.KV, so the tiered store
// can hand it a call without either side knowing which backend it reached.
type redisStore struct {
	client       *redis.Client
	deploymentID string
	write        *redis.Script
	del          *redis.Script
}

// newRedisStore builds a store over url. The client is lazy — go-redis dials on the
// first command and reconnects on its own — so a Redis that is still starting does
// not stop the runtime from starting, and a Redis that comes back later needs
// nothing rebuilt.
func newRedisStore(url, deploymentID string) (*redisStore, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		// The URL may carry a password and go-redis echoes its input in parse
		// errors, so say only that it did not parse.
		return nil, errors.New("k8s: REDIS_URL is not a valid redis:// url")
	}
	return &redisStore{
		client:       redis.NewClient(opts),
		deploymentID: deploymentID,
		write:        redis.NewScript(writeScript),
		del:          redis.NewScript(deleteScript),
	}, nil
}

// keyOf builds the Redis key for one object: "octo:kv:{deployment}:{namespace}:{key}".
//
// The deployment id comes first so a SCAN can sweep exactly one deployment, which
// is what undeploy cleanup needs. The namespace comes next so the browser can list
// one namespace without reading the rest. The object key is last and takes whatever
// is left, so it may contain colons; a namespace may not, which is what lets the
// orchestrator split a scanned key back apart at its first colon.
//
// Change this and you must change the orchestrator's copy in the same commit;
// rediskv_contract_test.go compares the two and fails when they disagree.
const volatileKeyLayout = "octo:kv:{deployment}:{namespace}:{key}"

func (s *redisStore) keyOf(namespace, key string) string {
	return redisPrefix + ":" + s.deploymentID + ":" + namespace + ":" + key
}

// Get returns the entry for key. A key Redis evicted or never had is a miss, which
// is exactly what the volatile tier promises and nothing more.
func (s *redisStore) Get(ctx context.Context, namespace, key string) (core.Entry, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	fields, err := s.client.HMGet(ctx, s.keyOf(namespace, key), fieldValue, fieldVersion).Result()
	if err != nil {
		return core.Entry{}, false, fmt.Errorf("redis kv get %q: %w", key, err)
	}
	if len(fields) != 2 || fields[0] == nil || fields[1] == nil {
		return core.Entry{}, false, nil
	}
	value, ok := fields[0].(string)
	if !ok {
		return core.Entry{}, false, fmt.Errorf("redis kv get %q: value is %T, want string", key, fields[0])
	}
	version, err := parseRedisVersion(fields[1])
	if err != nil {
		return core.Entry{}, false, fmt.Errorf("redis kv get %q: %w", key, err)
	}
	return core.Entry{Value: []byte(value), Version: version}, true, nil
}

// Set stores value under key with the usual optimistic concurrency and returns the
// new version.
func (s *redisStore) Set(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	res, err := s.write.Run(ctx, s.client,
		[]string{s.keyOf(namespace, key)},
		value, expectedVersion, time.Now().UnixMilli(),
	).Int64()
	if err != nil {
		return 0, fmt.Errorf("redis kv set %q: %w", key, err)
	}
	if res == conflictSentinel {
		return 0, core.ErrVersionConflict
	}
	return res, nil
}

// Delete removes key. expectedVersion 0 deletes unconditionally; a positive value
// must match. Deleting an absent key is a no-op.
func (s *redisStore) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	ctx, cancel := context.WithTimeout(ctx, redisTimeout)
	defer cancel()

	res, err := s.del.Run(ctx, s.client, []string{s.keyOf(namespace, key)}, expectedVersion).Int64()
	if err != nil {
		return fmt.Errorf("redis kv delete %q: %w", key, err)
	}
	if res == conflictSentinel {
		return core.ErrVersionConflict
	}
	return nil
}

func (s *redisStore) close() error {
	if err := s.client.Close(); err != nil {
		return fmt.Errorf("redis kv close: %w", err)
	}
	return nil
}

// parseRedisVersion reads the version field, which Redis hands back as a string.
// ParseInt rather than Sscanf: Sscanf would happily read "12" out of "12abc" and
// carry on with a version the store never wrote.
func parseRedisVersion(field any) (int64, error) {
	raw, ok := field.(string)
	if !ok {
		return 0, fmt.Errorf("version field is %T, want string", field)
	}
	version, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("version %q is not a number: %w", raw, err)
	}
	return version, nil
}
