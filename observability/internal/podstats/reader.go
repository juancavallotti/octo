package podstats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// The bounded read.
//
// A naive LRANGE key 0 -1 pulls 3600 rows of ~95 floats per pod even when the
// caller wants one series over the last minute, on a Redis shared with the
// trace folds and the volatile KV tier. Four things bound it, and only the
// first three are interesting:
//
//  1. Tier selection, which is free. liveDepth × sampleInterval is the entire
//     reach of the live tier — an hour at the defaults — so a window older than
//     that cannot be answered from live rows no matter how many are read. That
//     is decided before a row is fetched.
//
//  2. A score-filtered pod set. The pods index retains members for retention
//     plus an hour, eight days at the defaults, so a deployment that rolls a
//     few times a day carries dozens of dead pod names against one or two live
//     ones. A pod whose last write precedes the window cannot hold rows inside
//     it, which makes ZRANGEBYSCORE an exact filter rather than a heuristic.
//
//  3. An index estimate whose error is one-sided. Rows are newest-first and
//     spacing is at least the sample interval, because the sampler is a ticker
//     and gaps only widen it. So (t0 − from) / interval is an upper bound on
//     how many rows are needed: a gap means fewer, never more. The estimate
//     over-fetches and cannot under-fetch — which is what makes it safe, and
//     why it is still never used as a stopping condition. If the oldest row
//     read is still inside the window, a continuation read follows.
//
// What none of this bounds is the metric filter. Values are positional, so
// reading two of ninety-five series still transfers every byte of every row in
// the window. Nothing in Redis changes that, and it is not worth trying: the
// win is in the window and the pod set, and the filter's value is that it lets
// the rows be projected as they are decoded rather than retained whole.
//
// No Lua. The fold's scripts exist for atomicity across keys they were not
// passed; a read has nothing to make atomic. Decoding thousands of rows inside
// Redis's single thread would trade this service's idle CPU for a stall on the
// one instance the fold pipeline depends on.

// Nothing here sets its own deadline. A read rides the caller's context, as
// fold's store does, so one bound covers every round trip of a query rather
// than each stage getting its own and the total being unbounded.
const (
	// estimateSlack is added to a computed index range. Spacing can only fall
	// below the sample interval through ticker catch-up or a clock adjustment,
	// so a handful of rows covers it and the verify pass covers the rest.
	estimateSlack = 8

	// continuationChunk is how many rows a verify pass asks for when the
	// estimate fell short.
	continuationChunk = 512

	// maxPods caps the fan-out. A deployment with more pods than this reporting
	// into one window is answerable, but not in one response.
	maxPods = 32
)

// Reader fetches raw rows. It owns the Redis access and nothing else: what it
// returns is still dictionary-encoded, because deciding what a row means is
// query.go's job and testing that should not need a server.
type Reader struct {
	client *redis.Client
}

// NewReader returns a Reader over client.
func NewReader(client *redis.Client) *Reader {
	return &Reader{client: client}
}

// PodRef is a pod in a deployment's index, with the time of its last write.
type PodRef struct {
	Name     string
	LastSeen time.Time
}

// Pods lists the pods of a deployment that wrote at or after since, most
// recent first, capped at maxPods.
//
// A zero since lists everything the index still holds. The bool reports
// whether the cap truncated the list.
//
// An unknown deployment is an empty list and no error. This service has no
// deployment registry — that is the orchestrator's — so it cannot tell a
// deployment that never existed from one whose stats have expired or whose
// sidecar is switched off, and answering as though it could would be a lie.
func (r *Reader) Pods(ctx context.Context, deploymentID string, since time.Time) ([]PodRef, bool, error) {
	min := "-inf"
	if !since.IsZero() {
		min = strconv.FormatInt(since.UnixMilli(), 10)
	}

	// Descending, so the cap keeps the pods most likely to be wanted.
	rows, err := r.client.ZRevRangeByScoreWithScores(ctx, PodsKey(deploymentID), &redis.ZRangeBy{
		Min: min,
		Max: "+inf",
		// One past the cap, to learn whether there were more.
		Count: maxPods + 1,
	}).Result()
	if err != nil {
		return nil, false, fmt.Errorf("podstats: list pods of %s: %w", deploymentID, err)
	}

	truncated := len(rows) > maxPods
	if truncated {
		rows = rows[:maxPods]
	}

	pods := make([]PodRef, 0, len(rows))
	for _, row := range rows {
		name, ok := row.Member.(string)
		if !ok || name == "" {
			continue
		}
		pods = append(pods, PodRef{
			Name:     name,
			LastSeen: time.UnixMilli(int64(row.Score)),
		})
	}
	return pods, truncated, nil
}

// PodState is what one round trip can learn about a pod without reading its
// rows: how it is configured, how much it holds, and which dictionary
// generation its newest row names.
type PodState struct {
	Meta       Meta
	LiveRows   int64
	RollupRows int64

	// Gen is the generation to decode against. Taken from the newest row
	// rather than from the meta hash: generations are monotonic and the lists
	// are newest-first, so the head row's generation is the highest one that
	// exists, while meta lags whenever a WriteMeta failed after its dictionary
	// was written.
	Gen int

	// NewestMS is the head row's timestamp, the anchor the index estimate is
	// computed from. Zero when the tier is empty.
	NewestMS int64
}

// States describes several pods in one round trip.
//
// The result is keyed by pod name and omits nothing: a pod whose keys have all
// expired comes back with a defaulted Meta and zero counts, which is a normal
// state rather than an error. The live tier's TTL is only twice the rollup
// interval, so every pod that stopped more than two hours ago is in exactly
// that state while remaining in the index for eight days.
func (r *Reader) States(ctx context.Context, deploymentID string, pods []PodRef, tier Tier) (map[string]PodState, error) {
	if len(pods) == 0 {
		return map[string]PodState{}, nil
	}

	pipe := r.client.Pipeline()
	type handles struct {
		meta   *redis.MapStringStringCmd
		live   *redis.IntCmd
		rollup *redis.IntCmd
		head   *redis.StringSliceCmd
	}
	queued := make(map[string]handles, len(pods))
	for _, pod := range pods {
		queued[pod.Name] = handles{
			meta:   pipe.HGetAll(ctx, MetaKey(deploymentID, pod.Name)),
			live:   pipe.LLen(ctx, LiveKey(deploymentID, pod.Name)),
			rollup: pipe.LLen(ctx, RollupKey(deploymentID, pod.Name)),
			// One row: it costs a few hundred bytes and yields both the exact
			// anchor timestamp and the real generation.
			head: pipe.LRange(ctx, TierKey(deploymentID, pod.Name, tier), 0, 0),
		}
	}
	if err := exec(ctx, pipe); err != nil {
		return nil, fmt.Errorf("podstats: describe pods of %s: %w", deploymentID, err)
	}

	out := make(map[string]PodState, len(pods))
	for name, h := range queued {
		fields, _ := h.meta.Result()
		state := PodState{Meta: parseMeta(name, fields)}
		state.LiveRows, _ = h.live.Result()
		state.RollupRows, _ = h.rollup.Result()
		state.Gen = state.Meta.Gen

		if rows, err := h.head.Result(); err == nil && len(rows) > 0 {
			if gen, at, ok := rowHeader(rows[0], tier); ok {
				// Monotonic, so the head row's generation is the newest that
				// exists — and it is the one the rows actually name.
				if gen > state.Gen {
					state.Gen = gen
				}
				state.NewestMS = at
			}
		}
		out[name] = state
	}
	return out, nil
}

// Dictionary fetches one generation of a pod's series dictionary.
//
// Falls back to the generation recorded in meta when the row's own generation
// names a hash that is gone, which happens if a WriteDictionary failed or its
// key expired on its own. It never walks generations downward: an older
// dictionary is a subset, so decoding a newer row against it would silently
// mislabel every index past its end.
func (r *Reader) Dictionary(ctx context.Context, deploymentID, pod string, state PodState) (map[int]Entry, error) {
	gens := []int{state.Gen}
	if state.Meta.Gen != state.Gen {
		gens = append(gens, state.Meta.Gen)
	}

	for _, gen := range gens {
		fields, err := r.client.HGetAll(ctx, DictKey(deploymentID, pod, gen)).Result()
		if err != nil {
			return nil, fmt.Errorf("podstats: read dictionary %d of %s: %w", gen, pod, err)
		}
		if len(fields) == 0 {
			continue
		}

		dict := make(map[int]Entry, len(fields))
		for rawIndex, encoded := range fields {
			index, err := strconv.Atoi(rawIndex)
			if err != nil {
				continue
			}
			var entry Entry
			if err := json.Unmarshal([]byte(encoded), &entry); err != nil {
				continue
			}
			// The field name is the authority: it is what the row's values are
			// positional to, and the Index inside the JSON merely repeats it.
			entry.Index = index
			dict[index] = entry
		}
		return dict, nil
	}
	return nil, nil
}

// Window is the slice of a tier a query asks for.
type Window struct {
	Tier Tier
	From time.Time
	To   time.Time
}

// Rows reads a pod's rows for a window, newest first.
//
// The extra row: one row older than From is deliberately included when
// available, and the caller is expected to use it and not emit it. A counter's
// first delta needs the reading before the window, or the first point of every
// counter chart would be missing.
func (r *Reader) Rows(ctx context.Context, deploymentID, pod string, state PodState, w Window) ([]json.RawMessage, error) {
	depth := state.Meta.Depth(w.Tier)
	if depth <= 0 {
		depth = continuationChunk
	}

	key := TierKey(deploymentID, pod, w.Tier)
	stop := int64(estimate(state, w))
	if stop > depth {
		stop = depth
	}

	var (
		kept     []json.RawMessage
		oldestMS int64
		start    int64
	)
	fromMS := w.From.UnixMilli()

	for {
		rows, err := r.client.LRange(ctx, key, start, stop-1).Result()
		if err != nil {
			return nil, fmt.Errorf("podstats: read %s rows of %s: %w", w.Tier, pod, err)
		}
		if len(rows) == 0 {
			return kept, nil
		}

		reachedBack := false
		for _, raw := range rows {
			_, at, ok := rowHeader(raw, w.Tier)
			if !ok {
				continue
			}
			// The list shifts under a continuation read: LPUSH keeps landing
			// while this pages, so index start is not the row after the last
			// one seen. Times are strictly decreasing down the list, so
			// anything not older than what is already held is a row this pass
			// has seen before.
			if oldestMS != 0 && at >= oldestMS {
				continue
			}
			oldestMS = at

			kept = append(kept, json.RawMessage(raw))
			if at <= fromMS {
				// The seeding row for the first delta, and the end of the
				// window. Everything past it is older than what was asked for.
				reachedBack = true
				break
			}
		}

		// Reached back past the window, ran out of list, or hit the tier's cap:
		// in all three there is nothing further to read.
		if reachedBack || int64(len(rows)) < stop-start || stop >= depth {
			return kept, nil
		}

		// The estimate was short. Continue from where it stopped.
		start = stop
		stop = start + continuationChunk
		if stop > depth {
			stop = depth
		}
	}
}

// estimate is how many rows back the window's start is likely to be.
//
// Newest-first and at least one step apart, so the row count between two times
// is at most their distance divided by the step. Gaps make the real count
// smaller, never larger, which is the property that makes an estimate usable
// here at all: it can only ever over-fetch.
func estimate(state PodState, w Window) int {
	step := state.Meta.Step(w.Tier)
	if step <= 0 {
		return continuationChunk
	}

	anchor := state.NewestMS
	if anchor == 0 {
		anchor = w.To.UnixMilli()
	}
	span := anchor - w.From.UnixMilli()
	if span < 0 {
		// The whole window predates the newest row. One row still has to be
		// read to find that out.
		return 1
	}

	// A sub-millisecond step rounds to zero milliseconds, and dividing by it
	// panics. Nothing should configure one — the sidecar's sample interval is a
	// duration an operator writes — but a division by a value derived from
	// remote configuration is not the place to find out.
	stepMS := step.Milliseconds()
	if stepMS < 1 {
		stepMS = 1
	}

	n := span/stepMS + 1 + estimateSlack
	if n > continuationChunk*8 {
		// Do not ask for a whole tier in one command on the strength of an
		// estimate; the continuation path handles a genuinely large window.
		return continuationChunk * 8
	}
	return int(n)
}

// rowHeader pulls the generation and timestamp out of a raw row without
// decoding its values, which is most of the bytes and none of the information
// paging needs.
// Both tiers spell it "t" — a sample's timestamp and a bucket's start — so one
// probe reads either, and the tier only matters to what the number means.
func rowHeader(raw string, _ Tier) (gen int, atMS int64, ok bool) {
	var head struct {
		Gen int   `json:"g"`
		At  int64 `json:"t"`
	}
	if err := json.Unmarshal([]byte(raw), &head); err != nil {
		return 0, 0, false
	}
	return head.Gen, head.At, true
}

// exec runs a pipeline, treating redis.Nil as success. A pipeline reports the
// first missing key as an error even when every other command succeeded, and a
// missing key is the normal state of a pod whose data has expired.
func exec(ctx context.Context, pipe redis.Pipeliner) error {
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}
