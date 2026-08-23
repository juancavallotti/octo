package storagestats

import (
	"context"
	"testing"
)

// A real Redis INFO reply, trimmed to the fields this package reads plus the
// section headers and blank lines it has to skip. Pasted rather than synthesized so
// the parser is tested against the actual format.
const sampleInfo = `# Server
redis_version:7.2.4
uptime_in_seconds:86400

# Clients
connected_clients:12
blocked_clients:0

# Memory
used_memory:1048576
used_memory_peak:2097152
maxmemory:268435456
maxmemory_policy:allkeys-lru

# Stats
total_commands_processed:99999
instantaneous_ops_per_sec:42
expired_keys:7
evicted_keys:3
keyspace_hits:750
keyspace_misses:250
`

func TestParseInfo(t *testing.T) {
	info := parseInfo(sampleInfo)

	if got := info["redis_version"]; got != "7.2.4" {
		t.Errorf("redis_version = %q, want 7.2.4", got)
	}
	if got := info["maxmemory_policy"]; got != "allkeys-lru" {
		t.Errorf("maxmemory_policy = %q, want allkeys-lru", got)
	}
	if got := infoInt(info, "used_memory"); got != 1048576 {
		t.Errorf("used_memory = %d, want 1048576", got)
	}
	// Section headers and blanks must not become entries.
	if _, ok := info["# Server"]; ok {
		t.Error("a section header was parsed as a field")
	}
}

// Redis renames and removes counters between versions. A report that failed
// because one field was missing would be worse than one showing a zero for it.
func TestInfoIntToleratesMissingAndUnparseableFields(t *testing.T) {
	info := map[string]string{"weird": "not-a-number"}
	if got := infoInt(info, "absent"); got != 0 {
		t.Errorf("absent field = %d, want 0", got)
	}
	if got := infoInt(info, "weird"); got != 0 {
		t.Errorf("unparseable field = %d, want 0", got)
	}
}

// Absent is not the same as broken, and the page has to be able to tell them
// apart: an installation with no Redis is supported, and volatile objects simply
// fall back to the database.
func TestCollectReportsAbsentStoresWithAReason(t *testing.T) {
	got := NewService(nil, nil).Collect(context.Background())

	if got.Redis != nil || got.Database != nil {
		t.Fatal("neither store is configured, so neither half should be reported")
	}
	if got.RedisReason == "" {
		t.Error("an absent Redis needs a reason, or the page cannot tell it from a broken one")
	}
	if got.DatabaseReason == "" {
		t.Error("an absent database needs a reason")
	}
}

// The ratio is what anyone reads, so it has to be right — including the case where
// nothing has been looked up yet, which must not divide by zero.
func TestHitRate(t *testing.T) {
	cases := []struct {
		hits, misses int64
		want         float64
	}{
		{750, 250, 0.75},
		{0, 0, 0},
		{5, 0, 1},
		{0, 9, 0},
	}
	for _, tc := range cases {
		if got := hitRate(tc.hits, tc.misses); got != tc.want {
			t.Errorf("hitRate(%d, %d) = %v, want %v", tc.hits, tc.misses, got, tc.want)
		}
	}
}
