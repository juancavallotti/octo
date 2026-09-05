package series

import (
	"math"
	"sort"
	"strconv"

	dto "github.com/prometheus/client_model/go"
)

const (
	// bucketSuffix, sumSuffix and countSuffix are the series a histogram or
	// summary decomposes into, exactly as the exposition format already writes
	// them. Decomposing rather than modelling a histogram as one object is what
	// keeps the encoder, the collapse rules and the stored shape uniform: after
	// this point every series is a single float64 obeying one Kind.
	bucketSuffix = "_bucket"
	sumSuffix    = "_sum"
	countSuffix  = "_count"

	// leLabel and quantileLabel distinguish the decomposed series from one
	// another. le is a histogram's upper bound; quantile is a summary's rank.
	leLabel       = "le"
	quantileLabel = "quantile"

	// infBound is the +Inf bucket's le value, written the way Prometheus writes
	// it so a reader sees the same token the exposition used.
	infBound = "+Inf"

	// boundPrecision is how a finite bucket bound is formatted. 'g' with -1 gives
	// the shortest representation that round-trips, so 0.005 stays "0.005".
	boundPrecision = -1
)

// Encode turns one scrape into a Sample, growing the dictionary with any
// identity it has not seen.
//
// Every series the dictionary already knows gets a slot whether or not this
// scrape reported it: a flow removed by a config reload stops appearing, and
// writing NaN in its slot records that it was absent rather than that it read
// zero. Vector length therefore only ever grows, and always equals the
// dictionary length for the generation the sample names.
func (d *Dictionary) Encode(families map[string]*dto.MetricFamily, timeMS int64) Sample {
	// Interning happens first and in a deterministic order, so two pods that
	// scrape the same runtime build the same dictionary and a replay of one
	// scrape is byte-identical to the original.
	values := make(map[int]float64)
	for _, name := range sortedNames(families) {
		d.encodeFamily(families[name], values)
	}

	out := make([]float64, len(d.entries))
	for i := range out {
		if v, ok := values[i]; ok {
			out[i] = v
			continue
		}
		out[i] = math.NaN()
	}
	return Sample{Gen: d.gen, TimeMS: timeMS, Values: out}
}

// BumpGen advances the generation and returns it. Called once a scrape has
// appended identities, so the samples that follow name a dictionary that
// actually contains them.
func (d *Dictionary) BumpGen() int {
	d.gen++
	return d.gen
}

// sortedNames orders the families of a scrape by name. expfmt hands back a map,
// and map order would make the dictionary's index assignment depend on Go's
// hash seed.
func sortedNames(families map[string]*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encodeFamily writes every series of one family into values.
func (d *Dictionary) encodeFamily(f *dto.MetricFamily, values map[int]float64) {
	if f == nil {
		return
	}
	name := f.GetName()
	for _, m := range f.GetMetric() {
		switch f.GetType() {
		case dto.MetricType_COUNTER:
			d.put(values, name, labelsOf(m), KindCounter, m.GetCounter().GetValue())
		case dto.MetricType_GAUGE:
			d.put(values, name, labelsOf(m), KindGauge, m.GetGauge().GetValue())
		case dto.MetricType_HISTOGRAM:
			d.encodeHistogram(name, m, values)
		case dto.MetricType_SUMMARY:
			d.encodeSummary(name, m, values)
		case dto.MetricType_UNTYPED, dto.MetricType_GAUGE_HISTOGRAM:
			d.put(values, name, labelsOf(m), KindUntyped, m.GetUntyped().GetValue())
		}
	}
}

// encodeHistogram decomposes a histogram into its cumulative buckets plus _sum
// and _count. All three are counters: a bucket holds the number of observations
// at or below its bound since the process started, so the quantity that matters
// over a window is how much each grew. Collapsing them that way is what keeps
// the shape of the distribution instead of flattening it to one average.
func (d *Dictionary) encodeHistogram(name string, m *dto.Metric, values map[int]float64) {
	h := m.GetHistogram()
	base := labelsOf(m)
	for _, b := range h.GetBucket() {
		d.put(values, name+bucketSuffix, withLabel(base, leLabel, formatBound(b.GetUpperBound())),
			KindCounter, float64(b.GetCumulativeCount()))
	}
	// The +Inf bucket is implicit in the exposition — it equals the total count —
	// and is written explicitly here so a reader reconstructing the distribution
	// does not have to know that rule.
	d.put(values, name+bucketSuffix, withLabel(base, leLabel, infBound),
		KindCounter, float64(h.GetSampleCount()))
	d.put(values, name+sumSuffix, base, KindCounter, h.GetSampleSum())
	d.put(values, name+countSuffix, base, KindCounter, float64(h.GetSampleCount()))
}

// encodeSummary decomposes a summary into its quantiles plus _sum and _count.
//
// The quantiles are gauges, not counters. A quantile is a rank over the
// process's whole lifetime rather than a running total, so differencing two
// readings of it is meaningless; averaging them at least reports the typical
// value the process was showing. _sum and _count are cumulative as usual.
func (d *Dictionary) encodeSummary(name string, m *dto.Metric, values map[int]float64) {
	s := m.GetSummary()
	base := labelsOf(m)
	for _, q := range s.GetQuantile() {
		d.put(values, name, withLabel(base, quantileLabel, formatBound(q.GetQuantile())),
			KindGauge, q.GetValue())
	}
	d.put(values, name+sumSuffix, base, KindCounter, s.GetSampleSum())
	d.put(values, name+countSuffix, base, KindCounter, float64(s.GetSampleCount()))
}

// put interns an identity and records its value for this scrape.
func (d *Dictionary) put(values map[int]float64, name string, labels map[string]string, kind Kind, v float64) {
	values[d.intern(name, labels, kind)] = v
}

// withLabel returns base plus one more label, without mutating base — base is
// shared by every series a histogram decomposes into.
func withLabel(base map[string]string, name, value string) map[string]string {
	out := make(map[string]string, len(base)+1)
	for k, v := range base {
		out[k] = v
	}
	out[name] = value
	return out
}

// formatBound renders a bucket bound or quantile the way the exposition format
// does, so a stored label matches what a scrape of the same runtime would show.
func formatBound(v float64) string {
	if math.IsInf(v, 1) {
		return infBound
	}
	return strconv.FormatFloat(v, 'g', boundPrecision, 64)
}
