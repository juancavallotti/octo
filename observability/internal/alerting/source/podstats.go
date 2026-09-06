package source

import (
	"context"
	"fmt"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
	"github.com/juancavallotti/octo/observability/internal/podstats"
)

// maxPodStatPoints bounds one pod-stat read. The live tier samples once a
// second, so an hour-wide window across a handful of pods is already thousands of
// points; this is the same order as the limit the HTTP API imposes and exists for
// the same reason.
const maxPodStatPoints = 5000

// fetchPodStats reads a metric out of Redis and re-buckets it onto the watch's
// grid.
//
// Two stages, and the order matters. Within one series, the samples that fall in
// a bucket collapse under the query's own aggregate — that is what turns
// per-second scrapes into a per-minute number. Across series, the per-bucket
// numbers then collapse under Scope.Across, which is the question "how do this
// deployment's pods combine". Doing it the other way round would mix samples from
// different pods before either question had been asked.
func (f *Fetcher) fetchPodStats(ctx context.Context, q alerting.Query) (alerting.Series, error) {
	if f.stats == nil {
		return alerting.Series{}, fmt.Errorf("source: pod stats are unavailable: this process has no reader")
	}
	if q.Scope.DeploymentID == "" {
		// The Redis key layout is deployment-scoped, so there is no cross-
		// deployment read to fall back to. Refusing here names the missing field;
		// the alternative is an empty series that reads as a quiet deployment.
		return alerting.Series{}, fmt.Errorf(
			"source: %w: a pod-stat condition must name a deployment", alerting.ErrInvalidParams)
	}

	result, err := f.stats.Series(ctx, podstats.Query{
		DeploymentID: q.Scope.DeploymentID,
		Pods:         q.Scope.Pods,
		Selector:     podstats.Selector{Names: []string{q.Metric}, Labels: q.Scope.Labels},
		Tier:         podstats.TierAuto,
		From:         q.From,
		To:           q.To,
		Projection: podstats.Projection{
			Stats: []podstats.Stat{podstats.StatValue},
			// Deltas, because a counter's absolute reading resets when a pod
			// restarts and a watch on the raw value would see every rollout as a
			// collapse. Growth per bucket is the thing anybody means.
			Counters: podstats.CountersDelta,
			Limit:    maxPodStatPoints,
		},
	})
	if err != nil {
		return alerting.Series{}, fmt.Errorf("source: read pod stats %s: %w", q.Metric, err)
	}
	return rebucket(q, result), nil
}

// rebucket collapses podstats series onto the alerting grid.
//
// Nothing is filled. A scrape gap arrives from podstats as a nil and leaves here
// as a nil, because a sidecar that did not report is not a pod reading zero — and
// this is the one source where that distinction cannot be recovered later.
func rebucket(q alerting.Query, result podstats.Result) alerting.Series {
	out := alerting.NewSeries(q.From, q.To, q.Step)
	if len(result.Series) == 0 {
		return out
	}

	// Per bucket, per series: the samples that landed in it.
	perSeries := make([]map[int][]float64, len(result.Series))
	for i, s := range result.Series {
		perSeries[i] = bucketSamples(out, s)
	}

	across := q.Scope.Across
	if across == "" {
		// The worst pod. A watch on memory or error growth is asking whether any
		// replica is in trouble, and an average across a healthy majority is how
		// a single sick pod stays invisible.
		across = alerting.AggMax
	}

	for i := range out.Len() {
		values := make([]float64, 0, len(perSeries))
		for _, samples := range perSeries {
			if in, ok := samples[i]; ok && len(in) > 0 {
				if v, err := alerting.Reduce(q.Aggregate, in); err == nil {
					values = append(values, v)
				}
			}
		}
		if !usable(across, values, len(perSeries)) {
			continue
		}
		if v, err := alerting.Reduce(across, values); err == nil {
			out.Set(i, v)
		}
	}
	return out
}

// usable says whether a bucket has enough of its series to answer under across.
//
// A sum is the case that matters: totalling the pods that happened to report
// understates the deployment, and an understated total is exactly what a downward
// condition fires on. So a sum needs every series present, and reports unknown
// otherwise. A max, min or average over the pods that did report is still an
// honest answer to the question it was asked.
func usable(across alerting.Aggregate, values []float64, seriesCount int) bool {
	if len(values) == 0 {
		return false
	}
	if across == alerting.AggSum {
		return len(values) == seriesCount
	}
	return true
}

// bucketSamples indexes one podstats series' points by the bucket they fall in.
func bucketSamples(grid alerting.Series, s podstats.Series) map[int][]float64 {
	out := make(map[int][]float64)
	for i, ms := range s.TimesMS {
		if i >= len(s.Values) || s.Values[i] == nil {
			continue
		}
		index, ok := grid.IndexOf(time.UnixMilli(ms).UTC())
		if !ok {
			continue
		}
		out[index] = append(out[index], *s.Values[i])
	}
	return out
}
