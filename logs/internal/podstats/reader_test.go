package podstats

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// readerFor returns a Reader over a Redis, real or in-process.
//
// In-process by default, and a real server when REDIS_TEST_URL is set. Two
// backends rather than the skip-if-unset pattern internal/fold uses, and the
// reason is specific to this package: what is under test is the paging — an
// index estimate, a continuation read when it falls short, and a dedupe against
// rows the writer appended in between. None of that can be exercised without a
// server, so a test that skips everywhere would leave the one genuinely subtle
// part of the reader unexercised in CI. The sidecar that writes these keys made
// the same call for the same reason.
//
// The fold's tests stay as they are: what they check is Lua running inside a
// real server, which is exactly what a fake cannot stand in for.
func readerFor(t *testing.T) (*Reader, *redis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_URL")
	if addr == "" {
		addr = "redis://" + miniredis.RunT(t).Addr()
	}
	opts, err := redis.ParseURL(addr)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	// FlushDB is about to delete everything in the selected database, so refuse
	// to run against one that was not asked for. Database 0 is what a bare
	// redis:// URL selects and what a developer's own Redis is almost certainly
	// using; requiring a non-zero index makes pointing REDIS_TEST_URL at
	// something real an explicit act rather than an accident. miniredis is
	// exempt because nothing else is in it.
	if os.Getenv("REDIS_TEST_URL") != "" && opts.DB == 0 {
		t.Fatalf("refusing to flush database 0 of %s: point REDIS_TEST_URL at a "+
			"disposable database, e.g. %s/9", addr, addr)
	}

	client := redis.NewClient(opts)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return NewReader(client), client
}

const testDep = "dep-1"

// writePod lays down a pod the way the sidecar would: a meta hash, a
// dictionary, an index entry and a run of samples one interval apart, newest
// last (so they are LPUSHed in the order the sampler produces them).
func writePod(t *testing.T, client *redis.Client, pod string, gen int, entries []Entry, samples []Sample) {
	t.Helper()
	ctx := context.Background()

	if err := client.HSet(ctx, MetaKey(testDep, pod), map[string]any{
		"gen": gen, "pod": pod, "deployment": testDep,
		"sampleInterval": "1s", "rollupInterval": "1h0m0s", "retention": "168h0m0s",
		"liveDepth": 3600, "rollupDepth": 168,
		"startedAt": "2026-09-05T10:00:00Z",
	}).Err(); err != nil {
		t.Fatalf("write meta: %v", err)
	}

	fields := map[string]any{}
	for _, e := range entries {
		encoded, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("encode entry: %v", err)
		}
		fields[strconv.Itoa(e.Index)] = encoded
	}
	if len(fields) > 0 {
		if err := client.HSet(ctx, DictKey(testDep, pod, gen), fields).Err(); err != nil {
			t.Fatalf("write dict: %v", err)
		}
	}

	for _, s := range samples {
		pushSample(t, client, pod, s)
	}
}

// pushSample LPUSHes one sample and touches the index, as the writer's single
// transaction does.
func pushSample(t *testing.T, client *redis.Client, pod string, s Sample) {
	t.Helper()
	ctx := context.Background()

	encoded, err := marshalSample(s)
	if err != nil {
		t.Fatalf("encode sample: %v", err)
	}
	if err := client.LPush(ctx, LiveKey(testDep, pod), encoded).Err(); err != nil {
		t.Fatalf("lpush: %v", err)
	}
	if err := client.ZAdd(ctx, PodsKey(testDep), redis.Z{
		Score: float64(s.TimeMS), Member: pod,
	}).Err(); err != nil {
		t.Fatalf("zadd: %v", err)
	}
}

// marshalSample writes a sample the way the sidecar does, gaps as null. This
// package only decodes, so the encoder lives here in the test.
func marshalSample(s Sample) ([]byte, error) {
	parts := make([]string, 0, len(s.Values))
	for _, v := range s.Values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			parts = append(parts, "null")
			continue
		}
		parts = append(parts, strconv.FormatFloat(v, 'f', -1, 64))
	}
	out := fmt.Sprintf(`{"g":%d,"t":%d,"v":[`, s.Gen, s.TimeMS)
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return []byte(out + "]}"), nil
}

func gaugeEntry(i int, name string) Entry {
	return Entry{Index: i, Name: name, Kind: KindGauge}
}

// samplesEvery builds n samples one step apart, ending at endMS.
func samplesEvery(n int, endMS, stepMS int64, gen int, value func(i int) float64) []Sample {
	out := make([]Sample, 0, n)
	for i := range n {
		at := endMS - int64(n-1-i)*stepMS
		out = append(out, Sample{Gen: gen, TimeMS: at, Values: Values{value(i)}})
	}
	return out
}

// An unknown deployment is an empty answer, never an error. This service has no
// deployment registry, so it cannot tell one that never existed from one whose
// stats expired — and must not pretend otherwise.
func TestPodsOfAnUnknownDeploymentIsEmpty(t *testing.T) {
	r, _ := readerFor(t)

	pods, truncated, err := r.Pods(context.Background(), "nope", time.Time{})
	if err != nil {
		t.Fatalf("Pods: %v", err)
	}
	if len(pods) != 0 || truncated {
		t.Errorf("pods = %v (truncated %v), want none", pods, truncated)
	}
}

func TestPodsAreNewestFirstAndScoreFiltered(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	base := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	for name, at := range map[string]time.Time{
		"pod-old":    base.Add(-48 * time.Hour),
		"pod-recent": base.Add(-2 * time.Minute),
		"pod-now":    base,
	} {
		if err := client.ZAdd(ctx, PodsKey(testDep), redis.Z{
			Score: float64(at.UnixMilli()), Member: name,
		}).Err(); err != nil {
			t.Fatalf("zadd: %v", err)
		}
	}

	all, _, err := r.Pods(ctx, testDep, time.Time{})
	if err != nil {
		t.Fatalf("Pods: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("listed %d pods, want 3", len(all))
	}
	if all[0].Name != "pod-now" || all[2].Name != "pod-old" {
		t.Errorf("order = %v, want newest first", names(all))
	}
	if got := all[0].LastSeen.UTC(); !got.Equal(base) {
		t.Errorf("lastSeen = %v, want %v", got, base)
	}

	// The index holds eight days of dead pods; a window cannot need one that
	// stopped writing before it began.
	recent, _, err := r.Pods(ctx, testDep, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Pods: %v", err)
	}
	if len(recent) != 2 {
		t.Errorf("filtered to %v, want the two inside the window", names(recent))
	}
}

func TestPodsTruncatesAtTheCap(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	for i := range maxPods + 5 {
		if err := client.ZAdd(ctx, PodsKey(testDep), redis.Z{
			Score: float64(i), Member: fmt.Sprintf("pod-%02d", i),
		}).Err(); err != nil {
			t.Fatalf("zadd: %v", err)
		}
	}

	pods, truncated, err := r.Pods(ctx, testDep, time.Time{})
	if err != nil {
		t.Fatalf("Pods: %v", err)
	}
	if len(pods) != maxPods {
		t.Errorf("listed %d pods, want the cap of %d", len(pods), maxPods)
	}
	if !truncated {
		t.Error("truncated is false although the cap was hit")
	}
}

// The head row is the authority on the generation, because meta lags whenever
// a WriteMeta failed after its dictionary was written.
func TestStatesPrefersTheRowsGenerationOverMeta(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	writePod(t, client, "pod-a", 1, []Entry{gaugeEntry(0, "go_goroutines")},
		[]Sample{{Gen: 4, TimeMS: 1000, Values: Values{7}}})

	states, err := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	state := states["pod-a"]

	if state.Meta.Gen != 1 {
		t.Errorf("meta gen = %d, want 1", state.Meta.Gen)
	}
	if state.Gen != 4 {
		t.Errorf("decode gen = %d, want 4 from the newest row", state.Gen)
	}
	if state.NewestMS != 1000 {
		t.Errorf("anchor = %d, want 1000", state.NewestMS)
	}
	if state.LiveRows != 1 {
		t.Errorf("liveRows = %d, want 1", state.LiveRows)
	}
}

// A pod whose keys have all expired is a normal state, not an error: the live
// TTL is only twice the rollup interval, so every pod that stopped a few hours
// ago looks like this while staying in the index for eight days.
func TestStatesOfAnExpiredPod(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	if err := client.ZAdd(ctx, PodsKey(testDep), redis.Z{
		Score: 1, Member: "pod-gone",
	}).Err(); err != nil {
		t.Fatalf("zadd: %v", err)
	}

	states, err := r.States(ctx, testDep, []PodRef{{Name: "pod-gone"}}, TierLive)
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	state, ok := states["pod-gone"]
	if !ok {
		t.Fatal("the pod was dropped from the result entirely")
	}
	if state.Meta.Present {
		t.Error("Present is true for a pod with no meta hash")
	}
	if state.LiveRows != 0 || state.NewestMS != 0 {
		t.Errorf("state = %+v, want no rows", state)
	}
	// Still usable: the defaults are what bound a read of a pod like this.
	if state.Meta.LiveDepth <= 0 {
		t.Error("no depth to bound a read with")
	}
}

func TestDictionaryDecodes(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	entries := []Entry{
		gaugeEntry(0, "go_goroutines"),
		{Index: 1, Name: "octo_flow_messages_total", Labels: map[string]string{"flow": "a"}, Kind: KindCounter},
	}
	writePod(t, client, "pod-a", 2, entries, []Sample{{Gen: 2, TimeMS: 1000, Values: Values{1, 2}}})

	states, err := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	if err != nil {
		t.Fatalf("States: %v", err)
	}
	dict, err := r.Dictionary(ctx, testDep, "pod-a", states["pod-a"])
	if err != nil {
		t.Fatalf("Dictionary: %v", err)
	}
	if len(dict) != 2 {
		t.Fatalf("decoded %d entries, want 2", len(dict))
	}
	if dict[0].Name != "go_goroutines" || dict[0].Kind != KindGauge {
		t.Errorf("entry 0 = %+v", dict[0])
	}
	if dict[1].Labels["flow"] != "a" || dict[1].Kind != KindCounter {
		t.Errorf("entry 1 = %+v", dict[1])
	}
}

// When the row's generation names a dictionary that is gone, meta's is the
// fallback. Never an older one: an older dictionary is a subset, so decoding a
// newer row against it would mislabel everything past its end.
func TestDictionaryFallsBackToMeta(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	// Dictionary written at generation 1; the rows claim 9.
	writePod(t, client, "pod-a", 1, []Entry{gaugeEntry(0, "go_goroutines")},
		[]Sample{{Gen: 9, TimeMS: 1000, Values: Values{7}}})

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	dict, err := r.Dictionary(ctx, testDep, "pod-a", states["pod-a"])
	if err != nil {
		t.Fatalf("Dictionary: %v", err)
	}
	if len(dict) != 1 || dict[0].Name != "go_goroutines" {
		t.Errorf("dictionary = %v, want the generation meta names", dict)
	}
}

func TestDictionaryMissingEntirely(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	pushSample(t, client, "pod-a", Sample{Gen: 3, TimeMS: 1000, Values: Values{1}})

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	dict, err := r.Dictionary(ctx, testDep, "pod-a", states["pod-a"])
	if err != nil {
		t.Fatalf("Dictionary: %v", err)
	}
	if dict != nil {
		t.Errorf("dictionary = %v, want nil so the caller can warn and skip", dict)
	}
}

// The estimate should read the window and little else.
func TestRowsReadsOnlyTheWindow(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	end := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	endMS := end.UnixMilli()
	writePod(t, client, "pod-a", 0, []Entry{gaugeEntry(0, "g")},
		samplesEvery(600, endMS, 1000, 0, func(i int) float64 { return float64(i) }))

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	rows, err := r.Rows(ctx, testDep, "pod-a", states["pod-a"], Window{
		Tier: TierLive,
		From: end.Add(-10 * time.Second),
		To:   end,
	})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	// Eleven rows span the window inclusive; the estimate's slack may add a
	// few. What matters is that it is nothing like the 600 stored.
	if len(rows) < 11 || len(rows) > 11+estimateSlack+2 {
		t.Errorf("read %d rows for a 10-second window of 1-second samples, "+
			"want about 11", len(rows))
	}

	// Newest first, and reaching back at least to the window's start.
	_, newest, _ := rowHeader(string(rows[0]), TierLive)
	_, oldest, _ := rowHeader(string(rows[len(rows)-1]), TierLive)
	if newest != endMS {
		t.Errorf("newest row at %d, want %d", newest, endMS)
	}
	if oldest > end.Add(-10*time.Second).UnixMilli() {
		t.Errorf("oldest row at %d does not reach the window start", oldest)
	}
}

// A counter's first delta needs the reading before the window, or every
// counter chart would start a point late.
func TestRowsReachesPastTheWindowStart(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	end := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	writePod(t, client, "pod-a", 0, []Entry{gaugeEntry(0, "g")},
		samplesEvery(60, end.UnixMilli(), 1000, 0, func(i int) float64 { return float64(i) }))

	from := end.Add(-5 * time.Second)
	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	rows, err := r.Rows(ctx, testDep, "pod-a", states["pod-a"], Window{
		Tier: TierLive, From: from, To: end,
	})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	_, oldest, _ := rowHeader(string(rows[len(rows)-1]), TierLive)
	if oldest > from.UnixMilli() {
		t.Errorf("oldest row at %d, want one at or before the window start %d",
			oldest, from.UnixMilli())
	}
}

// The continuation read is what makes the estimate safe rather than merely
// quick, so it has to be exercised on a case where the estimate is genuinely
// too small.
//
// Meta claims hourly spacing while the rows are a second apart, which is what a
// pod looks like after its interval was reconfigured and the old rows are still
// in the list. The estimate for a one-hour window is then about ten rows, and
// the 3600 rows it actually needs sit past them. Only continuation finds them.
func TestRowsContinuesWhenTheEstimateFallsShort(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	end := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	endMS := end.UnixMilli()
	const rows = 2000

	writePod(t, client, "pod-a", 0, []Entry{gaugeEntry(0, "g")},
		samplesEvery(rows, endMS, 1000, 0, func(i int) float64 { return float64(i) }))
	if err := client.HSet(ctx, MetaKey(testDep, "pod-a"), "sampleInterval", "1h0m0s").Err(); err != nil {
		t.Fatalf("reset interval: %v", err)
	}

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	state := states["pod-a"]

	from := end.Add(-time.Duration(rows-1) * time.Second)
	window := Window{Tier: TierLive, From: from, To: end}

	// The premise: without continuation this read would stop far short.
	if got := estimate(state, window); got >= rows {
		t.Fatalf("estimate is %d for %d rows, so this test would pass without "+
			"continuation and proves nothing", got, rows)
	}

	got, err := r.Rows(ctx, testDep, "pod-a", state, window)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(got) < rows {
		t.Errorf("read %d rows, want all %d spanning the window", len(got), rows)
	}
	_, oldest, _ := rowHeader(string(got[len(got)-1]), TierLive)
	if oldest > from.UnixMilli() {
		t.Errorf("oldest row at %d does not reach the window start %d",
			oldest, from.UnixMilli())
	}
}

// Times are strictly decreasing down a list, so a row that is not older than
// what is already held is one this pass has seen — which is how a continuation
// read stays correct while the writer keeps LPUSHing underneath it.
func TestRowsAreStrictlyOlderGoingDown(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	end := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	writePod(t, client, "pod-a", 0, []Entry{gaugeEntry(0, "g")},
		samplesEvery(200, end.UnixMilli(), 1000, 0, func(i int) float64 { return float64(i) }))

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	rows, err := r.Rows(ctx, testDep, "pod-a", states["pod-a"], Window{
		Tier: TierLive, From: end.Add(-150 * time.Second), To: end,
	})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}

	prev := int64(math.MaxInt64)
	for i, raw := range rows {
		_, at, ok := rowHeader(string(raw), TierLive)
		if !ok {
			t.Fatalf("row %d has no header", i)
		}
		if at >= prev {
			t.Fatalf("row %d at %d is not older than the one before it (%d)", i, at, prev)
		}
		prev = at
	}
}

func TestRowsOfAnEmptyTier(t *testing.T) {
	r, client := readerFor(t)
	ctx := context.Background()

	if err := client.ZAdd(ctx, PodsKey(testDep), redis.Z{Score: 1, Member: "pod-a"}).Err(); err != nil {
		t.Fatalf("zadd: %v", err)
	}

	states, _ := r.States(ctx, testDep, []PodRef{{Name: "pod-a"}}, TierLive)
	rows, err := r.Rows(ctx, testDep, "pod-a", states["pod-a"], Window{
		Tier: TierLive, From: time.Unix(0, 0), To: time.Now(),
	})
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("read %d rows from an empty tier", len(rows))
	}
}

func names(pods []PodRef) []string {
	out := make([]string, 0, len(pods))
	for _, p := range pods {
		out = append(out, p.Name)
	}
	return out
}
