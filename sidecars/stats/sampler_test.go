package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/juancavallotti/octo/sidecars/stats/internal/series"
	"github.com/juancavallotti/octo/sidecars/stats/internal/store"
)

// runtimeStub is a stand-in for the runtime container's admin port. It serves a
// counter that grows on every scrape, so a test can tell samples apart.
type runtimeStub struct {
	*httptest.Server
	scrapes atomic.Int64
}

func newRuntimeStub(t *testing.T) *runtimeStub {
	t.Helper()
	stub := &runtimeStub{}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := stub.scrapes.Add(1)
		_, _ = fmt.Fprintf(w, `# TYPE octo_flow_messages_total counter
octo_flow_messages_total{flow="orders",outcome="ok"} %d
# TYPE octo_flow_in_flight gauge
octo_flow_in_flight{flow="orders"} 2
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 3.2768e+07
`, n*10)
	}))
	t.Cleanup(stub.Close)
	return stub
}

// addr is the host:port the scraper is configured with.
func (r *runtimeStub) addr() string { return strings.TrimPrefix(r.URL, "http://") }

// samplerFor wires a sampler against a stub runtime and an in-process Redis.
func samplerFor(t *testing.T, stub *runtimeStub, cfg config) (*sampler, *redis.Client) {
	t.Helper()

	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg.runtimeAdmin = stub.addr()
	cfg.deploymentID = "dep-1"
	cfg.podName = "octo-dep-1-abc"
	if cfg.sample == 0 {
		cfg.sample = 10 * time.Millisecond
	}
	if cfg.rollup == 0 {
		cfg.rollup = time.Hour
	}
	if cfg.retention == 0 {
		cfg.retention = 7 * 24 * time.Hour
	}

	return newSampler(cfg, store.New(client, store.Config{
		DeploymentID: cfg.deploymentID, PodName: cfg.podName,
		SampleInterval: cfg.sample, RollupInterval: cfg.rollup, Retention: cfg.retention,
		LiveDepth: cfg.liveDepth(), RollupDepth: cfg.rollupDepth(),
	})), client
}

// liveRows reads the pod's live tier back.
func liveRows(t *testing.T, client *redis.Client) []series.Sample {
	t.Helper()
	raw, err := client.LRange(context.Background(),
		"octo:stats:v0:dep-1:octo-dep-1-abc:live", 0, -1).Result()
	if err != nil {
		t.Fatalf("LRange: %v", err)
	}
	out := make([]series.Sample, 0, len(raw))
	for _, r := range raw {
		var s series.Sample
		if err := json.Unmarshal([]byte(r), &s); err != nil {
			t.Fatalf("decode sample: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// The whole pipeline: scrape, encode, store, and be readable from the
// deployment id alone.
func TestSamplerStoresSamplesReadableFromTheDeployment(t *testing.T) {
	stub := newRuntimeStub(t)
	s, client := samplerFor(t, stub, config{})

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	waitFor(t, func() bool { return len(liveRows(t, client)) >= 3 })
	cancel()

	// A reader starts from the deployment and finds the pod.
	pods, err := client.ZRange(context.Background(), store.PodsKey("dep-1"), 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange: %v", err)
	}
	if len(pods) != 1 || pods[0] != "octo-dep-1-abc" {
		t.Fatalf("pod index = %v, want [octo-dep-1-abc]", pods)
	}

	// The dictionary the rows name is there, and describes the series.
	rows := liveRows(t, client)
	gen := rows[0].Gen
	dict, err := client.HGetAll(context.Background(),
		fmt.Sprintf("octo:stats:v0:dep-1:octo-dep-1-abc:dict:%d", gen)).Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if len(dict) != len(rows[0].Values) {
		t.Errorf("dictionary has %d entries but a sample has %d values",
			len(dict), len(rows[0].Values))
	}

	names := map[string]series.Kind{}
	for _, raw := range dict {
		var e series.Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			t.Fatalf("decode entry: %v", err)
		}
		names[e.Name] = e.Kind
	}
	for name, want := range map[string]series.Kind{
		"octo_flow_messages_total":      series.KindCounter,
		"octo_flow_in_flight":           series.KindGauge,
		"process_resident_memory_bytes": series.KindGauge,
	} {
		if got, ok := names[name]; !ok {
			t.Errorf("%s missing from the dictionary", name)
		} else if got != want {
			t.Errorf("%s kind = %q, want %q", name, got, want)
		}
	}
}

// Meta names the generation the newest rows use, so a reader finds the right
// dictionary without parsing a row first.
func TestSamplerWritesMeta(t *testing.T) {
	stub := newRuntimeStub(t)
	s, client := samplerFor(t, stub, config{})

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	waitFor(t, func() bool { return len(liveRows(t, client)) >= 1 })
	cancel()

	meta, err := client.HGetAll(context.Background(),
		"octo:stats:v0:dep-1:octo-dep-1-abc:meta").Result()
	if err != nil {
		t.Fatalf("HGetAll: %v", err)
	}
	if meta["pod"] != "octo-dep-1-abc" || meta["deployment"] != "dep-1" {
		t.Errorf("meta identity = %v, want the pod and deployment", meta)
	}
	if meta["rollupInterval"] != "1h0m0s" {
		t.Errorf("meta rollupInterval = %q, want the configured 1h", meta["rollupInterval"])
	}
	if meta["gen"] != fmt.Sprint(liveRows(t, client)[0].Gen) {
		t.Errorf("meta gen = %q, does not match the newest row's generation", meta["gen"])
	}
}

// A completed bucket lands in the history tier.
func TestSamplerClosesBuckets(t *testing.T) {
	stub := newRuntimeStub(t)
	// A rollup interval barely longer than the sample interval, so buckets close
	// within the test rather than in an hour.
	s, client := samplerFor(t, stub, config{
		sample: 10 * time.Millisecond, rollup: 100 * time.Millisecond, retention: time.Hour,
	})

	ctx, cancel := context.WithCancel(context.Background())
	go s.Run(ctx)
	waitFor(t, func() bool {
		n, _ := client.LLen(context.Background(),
			"octo:stats:v0:dep-1:octo-dep-1-abc:rollup").Result()
		return n >= 1
	})
	cancel()

	raw, err := client.LRange(context.Background(),
		"octo:stats:v0:dep-1:octo-dep-1-abc:rollup", 0, 0).Result()
	if err != nil || len(raw) == 0 {
		t.Fatalf("LRange rollup: %v (%d rows)", err, len(raw))
	}
	var bucket struct {
		StartMS int64     `json:"t"`
		EndMS   int64     `json:"e"`
		Samples int       `json:"n"`
		Value   []float64 `json:"v"`
	}
	if err := json.Unmarshal([]byte(raw[0]), &bucket); err != nil {
		t.Fatalf("decode bucket: %v", err)
	}
	if bucket.Samples < 1 {
		t.Errorf("bucket holds %d samples, want at least one", bucket.Samples)
	}
	if bucket.EndMS-bucket.StartMS != 100 {
		t.Errorf("bucket span = %dms, want the 100ms rollup interval", bucket.EndMS-bucket.StartMS)
	}
	// Epoch-aligned, so pods of one deployment share a grid.
	if bucket.StartMS%100 != 0 {
		t.Errorf("bucket start %d is not aligned to the interval", bucket.StartMS)
	}
}

// The bucket in progress is written on the way out, which is the reason this
// runs as a native sidecar.
func TestSamplerFlushesOnShutdown(t *testing.T) {
	stub := newRuntimeStub(t)
	s, client := samplerFor(t, stub, config{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	waitFor(t, func() bool { return len(liveRows(t, client)) >= 2 })

	key := "octo:stats:v0:dep-1:octo-dep-1-abc:rollup"
	if n, _ := client.LLen(context.Background(), key).Result(); n != 0 {
		t.Fatalf("history tier has %d rows before shutdown, want 0", n)
	}

	cancel()
	<-done

	if n, _ := client.LLen(context.Background(), key).Result(); n != 1 {
		t.Errorf("history tier has %d rows after shutdown, want the flushed bucket", n)
	}
}

// An unreachable Redis loses data and keeps going. It must not stop the loop,
// because a cache outage that restarted every production pod would be far worse
// than a gap in the statistics.
func TestSamplerSurvivesRedisOutage(t *testing.T) {
	stub := newRuntimeStub(t)
	// Port 1 on loopback: reserved, nothing binds it. Opened the way main does,
	// so the test exercises the retry budget the sidecar actually runs with.
	client, err := openRedis("redis://127.0.0.1:1")
	if err != nil {
		t.Fatalf("openRedis: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	cfg := config{
		runtimeAdmin: stub.addr(), deploymentID: "dep-1", podName: "pod-1",
		sample: 10 * time.Millisecond, rollup: time.Hour, retention: time.Hour * 2,
	}
	s := newSampler(cfg, store.New(client, store.Config{
		DeploymentID: "dep-1", PodName: "pod-1",
		RollupInterval: cfg.rollup, Retention: cfg.retention, LiveDepth: 10, RollupDepth: 10,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	waitFor(t, func() bool { return s.Report().WriteErrors >= 2 })

	// Still scraping and still failing to write, several attempts in: the loop
	// did not wedge on the first outage and did not give up on the second.
	before := s.Report()
	waitFor(t, func() bool { return s.Report().Scrapes > before.Scrapes })
	cancel()
	<-done

	got := s.Report()
	// Scraping keeps working, so the moment Redis returns there is data to write.
	if got.Scrapes < 2 {
		t.Errorf("scrapes = %d, want the loop to have kept scraping", got.Scrapes)
	}
	if got.LastWriteError == "" {
		t.Error("expected the write failure to be reported on /status")
	}
}

// A runtime without --metrics is reported rather than retried into the ground.
func TestSamplerHandlesMetricsDisabled(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(http.NotFound))
	t.Cleanup(notFound.Close)

	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := config{
		runtimeAdmin: strings.TrimPrefix(notFound.URL, "http://"),
		deploymentID: "dep-1", podName: "pod-1",
		sample: 10 * time.Millisecond, rollup: time.Hour, retention: 2 * time.Hour,
	}
	s := newSampler(cfg, store.New(client, store.Config{
		DeploymentID: "dep-1", PodName: "pod-1",
		RollupInterval: cfg.rollup, Retention: cfg.retention, LiveDepth: 10, RollupDepth: 10,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	waitFor(t, func() bool { return s.Report().ScrapeErrors >= 2 })
	cancel()
	<-done

	if !strings.Contains(s.Report().LastScrapeError, "not serving metrics") {
		t.Errorf("last scrape error = %q, want it to name the disabled endpoint",
			s.Report().LastScrapeError)
	}
	if n, _ := client.LLen(context.Background(), "octo:stats:v0:dep-1:pod-1:live").Result(); n != 0 {
		t.Errorf("live tier has %d rows, want none when nothing could be scraped", n)
	}
}

// waitFor polls until cond holds or the test's patience runs out.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	const (
		limit = 5 * time.Second
		poll  = 5 * time.Millisecond
	)
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(poll)
	}
	t.Fatalf("condition not met within %v", limit)
}
