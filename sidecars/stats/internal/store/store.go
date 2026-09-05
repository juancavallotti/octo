// Package store writes a pod's stats to Redis and owns the key layout.
//
// # Why deployment-first
//
// Every key starts with the deployment id and only then names the pod. The
// question this data exists to answer is "how is this deployment behaving",
// and a deployment is a set of pods that come and go: a rollout replaces all of
// them, an autoscale adds one, a crash loop cycles one. A reader that starts
// from a deployment id must be able to find every pod that ever reported
// without knowing any pod's name in advance, which is what the pods index gives
// it. Pod-first keys would have made the common question a full keyspace scan.
//
// # Layout
//
//	octo:stats:v0:{deployment}:pods              ZSET  pod -> last write (unix ms)
//	octo:stats:v0:{deployment}:{pod}:meta        HASH  the pod's tier configuration
//	octo:stats:v0:{deployment}:{pod}:dict:{gen}  HASH  index -> series identity JSON
//	octo:stats:v0:{deployment}:{pod}:live        LIST  newest-first samples
//	octo:stats:v0:{deployment}:{pod}:rollup      LIST  newest-first collapsed buckets
//
// Reading a deployment is therefore: ZRANGE the pods index, then for each pod
// LRANGE the tier you want and HGETALL the dictionary generation its rows name.
// The ZSET score doubles as a liveness hint, so a reader can ignore pods that
// stopped reporting without having to fetch their rows to find out.
//
// v0 is in the keys because this is a first cut whose shape is expected to
// change once we see how it behaves. A later version writes under v1 and the v0
// keys expire on their own rather than needing a migration.
//
// # Bounds
//
// Both tiers are capped lists, trimmed on every write, and every key carries a
// TTL refreshed as it is written. Pods are ephemeral and nothing else will clean
// up after them, so a key that stops being written has to disappear by itself.
// The TTLs are what make that automatic: a pod deleted an hour ago stops
// occupying the cache without any sweeper needing to know it existed.
//
// This matters more than usual because the Redis being written to is shared. It
// is the same instance the trace-fold pipeline and the volatile KV tier use, at
// 256Mi with allkeys-lru, so stats that grew without bound would not fail — they
// would silently evict someone else's data.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/juancavallotti/octo/sidecars/stats/internal/rollup"
	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
)

const (
	// Layout is the key shape, as a single string, so the one place that
	// documents it is also the thing a contract test can assert. The same device
	// as volatileKeyLayout in runtime/services/k8s/rediskv_contract_test.go: the
	// layout is read by things outside this module, and a silent change to it is
	// a silent break.
	Layout = "octo:stats:v0:{deployment}:{pod}:{tier}"

	// prefix and the segment names Layout is built from.
	prefix     = "octo:stats:v0"
	podsKey    = "pods"
	metaKey    = "meta"
	dictKey    = "dict"
	liveKey    = "live"
	rollupKey  = "rollup"
	keySepChar = ":"

	// writeTimeout bounds one write. Short: a write that has not landed by now
	// has missed its sample, and the next one is a second away. Matching the
	// 5s the runtime's volatile KV tier uses (runtime/services/k8s/rediskv.go).
	writeTimeout = 5 * time.Second

	// liveTTLFactor multiplies the rollup interval to give the live tier's TTL.
	// Two rather than one so a pod whose samples stop exactly on a boundary does
	// not lose the bucket it was in the middle of before anything can read it.
	liveTTLFactor = 2

	// retentionSlack is added to the retention window for the TTL of everything
	// but the live tier. Without it the oldest row expires at the instant it
	// becomes the oldest row, and a reader asking for exactly a week gets
	// whatever survived the race.
	retentionSlack = time.Hour
)

// Config is what the store needs to know about the pod it is writing for.
type Config struct {
	DeploymentID string
	PodName      string

	SampleInterval time.Duration
	RollupInterval time.Duration
	Retention      time.Duration

	// LiveDepth and RollupDepth are the capped lengths of the two tiers.
	LiveDepth   int64
	RollupDepth int64
}

// Store writes one pod's stats.
type Store struct {
	client *redis.Client
	cfg    Config
}

// New returns a Store writing for the pod described by cfg.
func New(client *redis.Client, cfg Config) *Store {
	return &Store{client: client, cfg: cfg}
}

// PodsKey is the deployment's pod index — the entry point for any reader.
func PodsKey(deploymentID string) string {
	return join(prefix, deploymentID, podsKey)
}

// podKey builds a key under this store's pod.
func (s *Store) podKey(parts ...string) string {
	return join(append([]string{prefix, s.cfg.DeploymentID, s.cfg.PodName}, parts...)...)
}

// join assembles a key from its segments.
func join(parts ...string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += keySepChar + p
	}
	return out
}

// liveTTL and rollupTTL are how long each tier's keys survive without a write.
func (s *Store) liveTTL() time.Duration {
	return time.Duration(liveTTLFactor) * s.cfg.RollupInterval
}

func (s *Store) rollupTTL() time.Duration {
	return s.cfg.Retention + retentionSlack
}

// WriteMeta records the pod's tier configuration and marks it present in the
// deployment's index.
//
// Written at startup and again whenever the dictionary generation changes, so a
// reader can always discover which generation the newest rows name without
// parsing one.
func (s *Store) WriteMeta(ctx context.Context, gen int, startedAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, s.podKey(metaKey), map[string]any{
		"gen":            gen,
		"pod":            s.cfg.PodName,
		"deployment":     s.cfg.DeploymentID,
		"sampleInterval": s.cfg.SampleInterval.String(),
		"rollupInterval": s.cfg.RollupInterval.String(),
		"retention":      s.cfg.Retention.String(),
		"liveDepth":      s.cfg.LiveDepth,
		"rollupDepth":    s.cfg.RollupDepth,
		"startedAt":      startedAt.UTC().Format(time.RFC3339),
	})
	pipe.Expire(ctx, s.podKey(metaKey), s.rollupTTL())
	s.touchIndex(ctx, pipe, startedAt)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store: write meta: %w", err)
	}
	return nil
}

// WriteDictionary persists a generation of the series dictionary.
//
// Generations are written whole rather than appended to, because a reader
// holding one generation must be able to decode every index a sample of that
// generation names, and a partially written dictionary cannot. Whole is cheap:
// this happens at startup and on a config reload, not per sample.
func (s *Store) WriteDictionary(ctx context.Context, gen int, entries []series.Entry) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	fields := make(map[string]any, len(entries))
	for _, e := range entries {
		encoded, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("store: encode series %s: %w", e.Name, err)
		}
		fields[strconv.Itoa(e.Index)] = encoded
	}
	if len(fields) == 0 {
		return nil
	}

	key := s.podKey(dictKey, strconv.Itoa(gen))
	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key, fields)
	pipe.Expire(ctx, key, s.rollupTTL())
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store: write dictionary gen %d: %w", gen, err)
	}
	return nil
}

// WriteSample appends one live-tier sample.
func (s *Store) WriteSample(ctx context.Context, sample series.Sample) error {
	return s.push(ctx, s.podKey(liveKey), sample, s.cfg.LiveDepth, s.liveTTL(),
		time.UnixMilli(sample.TimeMS))
}

// WriteBucket appends one collapsed history-tier bucket.
func (s *Store) WriteBucket(ctx context.Context, bucket *rollup.Bucket) error {
	return s.push(ctx, s.podKey(rollupKey), bucket, s.cfg.RollupDepth, s.rollupTTL(),
		time.UnixMilli(bucket.EndMS))
}

// push is the one write shape both tiers use: prepend, trim to depth, refresh
// the TTL, and touch the deployment's pod index — as a single transaction, so a
// reader never sees a list that has been appended to but not yet trimmed.
//
// Newest-first (LPUSH plus a head-anchored LTRIM) rather than oldest-first,
// because trimming the head of a list is O(1) while trimming its tail is not,
// and because a reader almost always wants the most recent rows.
func (s *Store) push(ctx context.Context, key string, row any, depth int64, ttl time.Duration, at time.Time) error {
	encoded, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("store: encode row for %s: %w", key, err)
	}

	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()

	pipe := s.client.TxPipeline()
	pipe.LPush(ctx, key, encoded)
	pipe.LTrim(ctx, key, 0, depth-1)
	pipe.Expire(ctx, key, ttl)
	s.touchIndex(ctx, pipe, at)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("store: write %s: %w", key, err)
	}
	return nil
}

// touchIndex records this pod in its deployment's index, scored by the moment
// of the write. Queued onto an existing pipeline rather than issued on its own,
// so a pod appears in the index in the same transaction as the row that made it
// appear — a reader can never find a pod in the index and no rows behind it.
func (s *Store) touchIndex(ctx context.Context, pipe redis.Pipeliner, at time.Time) {
	key := PodsKey(s.cfg.DeploymentID)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(at.UnixMilli()), Member: s.cfg.PodName})
	// The index outlives any single pod, so it is trimmed by age rather than by
	// TTL alone: a deployment that has been running for months must not carry an
	// entry for every pod it has ever had.
	pipe.ZRemRangeByScore(ctx, key, "-inf",
		strconv.FormatInt(at.Add(-s.rollupTTL()).UnixMilli(), 10))
	pipe.Expire(ctx, key, s.rollupTTL())
}
