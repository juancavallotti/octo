package podstats

import (
	"encoding/json"
	"math"
	"sort"
)

// Turning rows back into series.
//
// Everything here is pure — rows in, series out, no Redis and no clock — which
// is deliberate: this is where the arithmetic that can be quietly wrong lives,
// and none of it should need a server to test.
//
// # Counters read as growth on both tiers
//
// A Prometheus counter is cumulative, and the history tier already stores the
// delta across a bucket rather than the readings. Live rows are made to match:
// a counter's value at a point is what it grew since the point before. A caller
// charting octo_flow_messages_total therefore never has to know which tier
// answered, which is the whole reason the tiers can be chosen automatically.
//
// The reset rule is the writer's: a reading lower than the one before means the
// process restarted, so the new reading is the growth rather than a large
// negative number.
//
// # Gaps stay gaps
//
// A reading the scrape did not report is nil, never zero, all the way to the
// JSON. Two reasons, and the second is the sharper one: a zero would draw a
// cliff where a series merely stopped being reported, and a NaN would reach
// encoding/json, which refuses it — and because httpx.WriteJSON writes the
// status before encoding, that surfaces as a 200 with a truncated body rather
// than an error anyone could act on.

// Stat names one of the numbers a rollup row carries. Live rows only ever have
// a value; the rest are what collapsing a bucket produced.
type Stat string

const (
	StatValue   Stat = "value"
	StatMin     Stat = "min"
	StatMax     Stat = "max"
	StatLast    Stat = "last"
	StatSamples Stat = "samples"
)

// KnownStats is every projectable stat, for validating a request.
var KnownStats = []Stat{StatValue, StatMin, StatMax, StatLast, StatSamples}

// Counters says whether a counter is reported as growth or as the raw
// cumulative reading.
type Counters string

const (
	// CountersDelta reports growth since the previous point.
	CountersDelta Counters = "delta"
	// CountersAbsolute reports the cumulative reading as stored.
	CountersAbsolute Counters = "absolute"
)

// Projection is what a query wants back per point.
type Projection struct {
	Stats    []Stat
	Counters Counters
	// Limit caps points per series, newest kept. Zero means unlimited, which
	// only the caller's own bounds should ever permit.
	Limit int
}

func (p Projection) wants(s Stat) bool {
	for _, want := range p.Stats {
		if want == s {
			return true
		}
	}
	return false
}

// Series is one decoded series of one pod, columnar and oldest-first.
//
// Columnar rather than a list of point objects because a series is thousands of
// points and every repeated key is paid per point. Times are unix milliseconds
// for the same reason: a thousand RFC3339 strings is mostly punctuation, for a
// number every chart converts straight back to an integer.
type Series struct {
	Pod    string
	Name   string
	Kind   Kind
	Labels map[string]string

	TimesMS []int64
	// EndsMS is set on the rollup tier only. A bucket's end is not the next
	// bucket's start when scraping stopped in between, so carrying both makes a
	// gap visible without inventing rows to fill it.
	EndsMS []int64

	Values  []*float64
	Min     []*float64
	Max     []*float64
	Last    []*float64
	Samples []int
}

// decodeRows turns one pod's raw rows into series.
//
// rows are newest-first as Redis returns them; the output is oldest-first,
// which is what a chart wants and what makes the counter delta a forward scan.
// Rows outside [fromMS, toMS] are dropped, except that one row older than
// fromMS is used to seed the first counter delta and then discarded.
func decodeRows(
	pod string,
	tier Tier,
	rows []json.RawMessage,
	dict map[int]Entry,
	indices []int,
	fromMS, toMS int64,
	p Projection,
) []Series {
	if len(indices) == 0 || len(rows) == 0 {
		return nil
	}

	// Oldest-first, so the seeding row comes before the points it seeds.
	ordered := make([]json.RawMessage, len(rows))
	for i, raw := range rows {
		ordered[len(rows)-1-i] = raw
	}

	series := make([]*Series, len(indices))
	for i, index := range indices {
		entry := dict[index]
		series[i] = &Series{
			Pod: pod, Name: entry.Name, Kind: entry.Kind, Labels: entry.Labels,
		}
	}

	// previous holds the last cumulative reading seen per series, which is what
	// a counter's delta is measured against. Seeded by rows before the window.
	previous := make([]float64, len(indices))
	seen := make([]bool, len(indices))

	for _, raw := range ordered {
		row, ok := decodeRow(raw, tier)
		if !ok {
			continue
		}
		if row.atMS > toMS {
			continue
		}
		inWindow := row.atMS >= fromMS

		for i, index := range indices {
			value, hasValue := row.value.At(index)
			kind := series[i].Kind

			// A counter's growth is measured whether or not the row is emitted,
			// so a row before the window still seeds the first delta.
			var point *float64
			if kind == KindCounter && p.Counters == CountersDelta && tier == TierLive {
				if hasValue {
					if seen[i] {
						point = ptr(growth(previous[i], value))
					}
					previous[i], seen[i] = value, true
				} else {
					// A gap breaks the chain: the next reading is not growth
					// since the last one seen, because what happened in between
					// is unknown.
					seen[i] = false
				}
			} else if hasValue {
				point = ptr(value)
			}

			if !inWindow {
				continue
			}
			appendPoint(series[i], row, index, point, tier, p)
		}
	}

	out := make([]Series, 0, len(series))
	for _, s := range series {
		if len(s.TimesMS) == 0 {
			continue
		}
		trim(s, p.Limit)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return labelKey(out[i].Labels) < labelKey(out[j].Labels)
	})
	return out
}

// appendPoint adds one point to a series, projecting only the stats asked for.
func appendPoint(s *Series, row decodedRow, index int, value *float64, tier Tier, p Projection) {
	s.TimesMS = append(s.TimesMS, row.atMS)
	s.Values = append(s.Values, value)

	if tier != TierRollup {
		return
	}

	s.EndsMS = append(s.EndsMS, row.endMS)
	if p.wants(StatMin) {
		s.Min = append(s.Min, nullable(row.min, index))
	}
	if p.wants(StatMax) {
		s.Max = append(s.Max, nullable(row.max, index))
	}
	if p.wants(StatLast) {
		s.Last = append(s.Last, nullable(row.last, index))
	}
	if p.wants(StatSamples) {
		s.Samples = append(s.Samples, row.samples)
	}
}

// trim keeps the newest limit points of a series.
func trim(s *Series, limit int) {
	if limit <= 0 || len(s.TimesMS) <= limit {
		return
	}
	cut := len(s.TimesMS) - limit
	s.TimesMS = s.TimesMS[cut:]
	s.Values = s.Values[cut:]
	s.EndsMS = tailInt64(s.EndsMS, cut)
	s.Min = tailPtr(s.Min, cut)
	s.Max = tailPtr(s.Max, cut)
	s.Last = tailPtr(s.Last, cut)
	if len(s.Samples) > cut {
		s.Samples = s.Samples[cut:]
	}
}

func tailInt64(v []int64, cut int) []int64 {
	if len(v) <= cut {
		return v
	}
	return v[cut:]
}

func tailPtr(v []*float64, cut int) []*float64 {
	if len(v) <= cut {
		return v
	}
	return v[cut:]
}

// decodedRow is a row of either tier, read once.
type decodedRow struct {
	atMS    int64
	endMS   int64
	samples int
	value   Values
	min     Values
	max     Values
	last    Values
}

func decodeRow(raw json.RawMessage, tier Tier) (decodedRow, bool) {
	if tier == TierRollup {
		var b Bucket
		if err := json.Unmarshal(raw, &b); err != nil {
			return decodedRow{}, false
		}
		return decodedRow{
			atMS: b.StartMS, endMS: b.EndMS, samples: b.Samples,
			value: b.Value, min: b.Min, max: b.Max, last: b.Last,
		}, true
	}

	var s Sample
	if err := json.Unmarshal(raw, &s); err != nil {
		return decodedRow{}, false
	}
	return decodedRow{atMS: s.TimeMS, value: s.Values}, true
}

// growth is the writer's counter arithmetic: a reading lower than the one
// before means the process restarted, so the whole new reading is the growth
// rather than a negative difference.
func growth(previous, current float64) float64 {
	if current < previous {
		return current
	}
	return current - previous
}

// nullable reads an index out of a column, as nil when it is absent or not a
// real number. Every access goes through it, because the row and the dictionary
// genuinely disagree about length: a row written at an older generation is
// short, and one written between the dictionary read and the row read is long.
func nullable(v Values, index int) *float64 {
	f, ok := v.At(index)
	if !ok {
		return nil
	}
	return &f
}

func ptr(f float64) *float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

// labelKey renders a label set as a stable string, for ordering only.
func labelKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := ""
	for _, k := range keys {
		out += k + "=" + labels[k] + ","
	}
	return out
}
