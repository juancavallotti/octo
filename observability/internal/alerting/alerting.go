// Package alerting turns the telemetry this service already stores into standing
// questions that answer themselves.
//
// A watch is one such question: a set of conditions over trace summaries, log
// events or pod stats, combined with all/any, asked on a fixed interval, and
// answered by taking an action when the combined verdict has held for long
// enough. The vocabulary in this file is what the rest of the package is written
// in; the arithmetic is in series.go and stats.go, the judgement in condition.go,
// and the decision about what to do with it in state.go.
//
// The shape of the package is deliberate: everything between "fetch" and "write"
// is a pure function over value types. The statistics, the gap policy and the
// flap control are exactly the parts that can be quietly wrong — a watch that
// never fires looks identical to a watch with nothing to report — so none of them
// may need a database or a clock to test. The fetchers live in the source
// subpackage for the same reason: this package's test binary never links pgx.
package alerting

import "time"

const (
	// EvalLag is how far behind the wall clock the newest readable bucket sits.
	//
	// A trace summary is upserted as its records arrive, the fold holds a
	// streaming run open for a second past its last record, and a pod-stat rollup
	// bucket is stamped with its start and written at its end. A bucket read the
	// instant it closes is therefore always short — and a series whose last point
	// is always short is indistinguishable from a series that just dropped, which
	// is the exact shape every downward condition here is watching for.
	EvalLag = 90 * time.Second

	// MinStep and MaxStep bound the bucket width a watch may ask for. Below the
	// minimum the evaluation lag is longer than the bucket and every series is
	// mostly hole; above the maximum a baseline of a dozen points is a week of
	// history, which is no longer a baseline for anything that happened today.
	MinStep = time.Minute
	MaxStep = time.Hour
)

// Source names which store answers a query.
//
// It is separate from the metric because the fetch, the gap policy and the
// failure modes all differ per store, and a metric name alone does not say which
// one it came from — "errors" means a log level in one and a trace status in
// another.
type Source string

const (
	SourceTraces   Source = "traces"    // Postgres: trace_summaries
	SourceLogs     Source = "logs"      // Postgres: logs
	SourcePodStats Source = "pod_stats" // Redis: the stats sidecar's tiers
)

// Aggregate is how the rows inside one bucket collapse to one number.
type Aggregate string

const (
	AggCount Aggregate = "count"
	AggSum   Aggregate = "sum"
	AggAvg   Aggregate = "avg"
	AggMin   Aggregate = "min"
	AggMax   Aggregate = "max"
	AggP95   Aggregate = "p95"
	// AggRatio divides a numerator count by a denominator count. It is an
	// aggregate rather than a condition kind because "error rate above 5%" and
	// "error rate suddenly doubled" are the same number judged two ways, and
	// making the ratio a metric is what stops the condition vocabulary growing a
	// kind per metric.
	AggRatio Aggregate = "ratio"
)

// FillsZero says whether a bucket with no rows is a zero or an unknown.
//
// The rule the whole package follows: zero-fill only where absence is itself a
// recorded observation. The trace and log tables record every event, so a bucket
// with no rows genuinely means none happened and counting it as zero is honest.
// An average, a percentile or a ratio over zero rows is undefined rather than
// zero — "no traces ran" is not "0% of them failed" — and reporting it as zero
// would quietly satisfy every downward condition in the product.
//
// Pod stats never fill, whatever the aggregate: a missing scrape means the
// sidecar did not report, which podstats already surfaces as a nil rather than a
// zero, and undoing that here would turn a scrape gap into a reading of nothing.
func (a Aggregate) FillsZero(src Source) bool {
	if src == SourcePodStats {
		return false
	}
	switch a {
	case AggCount, AggSum:
		return true
	case AggAvg, AggMin, AggMax, AggP95, AggRatio:
		return false
	default:
		return false
	}
}

// Op is how an observed number is compared with a threshold.
type Op string

const (
	OpGT  Op = "gt"
	OpGTE Op = "gte"
	OpLT  Op = "lt"
	OpLTE Op = "lte"
)

// Compare applies the operator. An unknown operator compares false rather than
// panicking: operators are validated when a watch is saved, and an evaluator that
// panicked on stored data would take the whole tick down with it.
func (o Op) Compare(observed, threshold float64) bool {
	switch o {
	case OpGT:
		return observed > threshold
	case OpGTE:
		return observed >= threshold
	case OpLT:
		return observed < threshold
	case OpLTE:
		return observed <= threshold
	default:
		return false
	}
}

// Downward reports whether the operator fires on small numbers. Those are the
// conditions a broken ingest pipeline would satisfy for the wrong reason, so the
// runner suppresses them while nothing at all is arriving.
func (o Op) Downward() bool { return o == OpLT || o == OpLTE }

// Truth is a three-valued verdict: a condition holds, does not hold, or could not
// be decided.
//
// Unknown is not a detail. A condition whose backend was unreachable has not
// reported false — under `all` that distinction decides whether a watch fires on
// the strength of its remaining conditions, which is not what somebody who wrote
// a conjunction asked for.
type Truth uint8

const (
	False Truth = iota
	True
	Unknown
)

func (t Truth) String() string {
	switch t {
	case True:
		return "true"
	case False:
		return "false"
	default:
		return "unknown"
	}
}

// TruthOf lifts an ordinary boolean.
func TruthOf(b bool) Truth {
	if b {
		return True
	}
	return False
}

// Combinator joins a watch's conditions.
type Combinator string

const (
	CombineAll Combinator = "all"
	CombineAny Combinator = "any"
)

// Combine folds condition verdicts into one, under Kleene's three-valued logic.
//
// The asymmetry is the whole point and is easy to get wrong. Under `all` a single
// false settles it — the other conditions cannot rescue a conjunction — but a
// single unknown poisons an otherwise-true result, because the operator asked for
// every condition and one of them was never answered. Under `any` a single true
// settles it even while a sibling is unknown, because a satisfied disjunct is a
// satisfied disjunct however blind the rest of the watch was.
//
// An empty set is Unknown rather than True (the vacuous reading) or False. A watch
// with no conditions is a mistake, and neither boolean answer says so.
func Combine(c Combinator, verdicts []Truth) Truth {
	if len(verdicts) == 0 {
		return Unknown
	}
	unknown := false
	for _, v := range verdicts {
		switch {
		case c == CombineAll && v == False:
			return False
		case c == CombineAny && v == True:
			return True
		case v == Unknown:
			unknown = true
		}
	}
	if unknown {
		return Unknown
	}
	if c == CombineAll {
		return True
	}
	return False
}

// NoDataPolicy says how a condition with nothing to measure is read.
//
// The default is NoDataOK, because the ordinary reason a window is empty is that
// an app was quiet, and a platform that paged on quiet would page on every
// weekend. A watch that should fire on silence says so with a count condition,
// where it is visible in the definition rather than implied by a default.
type NoDataPolicy string

const (
	NoDataOK   NoDataPolicy = "ok"   // absence does not satisfy the condition
	NoDataFire NoDataPolicy = "fire" // absence satisfies it
	NoDataKeep NoDataPolicy = "keep" // absence is unknown; the state machine does not move
)

// Truth reads an absent measurement under this policy.
func (p NoDataPolicy) Truth() Truth {
	switch p {
	case NoDataFire:
		return True
	case NoDataKeep:
		return Unknown
	default:
		return False
	}
}
