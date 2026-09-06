package alerting

import "fmt"

// Unit names what a number is, so the UI can render "11.4%" and "1.9 GB" beside
// each other without the watch having to declare one unit for all of it.
type Unit string

const (
	UnitCount   Unit = "count"
	UnitRatio   Unit = "ratio" // rendered as a percentage
	UnitUSD     Unit = "usd"
	UnitNanos   Unit = "ns"
	UnitBytes   Unit = "bytes"
	UnitUnknown Unit = ""
)

// Metric is one number a condition can be written about.
//
// The catalogue below is deliberately closed for the two Postgres sources. An
// open expression language over those tables is a second product — it needs a
// parser, a safety story and a way to explain itself in an incident — and every
// alert anyone has actually asked for is in this table. Pod stats are the
// exception and are open by necessity: the metric names come from whatever the
// runtime exports, and this service does not get to enumerate them.
//
// CountLike drives the Poisson floor in Scale. It is true for anything that
// counts occurrences, where the natural spread of a quiet series is the square
// root of its mean, and false for money, durations and proportions, where it is
// not.
type Metric struct {
	Name       string
	Source     Source
	Aggregates []Aggregate // permitted, first is the default
	Unit       Unit
	CountLike  bool

	// Numerator and Denominator name the two counts a ratio is built from. They
	// are the fetcher's instruction, and they are here rather than in the fetcher
	// so that "what is an error rate" is answered in one place for both the SQL
	// and the UI.
	Numerator   string
	Denominator string
}

// Ratio reports whether this metric is only ever a proportion.
func (m Metric) Ratio() bool { return m.Unit == UnitRatio }

// DefaultAggregate is what a condition gets when it names none.
func (m Metric) DefaultAggregate() Aggregate {
	if len(m.Aggregates) == 0 {
		return AggCount
	}
	return m.Aggregates[0]
}

// Allows reports whether agg is permitted for this metric. A percentile over a
// cost column is arithmetic nobody asked for, and refusing it at save time is
// cheaper than explaining the resulting number later.
func (m Metric) Allows(agg Aggregate) bool {
	for _, a := range m.Aggregates {
		if a == agg {
			return true
		}
	}
	return false
}

// traceMetrics and logMetrics are the closed catalogues. Keyed by name within a
// source, because "error_rate" means a trace status in one and a log level in the
// other and the two must not collide.
var (
	traceMetrics = map[string]Metric{
		"traces": {
			Name: "traces", Source: SourceTraces, Aggregates: []Aggregate{AggCount},
			Unit: UnitCount, CountLike: true,
		},
		"failed_traces": {
			Name: "failed_traces", Source: SourceTraces, Aggregates: []Aggregate{AggCount},
			Unit: UnitCount, CountLike: true,
		},
		"error_rate": {
			Name: "error_rate", Source: SourceTraces, Aggregates: []Aggregate{AggRatio},
			Unit: UnitRatio, Numerator: "failed_traces", Denominator: "traces",
		},
		"duration_ns": {
			Name: "duration_ns", Source: SourceTraces,
			Aggregates: []Aggregate{AggP95, AggAvg, AggMax}, Unit: UnitNanos,
		},
		"cost_usd": {
			Name: "cost_usd", Source: SourceTraces, Aggregates: []Aggregate{AggSum}, Unit: UnitUSD,
		},
		"tokens": {
			Name: "tokens", Source: SourceTraces, Aggregates: []Aggregate{AggSum},
			Unit: UnitCount, CountLike: true,
		},
		"llm_calls": {
			Name: "llm_calls", Source: SourceTraces, Aggregates: []Aggregate{AggSum},
			Unit: UnitCount, CountLike: true,
		},
		"unpriced_calls": {
			Name: "unpriced_calls", Source: SourceTraces, Aggregates: []Aggregate{AggSum},
			Unit: UnitCount, CountLike: true,
		},
	}

	logMetrics = map[string]Metric{
		"events": {
			Name: "events", Source: SourceLogs, Aggregates: []Aggregate{AggCount},
			Unit: UnitCount, CountLike: true,
		},
		"error_rate": {
			Name: "error_rate", Source: SourceLogs, Aggregates: []Aggregate{AggRatio},
			Unit: UnitRatio, Numerator: "error_events", Denominator: "events",
		},
	}
)

// LookupMetric resolves a metric within a source.
//
// A pod-stat metric is synthesized rather than looked up: the names are whatever
// the runtime's Prometheus registry exports, which this service has no business
// enumerating. It is treated as count-like only when its name says so, following
// the convention the exporter already uses — a _total is a counter, and the
// sidecar reports counters as per-bucket deltas, so its natural spread really is
// Poisson.
func LookupMetric(source Source, name string) (Metric, error) {
	switch source {
	case SourceTraces:
		if m, ok := traceMetrics[name]; ok {
			return m, nil
		}
	case SourceLogs:
		if m, ok := logMetrics[name]; ok {
			return m, nil
		}
	case SourcePodStats:
		if name == "" {
			return Metric{}, fmt.Errorf("alerting: %w: a pod-stat condition must name a metric", ErrInvalidParams)
		}
		return Metric{
			Name: name, Source: SourcePodStats,
			Aggregates: []Aggregate{AggMax, AggAvg, AggSum, AggMin},
			Unit:       UnitUnknown,
			CountLike:  hasSuffix(name, "_total"),
		}, nil
	default:
		return Metric{}, fmt.Errorf("alerting: %w: %q", ErrUnknownSource, source)
	}
	return Metric{}, fmt.Errorf("alerting: unknown %s metric %q", source, name)
}

// Metrics lists the closed catalogue for a source, for the editor's pickers. Pod
// stats return nothing, because their names are discovered from a deployment
// rather than declared here — the existing /stats/{id}/metrics route is what
// answers that question.
func Metrics(source Source) []Metric {
	var from map[string]Metric
	switch source {
	case SourceTraces:
		from = traceMetrics
	case SourceLogs:
		from = logMetrics
	case SourcePodStats:
		return nil
	default:
		return nil
	}
	out := make([]Metric, 0, len(from))
	for _, m := range from {
		out = append(out, m)
	}
	return out
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
