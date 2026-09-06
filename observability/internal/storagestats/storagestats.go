// Package storagestats reports how full the platform's two stores are.
//
// It is the deeper half of what the orchestrator's health report deliberately
// refuses to answer. That one reports one thing per dependency — did it answer —
// and says in its own doc comment that a Redis answering a ping tells you nothing
// about how full it is. This is where that question gets asked: memory against
// the ceiling, the hit rate, what has been evicted, how much of the connection
// pool is in use, how large the KV table has grown.
//
// It lives in this service because this service holds both stores and is the
// heaviest writer to one of them: every log and trace record lands through the
// pool reported here, so a pool with no connection to spare is the shape of
// telemetry backing up. The KV table is the orchestrator's, but its size is a
// question about the database rather than about who writes to it.
//
// Both halves are optional and reported independently. An installation with no
// Redis is a supported one (volatile objects fall back to the database), and this
// service serves /healthz without a database while Postgres comes up, so each side
// reports "not configured" with a reason rather than failing the request. A page
// that could not distinguish "absent" from "broken" would be worse than no page,
// because it would be believed.
package storagestats

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// probeTimeout bounds one store's collection. Short, like the health page's: this
// is something an operator reloads while something is wrong, and a page that takes
// its time to fail feels like another broken thing.
const probeTimeout = 5 * time.Second

// Stats is the whole report. Either half may be absent; see the package comment.
type Stats struct {
	Redis    *RedisStats    `json:"redis"`
	Database *DatabaseStats `json:"database"`
	// RedisReason and DatabaseReason say why a half is absent, so the page can tell
	// "this installation has no Redis" from "Redis is down".
	RedisReason    string `json:"redisReason,omitempty"`
	DatabaseReason string `json:"databaseReason,omitempty"`
}

// RedisStats is what INFO and DBSIZE report, named for what an operator is
// actually asking rather than for Redis's own field names.
type RedisStats struct {
	Version         string `json:"version"`
	UptimeSeconds   int64  `json:"uptimeSeconds"`
	ConnectedClient int64  `json:"connectedClients"`
	BlockedClients  int64  `json:"blockedClients"`
	UsedMemory      int64  `json:"usedMemoryBytes"`
	PeakMemory      int64  `json:"peakMemoryBytes"`
	// MaxMemory 0 means no ceiling is configured, which is worth showing as such
	// rather than as a usage of 100% or of 0%.
	MaxMemory       int64  `json:"maxMemoryBytes"`
	MaxMemoryPolicy string `json:"maxMemoryPolicy"`
	KeyCount        int64  `json:"keyCount"`
	// Hits and Misses are cumulative since the server started. The ratio is what
	// anyone actually reads, but both are reported because a high ratio over a tiny
	// sample says nothing.
	Hits          int64   `json:"keyspaceHits"`
	Misses        int64   `json:"keyspaceMisses"`
	HitRate       float64 `json:"hitRate"`
	EvictedKeys   int64   `json:"evictedKeys"`
	ExpiredKeys   int64   `json:"expiredKeys"`
	TotalCommands int64   `json:"totalCommands"`
	OpsPerSecond  int64   `json:"opsPerSecond"`
}

// DatabaseStats pairs this service's connection pool accounting with two sizes
// read from Postgres. The pool numbers are free — pgxpool already tracks them —
// and they describe the pool the telemetry consumers insert through, which is
// what explains stored logs and traces arriving late without anything being down.
type DatabaseStats struct {
	TotalConns    int32 `json:"totalConns"`
	AcquiredConns int32 `json:"acquiredConns"`
	IdleConns     int32 `json:"idleConns"`
	MaxConns      int32 `json:"maxConns"`
	// EmptyAcquireCount is how often a caller had to wait for a connection, which
	// is the number that turns "the platform feels slow" into "the pool is too
	// small".
	EmptyAcquireCount int64 `json:"emptyAcquireCount"`
	DatabaseBytes     int64 `json:"databaseBytes"`
	KVTableBytes      int64 `json:"kvTableBytes"`
	KVRowCount        int64 `json:"kvRowCount"`
}

// Service collects the report. Either dependency may be nil.
type Service struct {
	redis *redis.Client
	pool  *pgxpool.Pool
}

// NewService returns a service over whichever stores this installation has.
func NewService(client *redis.Client, pool *pgxpool.Pool) *Service {
	return &Service{redis: client, pool: pool}
}

// Collect gathers both halves. It never fails as a whole: a store that is absent
// or unreachable becomes a nil half plus a reason, because the half that did answer
// is still worth showing.
func (s *Service) Collect(ctx context.Context) Stats {
	var out Stats
	out.Redis, out.RedisReason = s.collectRedis(ctx)
	out.Database, out.DatabaseReason = s.collectDatabase(ctx)
	return out
}

// collectRedis reads INFO and DBSIZE.
//
// INFO with no argument, not INFO with a list of sections: multiple section
// arguments only arrived in Redis 7.0, and against an older server that call is an
// error — which this package would then report as "Redis is not reachable", the
// exact confusion between absent, broken and merely old that its doc comment says
// to avoid. The default section set already contains every field read below, and it
// leaves out the expensive ones (commandstats, latencystats) that INFO ALL would
// pull in.
//
// DBSIZE is asked for separately because INFO reports the key count only per
// logical database, in a shape that is more work to read than to ask for.
func (s *Service) collectRedis(ctx context.Context) (*RedisStats, string) {
	if s.redis == nil {
		return nil, "no REDIS_URL is configured for this installation"
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	raw, err := s.redis.Info(ctx).Result()
	if err != nil {
		return nil, "redis is not reachable: " + err.Error()
	}
	info := parseInfo(raw)

	out := &RedisStats{
		Version:         info["redis_version"],
		UptimeSeconds:   infoInt(info, "uptime_in_seconds"),
		ConnectedClient: infoInt(info, "connected_clients"),
		BlockedClients:  infoInt(info, "blocked_clients"),
		UsedMemory:      infoInt(info, "used_memory"),
		PeakMemory:      infoInt(info, "used_memory_peak"),
		MaxMemory:       infoInt(info, "maxmemory"),
		MaxMemoryPolicy: info["maxmemory_policy"],
		Hits:            infoInt(info, "keyspace_hits"),
		Misses:          infoInt(info, "keyspace_misses"),
		EvictedKeys:     infoInt(info, "evicted_keys"),
		ExpiredKeys:     infoInt(info, "expired_keys"),
		TotalCommands:   infoInt(info, "total_commands_processed"),
		OpsPerSecond:    infoInt(info, "instantaneous_ops_per_sec"),
	}
	out.HitRate = hitRate(out.Hits, out.Misses)
	// A DBSIZE that fails is not worth losing the rest of the report over.
	if size, sizeErr := s.redis.DBSize(ctx).Result(); sizeErr == nil {
		out.KeyCount = size
	}
	return out, ""
}

// collectDatabase reads the pool's own counters and two sizes from Postgres.
func (s *Service) collectDatabase(ctx context.Context) (*DatabaseStats, string) {
	if s.pool == nil {
		return nil, "this service is running without a database"
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	stat := s.pool.Stat()
	out := &DatabaseStats{
		TotalConns:        stat.TotalConns(),
		AcquiredConns:     stat.AcquiredConns(),
		IdleConns:         stat.IdleConns(),
		MaxConns:          stat.MaxConns(),
		EmptyAcquireCount: stat.EmptyAcquireCount(),
	}

	// One round trip for all three numbers. pg_total_relation_size includes the
	// indexes, which is what "how much room is this costing" means.
	row := s.pool.QueryRow(ctx, `
		SELECT pg_database_size(current_database()),
		       pg_total_relation_size('kv_store'),
		       (SELECT count(*) FROM kv_store)`)
	if err := row.Scan(&out.DatabaseBytes, &out.KVTableBytes, &out.KVRowCount); err != nil {
		// The pool numbers are already in hand and are the half that explains a slow
		// platform, so report them and say why the sizes are missing rather than
		// dropping everything.
		return out, "sizes are unavailable: " + err.Error()
	}
	return out, ""
}

// hitRate is hits over lookups. A server that has answered nothing yet reports 0
// rather than dividing by zero — and the page shows the counts beside it, because
// a rate over a tiny sample says nothing either way.
func hitRate(hits, misses int64) float64 {
	total := hits + misses
	if total <= 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// parseInfo turns Redis's INFO reply into a map. The format is one "key:value" per
// line, with "# Section" headers and blank lines between sections — small enough to
// read here and not worth a dependency.
func parseInfo(raw string) map[string]string {
	out := make(map[string]string)
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		out[key] = value
	}
	return out
}

// infoInt reads a numeric INFO field, treating an absent or unreadable one as zero.
// Redis's field set varies by version, and a report that failed because one counter
// was renamed would be worse than one showing a zero.
func infoInt(info map[string]string, key string) int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(info[key]), 10, 64)
	if err != nil {
		return 0
	}
	return value
}
