package alerting

import (
	"encoding/json"
	"testing"
	"time"
)

var fixtureStart = time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

// nowAfter returns the instant at which a series of n buckets is exactly fully
// readable: the newest closed bucket is the last one, and nothing is partial.
func nowAfter(n int, step time.Duration) time.Time {
	return fixtureStart.Add(time.Duration(n) * step).Add(EvalLag)
}

// counts builds a filled count series — every bucket a measurement, which is what
// the trace and log fetchers produce.
func counts(values ...float64) Series {
	s := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, len(values)), Filled: true}
	for i, v := range values {
		s.Set(i, v)
	}
	return s
}

// ratios builds a ratio series: numerators in Values, denominators alongside.
func ratios(pairs ...[2]float64) Series {
	s := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, len(pairs))}
	for i, p := range pairs {
		s.SetRatio(i, p[0], p[1])
	}
	return s
}

// repeat is the baseline builder: n buckets all reading v.
func repeat(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func mustCondition(t *testing.T, spec ConditionSpec) Condition {
	t.Helper()
	c, err := NewCondition(spec, time.Minute)
	if err != nil {
		t.Fatalf("building %s: %v", spec.Type, err)
	}
	return c
}

func params(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling params: %v", err)
	}
	return raw
}

func thresholdSpec(t *testing.T, metric string, p ThresholdParams) ConditionSpec {
	return ConditionSpec{
		ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: metric,
		Params: params(t, p),
	}
}

func spikeSpec(t *testing.T, metric string, p SpikeParams) ConditionSpec {
	return ConditionSpec{
		ID: "c_1", Type: KindSpike, Source: SourceTraces, Metric: metric,
		Params: params(t, p),
	}
}

func TestThresholdOnACount(t *testing.T) {
	c := mustCondition(t, thresholdSpec(t, "traces", ThresholdParams{
		Op: OpGT, Threshold: 10, WindowBuckets: 5, MinSamples: 1,
	}))
	s := counts(1, 2, 3, 4, 5) // sums to 15

	out := c.Evaluate(nowAfter(5, time.Minute), s)
	if out.Verdict != True {
		t.Fatalf("verdict %s (%s), want true", out.Verdict, out.Reason)
	}
	if out.Observed == nil || *out.Observed != 15 {
		t.Errorf("observed %v, want 15", out.Observed)
	}
	if out.Threshold != 10 {
		t.Errorf("the outcome does not carry the threshold it was judged against")
	}
	if out.WindowTo.Sub(out.WindowFrom) != 5*time.Minute {
		t.Errorf("window is %s, want 5m", out.WindowTo.Sub(out.WindowFrom))
	}
}

func TestThresholdReportsWhyItDidNotFire(t *testing.T) {
	c := mustCondition(t, thresholdSpec(t, "traces", ThresholdParams{
		Op: OpGT, Threshold: 100, WindowBuckets: 5, MinSamples: 1,
	}))
	out := c.Evaluate(nowAfter(5, time.Minute), counts(1, 2, 3, 4, 5))
	if out.Verdict != False || out.Reason != ReasonThresholdUnmet {
		t.Errorf("verdict %s reason %q, want false/%s", out.Verdict, out.Reason, ReasonThresholdUnmet)
	}
}

// An empty window is undecided, not fine. "We looked and it was fine" and "we
// could not look" are the two states an operator most needs to tell apart.
func TestThresholdWithNoDataIsUndecided(t *testing.T) {
	c := mustCondition(t, thresholdSpec(t, "duration_ns", ThresholdParams{
		Op: OpGT, Threshold: 1, WindowBuckets: 3, MinSamples: 1, Confidence: 0,
	}))
	s := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, 3)}
	out := c.Evaluate(nowAfter(3, time.Minute), s)
	if out.Verdict != Unknown || !out.NoData || out.Reason != ReasonNoData {
		t.Errorf("verdict %s noData %v reason %q, want unknown/true/%s",
			out.Verdict, out.NoData, out.Reason, ReasonNoData)
	}
}

func TestThresholdRefusesAMostlyEmptyWindow(t *testing.T) {
	c := mustCondition(t, thresholdSpec(t, "duration_ns", ThresholdParams{
		Op: OpGT, Threshold: 1, WindowBuckets: 5, MinSamples: 3,
	}))
	s := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, 5)}
	s.Set(4, 1000)
	out := c.Evaluate(nowAfter(5, time.Minute), s)
	if out.Verdict != Unknown || out.Reason != ReasonFewSamples {
		t.Errorf("verdict %s reason %q, want unknown/%s", out.Verdict, out.Reason, ReasonFewSamples)
	}
}

// The headline ratio case. Two traces, one of them failed, against a 10%
// threshold: the point estimate says 50% and the bound says 9%, and only one of
// those should be allowed to wake somebody up.
func TestRatioThresholdIgnoresATinySample(t *testing.T) {
	c := mustCondition(t, ConditionSpec{
		ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 0.10, WindowBuckets: 1, MinSamples: 1, MinDenominator: 1}),
	})
	out := c.Evaluate(nowAfter(1, time.Minute), ratios([2]float64{1, 2}))

	if out.Verdict != False {
		t.Fatalf("one failure in two traces cleared a 10%% threshold: %s (%s)", out.Verdict, out.Reason)
	}
	if out.Observed == nil || *out.Observed != 0.5 {
		t.Errorf("observed %v, want the 0.5 point estimate a human recognises", out.Observed)
	}
	if out.Score == nil || *out.Score > 0.10 {
		t.Errorf("score %v is not the conservative bound", out.Score)
	}
}

func TestRatioThresholdFiresOnRealEvidence(t *testing.T) {
	c := mustCondition(t, ConditionSpec{
		ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 0.10, WindowBuckets: 1, MinSamples: 1}),
	})
	out := c.Evaluate(nowAfter(1, time.Minute), ratios([2]float64{200, 400}))
	if out.Verdict != True {
		t.Fatalf("200 failures in 400 traces did not clear a 10%% threshold: %s (%s)", out.Verdict, out.Reason)
	}
}

func TestRatioThresholdRefusesTooFewTrials(t *testing.T) {
	c := mustCondition(t, ConditionSpec{
		ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 0.10, WindowBuckets: 1, MinSamples: 1}),
	})
	out := c.Evaluate(nowAfter(1, time.Minute), ratios([2]float64{2, 3}))
	if out.Verdict != Unknown || out.Reason != ReasonSmallDenom {
		t.Errorf("verdict %s reason %q, want unknown/%s", out.Verdict, out.Reason, ReasonSmallDenom)
	}
}

// A window in which nothing ran has no error rate. Reporting it as 0%% would
// satisfy every downward condition in the product.
func TestRatioThresholdWithNoTrialsIsUndecided(t *testing.T) {
	c := mustCondition(t, ConditionSpec{
		ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Params: params(t, ThresholdParams{Op: OpLT, Threshold: 0.10, WindowBuckets: 1, MinSamples: 1}),
	})
	out := c.Evaluate(nowAfter(1, time.Minute), ratios([2]float64{0, 0}))
	if out.Verdict != Unknown || !out.NoData {
		t.Errorf("verdict %s noData %v, want unknown/true", out.Verdict, out.NoData)
	}
}

// The guarantee, asserted twice with each gate disabled in turn, because the two
// stop it for different reasons and a change that broke one would otherwise be
// masked by the other.
func TestSpikeDoesNotFireOnAQuietSeriesGoingOneToThree(t *testing.T) {
	history := append(repeat(30, 1), 3)

	t.Run("the z gate holds it", func(t *testing.T) {
		c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{
			BaselineBuckets: 29, MinBaseline: 12,
			MinDelta: 0.0001, MinRatio: 0.0001, // both floors effectively off
		}))
		out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
		if out.Verdict != False {
			t.Fatalf("1 to 3 fired with only the z gate: %s (%s), z=%v", out.Verdict, out.Reason, out.Score)
		}
		if out.Reason != ReasonBelowZ {
			t.Errorf("reason %q, want %s", out.Reason, ReasonBelowZ)
		}
	})

	t.Run("the absolute floor holds it", func(t *testing.T) {
		c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{
			BaselineBuckets: 29, MinBaseline: 12,
			Z: 0.001, MinRatio: 0.0001, // the statistical gate effectively off
		}))
		out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
		if out.Verdict != False {
			t.Fatalf("1 to 3 fired with only the absolute floor: %s (%s)", out.Verdict, out.Reason)
		}
		if out.Reason != ReasonSmallDelta {
			t.Errorf("reason %q, want %s", out.Reason, ReasonSmallDelta)
		}
	})
}

func TestSpikeFiresOnARealJump(t *testing.T) {
	history := append(repeat(30, 1), 60)
	c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12}))

	out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
	if out.Verdict != True {
		t.Fatalf("1 to 60 did not fire: %s (%s), z=%v", out.Verdict, out.Reason, out.Score)
	}
	if out.Baseline == nil || *out.Baseline != 1 {
		t.Errorf("baseline %v, want the median of 1", out.Baseline)
	}
	if out.Observed == nil || *out.Observed != 60 {
		t.Errorf("observed %v, want 60", out.Observed)
	}
}

// The claim that makes median/MAD worth having over an EWMA: an earlier incident
// in the baseline must not stop the next one firing. Under an EWMA the mean would
// have been walked up by the first spike and the second would look ordinary.
func TestSpikeSurvivesAContaminatedBaseline(t *testing.T) {
	history := repeat(30, 2)
	for i := 5; i < 12; i++ {
		history[i] = 80 // a previous outage, seven buckets of it
	}
	history = append(history, 90)

	c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12}))
	out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
	if out.Verdict != True {
		t.Fatalf("a spike after an earlier one did not fire: %s (%s), baseline=%v z=%v",
			out.Verdict, out.Reason, out.Baseline, out.Score)
	}
	if out.Baseline == nil || *out.Baseline != 2 {
		t.Errorf("baseline %v, want the uncontaminated median of 2", out.Baseline)
	}
}

// The guard band's whole job: a ramp must not be allowed to supply its own
// baseline. Without the gap the immediately-preceding buckets are already high
// and the climb never registers.
func TestSpikeGuardBandKeepsARampVisible(t *testing.T) {
	history := append(repeat(30, 2), 40, 60, 90)
	spec := SpikeParams{BaselineBuckets: 25, MinBaseline: 12, GuardBuckets: 5}
	c := mustCondition(t, spikeSpec(t, "traces", spec))

	out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
	if out.Verdict != True {
		t.Fatalf("a ramp did not fire with a guard band: %s (%s), baseline=%v", out.Verdict, out.Reason, out.Baseline)
	}
	if out.Baseline == nil || *out.Baseline != 2 {
		t.Errorf("baseline %v, want 2 — the guard band did not hold the ramp out", out.Baseline)
	}
}

// A new watch has no history, and that is undecided rather than fine: calling it
// ok would let it resolve an incident it never actually evaluated.
func TestSpikeWithoutEnoughBaselineIsUndecided(t *testing.T) {
	c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12}))
	out := c.Evaluate(nowAfter(4, time.Minute), counts(1, 1, 1, 50))
	if out.Verdict != Unknown || out.Reason != ReasonShortBaseline {
		t.Errorf("verdict %s reason %q, want unknown/%s", out.Verdict, out.Reason, ReasonShortBaseline)
	}
}

func TestSpikeDownFiresOnACollapse(t *testing.T) {
	history := append(repeat(30, 100), 2)
	c := mustCondition(t, spikeSpec(t, "traces", SpikeParams{
		Direction: DirectionDown, BaselineBuckets: 29, MinBaseline: 12,
	}))
	out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
	if out.Verdict != True {
		t.Fatalf("a collapse from 100 to 2 did not fire: %s (%s), z=%v", out.Verdict, out.Reason, out.Score)
	}
	if !c.Downward() {
		t.Error("a downward spike does not report itself as downward")
	}
}

func TestAbsenceFiresOnlyOnSomethingThatUsedToReport(t *testing.T) {
	t.Run("reported, then silent", func(t *testing.T) {
		history := append(repeat(20, 5), repeat(5, 0)...)
		c := mustCondition(t, ConditionSpec{
			ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
			Params: params(t, AbsenceParams{ForBuckets: 5, MinBaseline: 12, BaselineBuckets: 20}),
		})
		out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
		if out.Verdict != True {
			t.Fatalf("a deployment that went silent did not fire: %s (%s)", out.Verdict, out.Reason)
		}
	})

	t.Run("never reported", func(t *testing.T) {
		history := repeat(25, 0)
		c := mustCondition(t, ConditionSpec{
			ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
			Params: params(t, AbsenceParams{ForBuckets: 5, MinBaseline: 12, BaselineBuckets: 20}),
		})
		out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
		if out.Verdict != False || out.Reason != ReasonNeverReported {
			t.Errorf("verdict %s reason %q, want false/%s — a watch on something that never ran must not fire forever",
				out.Verdict, out.Reason, ReasonNeverReported)
		}
	})

	t.Run("still reporting", func(t *testing.T) {
		history := append(repeat(20, 5), 0, 0, 3, 0, 0)
		c := mustCondition(t, ConditionSpec{
			ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
			Params: params(t, AbsenceParams{ForBuckets: 5, MinBaseline: 12, BaselineBuckets: 20}),
		})
		out := c.Evaluate(nowAfter(len(history), time.Minute), counts(history...))
		if out.Verdict != False {
			t.Errorf("verdict %s, want false — one bucket in the window did report", out.Verdict)
		}
	})
}

// Every reason code must be reachable, or the UI is rendering prose for states
// that cannot happen while the state that did happen has no prose at all.
func TestEveryDeclineReasonIsProduced(t *testing.T) {
	produced := map[string]bool{}
	record := func(o Outcome) { produced[o.Reason] = true }

	c := mustCondition(t, thresholdSpec(t, "traces", ThresholdParams{Op: OpGT, Threshold: 1e9, WindowBuckets: 1, MinSamples: 1}))
	record(c.Evaluate(nowAfter(1, time.Minute), counts(1)))
	record(c.Unavailable(ReasonFetchFailed, "boom"))

	empty := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, 3)}
	c = mustCondition(t, thresholdSpec(t, "duration_ns", ThresholdParams{Op: OpGT, Threshold: 1, WindowBuckets: 3, MinSamples: 1}))
	record(c.Evaluate(nowAfter(3, time.Minute), empty))
	c = mustCondition(t, thresholdSpec(t, "duration_ns", ThresholdParams{Op: OpGT, Threshold: 1, WindowBuckets: 3, MinSamples: 3}))
	one := empty
	one.Set(2, 5)
	record(c.Evaluate(nowAfter(3, time.Minute), one))

	c = mustCondition(t, ConditionSpec{ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "error_rate",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 0.1, WindowBuckets: 1, MinSamples: 1})})
	record(c.Evaluate(nowAfter(1, time.Minute), ratios([2]float64{1, 2})))

	history := append(repeat(30, 1), 3)
	c = mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12}))
	record(c.Evaluate(nowAfter(len(history), time.Minute), counts(history...)))
	c = mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12, Z: 0.001, MinRatio: 0.0001}))
	record(c.Evaluate(nowAfter(len(history), time.Minute), counts(history...)))
	// A rise that clears both the statistical and absolute gates but is not
	// proportionally large: 10 to 15 is only half again, against a floor of double.
	modest := append(repeat(30, 10), 15)
	c = mustCondition(t, spikeSpec(t, "traces", SpikeParams{BaselineBuckets: 29, MinBaseline: 12, Z: 0.001, MinDelta: 0.0001}))
	record(c.Evaluate(nowAfter(len(modest), time.Minute), counts(modest...)))
	record(c.Evaluate(nowAfter(4, time.Minute), counts(1, 1, 1, 3)))

	c = mustCondition(t, ConditionSpec{ID: "c_1", Type: KindAbsence, Source: SourceTraces, Metric: "traces",
		Params: params(t, AbsenceParams{ForBuckets: 5, MinBaseline: 12, BaselineBuckets: 20})})
	record(c.Evaluate(nowAfter(25, time.Minute), counts(repeat(25, 0)...)))

	want := []string{
		ReasonThresholdUnmet, ReasonFetchFailed, ReasonNoData, ReasonFewSamples,
		ReasonSmallDenom, ReasonBelowZ, ReasonSmallDelta, ReasonSmallRatio,
		ReasonShortBaseline, ReasonNeverReported,
	}
	for _, r := range want {
		if !produced[r] {
			t.Errorf("no case produces the reason %q", r)
		}
	}
}
