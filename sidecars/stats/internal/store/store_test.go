package store

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/juancavallotti/octo/sidecars/stats/internal/rollup"
	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
)

// storeFor returns a Store over a Redis, real or in-process.
//
// In-process by default, and a real server when REDIS_TEST_URL is set. Two
// backends rather than the skip-if-unset pattern logs/internal/fold uses,
// because a test that skips everywhere — including CI — is a test nobody is
// running. What is under test is the transaction shape, the trimming and the
// TTLs, and miniredis executes the same command sequence against the same
// client. Pointing REDIS_TEST_URL at a real server is how that assumption gets
// checked.
func storeFor(t *testing.T, cfg Config) (*Store, *redis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_URL")
	if addr == "" {
		addr = "redis://" + miniredis.RunT(t).Addr()
	}
	opts, err := redis.ParseURL(addr)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	client := redis.NewClient(opts)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	if cfg.DeploymentID == "" {
		cfg.DeploymentID = "dep-1"
	}
	if cfg.PodName == "" {
		cfg.PodName = "octo-dep-1-abc"
	}
	if cfg.RollupInterval == 0 {
		cfg.RollupInterval = time.Hour
	}
	if cfg.Retention == 0 {
		cfg.Retention = 7 * 24 * time.Hour
	}
	if cfg.SampleInterval == 0 {
		cfg.SampleInterval = time.Second
	}
	if cfg.LiveDepth == 0 {
		cfg.LiveDepth = 3600
	}
	if cfg.RollupDepth == 0 {
		cfg.RollupDepth = 168
	}
	return New(client, cfg), client
}

func sample(tMS int64, values ...float64) series.Sample {
	return series.Sample{Gen: 0, TimeMS: tMS, Values: values}
}

func TestWriteSampleRoundTrips(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	if err := s.WriteSample(ctx, sample(1000, 1, 2, 3)); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}

	rows, err := client.LRange(ctx, s.podKey(liveKey), 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("live tier has %d rows, want 1", len(rows))
	}
	var got series.Sample
	if err := json.Unmarshal([]byte(rows[0]), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TimeMS != 1000 || len(got.Values) != 3 || got.Values[2] != 3 {
		t.Errorf("row = %+v, want the sample that was written", got)
	}
}

// Newest first, so a reader that wants recent history takes the head.
func TestLiveTierIsNewestFirst(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	for i := int64(1); i <= 3; i++ {
		if err := s.WriteSample(ctx, sample(i*1000, float64(i))); err != nil {
			t.Fatalf("WriteSample: %v", err)
		}
	}
	rows, _ := client.LRange(ctx, s.podKey(liveKey), 0, -1).Result()
	var head series.Sample
	_ = json.Unmarshal([]byte(rows[0]), &head)
	if head.TimeMS != 3000 {
		t.Errorf("head timestamp = %d, want 3000 (the newest)", head.TimeMS)
	}
}

// The cap is enforced on every write, so the tier cannot grow past its depth
// however long the pod runs.
func TestTiersAreCapped(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{LiveDepth: 5, RollupDepth: 2})

	for i := int64(0); i < 20; i++ {
		if err := s.WriteSample(ctx, sample(i*1000, float64(i))); err != nil {
			t.Fatalf("WriteSample: %v", err)
		}
	}
	if n, _ := client.LLen(ctx, s.podKey(liveKey)).Result(); n != 5 {
		t.Errorf("live tier length = %d, want the cap 5", n)
	}

	for i := int64(0); i < 10; i++ {
		b := &rollup.Bucket{StartMS: i * 3_600_000, EndMS: (i + 1) * 3_600_000, Value: []float64{1}}
		if err := s.WriteBucket(ctx, b); err != nil {
			t.Fatalf("WriteBucket: %v", err)
		}
	}
	if n, _ := client.LLen(ctx, s.podKey(rollupKey)).Result(); n != 2 {
		t.Errorf("rollup tier length = %d, want the cap 2", n)
	}
}

// Everything a pod writes carries a TTL, because pods are ephemeral and nothing
// else cleans up after them.
func TestEveryKeyHasATTL(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	if err := s.WriteMeta(ctx, 0, time.Now()); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}
	if err := s.WriteDictionary(ctx, 0, []series.Entry{
		{Index: 0, Name: "octo_flow_messages_total", Kind: series.KindCounter},
	}); err != nil {
		t.Fatalf("WriteDictionary: %v", err)
	}
	if err := s.WriteSample(ctx, sample(1000, 1)); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}
	if err := s.WriteBucket(ctx, &rollup.Bucket{EndMS: 3_600_000, Value: []float64{1}}); err != nil {
		t.Fatalf("WriteBucket: %v", err)
	}

	for _, key := range []string{
		s.podKey(metaKey), s.podKey(dictKey, "0"),
		s.podKey(liveKey), s.podKey(rollupKey),
		PodsKey(s.cfg.DeploymentID),
	} {
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("TTL %s: %v", key, err)
		}
		if ttl <= 0 {
			t.Errorf("%s has TTL %v, want a positive one", key, ttl)
		}
	}
}

// The requirement the layout exists for: one deployment id finds every pod.
func TestOneDeploymentFindsAllItsPods(t *testing.T) {
	ctx := context.Background()
	first, client := storeFor(t, Config{PodName: "octo-dep-1-aaa"})
	second := New(client, Config{
		DeploymentID: first.cfg.DeploymentID, PodName: "octo-dep-1-bbb",
		RollupInterval: time.Hour, Retention: 7 * 24 * time.Hour, LiveDepth: 10, RollupDepth: 10,
	})
	// A pod of a different deployment, which must not show up.
	other := New(client, Config{
		DeploymentID: "dep-2", PodName: "octo-dep-2-ccc",
		RollupInterval: time.Hour, Retention: 7 * 24 * time.Hour, LiveDepth: 10, RollupDepth: 10,
	})

	for _, s := range []*Store{first, second, other} {
		if err := s.WriteSample(ctx, sample(time.Now().UnixMilli(), 1)); err != nil {
			t.Fatalf("WriteSample: %v", err)
		}
	}

	pods, err := client.ZRange(ctx, PodsKey("dep-1"), 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange: %v", err)
	}
	if len(pods) != 2 {
		t.Fatalf("dep-1 index has %v, want exactly its two pods", pods)
	}
	// And each pod's rows are reachable from its name alone.
	for _, pod := range pods {
		s := New(client, Config{DeploymentID: "dep-1", PodName: pod})
		if n, _ := client.LLen(ctx, s.podKey(liveKey)).Result(); n != 1 {
			t.Errorf("pod %s has %d rows, want 1", pod, n)
		}
	}
}

// The index score is the write time, so a reader can tell a live pod from one
// that stopped reporting without fetching its rows.
func TestPodIndexIsScoredByWriteTime(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	at := time.Now()
	if err := s.WriteSample(ctx, sample(at.UnixMilli(), 1)); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}
	score, err := client.ZScore(ctx, PodsKey(s.cfg.DeploymentID), s.cfg.PodName).Result()
	if err != nil {
		t.Fatalf("ZScore: %v", err)
	}
	if int64(score) != at.UnixMilli() {
		t.Errorf("score = %d, want the write time %d", int64(score), at.UnixMilli())
	}
}

// A pod that has not written for longer than the retention window is dropped
// from the index, so a long-lived deployment does not accumulate an entry for
// every pod it has ever had.
func TestStalePodsLeaveTheIndex(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	now := time.Now()
	ancient := now.Add(-30 * 24 * time.Hour)
	if err := client.ZAdd(ctx, PodsKey(s.cfg.DeploymentID),
		redis.Z{Score: float64(ancient.UnixMilli()), Member: "octo-dep-1-old"}).Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := s.WriteSample(ctx, sample(now.UnixMilli(), 1)); err != nil {
		t.Fatalf("WriteSample: %v", err)
	}

	pods, _ := client.ZRange(ctx, PodsKey(s.cfg.DeploymentID), 0, -1).Result()
	for _, p := range pods {
		if p == "octo-dep-1-old" {
			t.Error("a pod older than the retention window is still in the index")
		}
	}
}

func TestWriteDictionaryRoundTrips(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	entries := []series.Entry{
		{Index: 0, Name: "octo_flow_messages_total",
			Labels: map[string]string{"flow": "orders", "outcome": "ok"}, Kind: series.KindCounter},
		{Index: 1, Name: "octo_flow_in_flight",
			Labels: map[string]string{"flow": "orders"}, Kind: series.KindGauge},
	}
	if err := s.WriteDictionary(ctx, 2, entries); err != nil {
		t.Fatalf("WriteDictionary: %v", err)
	}

	fields, err := client.HGetAll(ctx, s.podKey(dictKey, "2")).Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if len(fields) != 2 {
		t.Fatalf("dictionary has %d entries, want 2", len(fields))
	}
	var got series.Entry
	if err := json.Unmarshal([]byte(fields["0"]), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "octo_flow_messages_total" || got.Kind != series.KindCounter {
		t.Errorf("entry 0 = %+v, want the counter that was written", got)
	}
	if got.Labels["flow"] != "orders" {
		t.Errorf("labels = %v, want flow=orders", got.Labels)
	}
}

// Generations live side by side, so a sample naming an older one stays
// decodable after a config reload grows the dictionary.
func TestDictionaryGenerationsCoexist(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	if err := s.WriteDictionary(ctx, 0, []series.Entry{{Index: 0, Name: "a", Kind: series.KindGauge}}); err != nil {
		t.Fatalf("gen 0: %v", err)
	}
	if err := s.WriteDictionary(ctx, 1, []series.Entry{
		{Index: 0, Name: "a", Kind: series.KindGauge},
		{Index: 1, Name: "b", Kind: series.KindGauge},
	}); err != nil {
		t.Fatalf("gen 1: %v", err)
	}

	if n, _ := client.HLen(ctx, s.podKey(dictKey, "0")).Result(); n != 1 {
		t.Errorf("gen 0 has %d entries, want 1", n)
	}
	if n, _ := client.HLen(ctx, s.podKey(dictKey, "1")).Result(); n != 2 {
		t.Errorf("gen 1 has %d entries, want 2", n)
	}
}

// An unreachable Redis is an ordinary error the caller rides out, not a panic
// and not something that takes the process down.
func TestUnreachableRedisReturnsAnError(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = client.Close() })
	s := New(client, Config{
		DeploymentID: "dep-1", PodName: "pod-1",
		RollupInterval: time.Hour, Retention: time.Hour, LiveDepth: 10, RollupDepth: 10,
	})
	if err := s.WriteSample(context.Background(), sample(1000, 1)); err == nil {
		t.Fatal("expected an error from an unreachable Redis")
	}
}

// A sample carrying a gap has to reach Redis.
//
// This is the regression test for a write path that silently lost data: NaN is
// how series.Encode records a series the dictionary knows but the scrape did
// not report, encoding/json refuses NaN, and push only logs the encode error.
// Because indices are append-only, one series that stops being reported puts a
// NaN in every subsequent sample — so the tier stopped being written for the
// rest of the pod's life, with nothing but a log line to say so.
func TestWriteSampleWithAGap(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	if err := s.WriteSample(ctx, sample(1000, 1, math.NaN(), 3)); err != nil {
		t.Fatalf("WriteSample with a gap: %v", err)
	}

	rows, err := client.LRange(ctx, s.podKey(liveKey), 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("live tier has %d rows, want 1", len(rows))
	}

	var got series.Sample
	if err := json.Unmarshal([]byte(rows[0]), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Values) != 3 {
		t.Fatalf("decoded %d values, want 3", len(got.Values))
	}
	if !math.IsNaN(got.Values[1]) {
		t.Errorf("gap read back as %v, want NaN", got.Values[1])
	}
	if got.Values[0] != 1 || got.Values[2] != 3 {
		t.Errorf("readings either side of the gap = %v, want 1 and 3",
			got.Values)
	}
}

// The same hazard on the history tier: a series no sample in the bucket
// reported collapses to NaN in all four slices.
func TestWriteBucketWithAnUnobservedSeries(t *testing.T) {
	ctx := context.Background()
	s, client := storeFor(t, Config{})

	nan := math.NaN()
	bucket := &rollup.Bucket{
		Gen: 0, StartMS: 0, EndMS: 3600000, Samples: 12,
		Value: series.Values{1, nan},
		Min:   series.Values{1, nan},
		Max:   series.Values{2, nan},
		Last:  series.Values{2, nan},
	}
	if err := s.WriteBucket(ctx, bucket); err != nil {
		t.Fatalf("WriteBucket with an unobserved series: %v", err)
	}

	rows, err := client.LRange(ctx, s.podKey(rollupKey), 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("history tier has %d rows, want 1", len(rows))
	}

	var got rollup.Bucket
	if err := json.Unmarshal([]byte(rows[0]), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for name, column := range map[string]series.Values{
		"v": got.Value, "mn": got.Min, "mx": got.Max, "l": got.Last,
	} {
		if len(column) != 2 {
			t.Errorf("%s has %d entries, want 2", name, len(column))
			continue
		}
		if !math.IsNaN(column[1]) {
			t.Errorf("%s[1] = %v, want NaN", name, column[1])
		}
	}
}
