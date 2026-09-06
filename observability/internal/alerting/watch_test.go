package alerting

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func watchWith(specs ...ConditionSpec) Watch {
	return Watch{
		ID: "w_1", Name: "checkout", Enabled: true, Severity: "warning",
		Combinator: CombineAll, Conditions: specs, OnNoData: NoDataOK,
		Step: time.Minute, Interval: time.Minute, For: 5 * time.Minute,
	}
}

func countThreshold(t *testing.T, id string, threshold float64) ConditionSpec {
	t.Helper()
	return ConditionSpec{
		ID: id, Type: KindThreshold, Source: SourceTraces, Metric: "traces",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: threshold, WindowBuckets: 5, MinSamples: 1}),
	}
}

func mustBuild(t *testing.T, w Watch) *Built {
	t.Helper()
	b, err := Build(w)
	if err != nil {
		t.Fatalf("building the watch: %v", err)
	}
	return b
}

// fetchAll answers every query in a plan with the same series, which is what a
// test that cares about combination rather than about fetching wants.
func fetchAll(b *Built, now time.Time, s Series) map[string]Fetched {
	out := map[string]Fetched{}
	for _, q := range b.Plan(now) {
		out[q.Key()] = Fetched{Series: s}
	}
	return out
}

func TestHoldIsCountedInEvaluations(t *testing.T) {
	cases := []struct {
		forDur, interval time.Duration
		want             int
	}{
		{5 * time.Minute, time.Minute, 5},
		{0, time.Minute, 1},
		// Rounded up: a hold shorter than one tick still demands one evaluation.
		{90 * time.Second, time.Minute, 2},
		{time.Hour, 5 * time.Minute, 12},
	}
	for _, c := range cases {
		w := Watch{For: c.forDur, Interval: c.interval}
		if got := w.HoldEvaluations(); got != c.want {
			t.Errorf("for=%s interval=%s: %d evaluations, want %d", c.forDur, c.interval, got, c.want)
		}
	}
}

func TestBuildValidates(t *testing.T) {
	cases := []struct {
		name  string
		mutmp func(*Watch)
		want  error
	}{
		{"no name", func(w *Watch) { w.Name = "  " }, ErrInvalidWatch},
		{"no conditions", func(w *Watch) { w.Conditions = nil }, ErrInvalidWatch},
		{"bad combinator", func(w *Watch) { w.Combinator = "either" }, ErrInvalidWatch},
		{"step too small", func(w *Watch) { w.Step = time.Second }, ErrInvalidWatch},
		{"interval too large", func(w *Watch) { w.Interval = 48 * time.Hour }, ErrInvalidWatch},
		{"negative renotify", func(w *Watch) { w.Renotify = -time.Minute }, ErrInvalidWatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := watchWith(countThreshold(t, "c_1", 1))
			c.mutmp(&w)
			if _, err := Build(w); !errors.Is(err, c.want) {
				t.Errorf("error = %v, want %v", err, c.want)
			}
		})
	}
}

func TestBuildRefusesTooManyConditions(t *testing.T) {
	specs := make([]ConditionSpec, 0, MaxConditions+1)
	for i := range MaxConditions + 1 {
		specs = append(specs, countThreshold(t, string(rune('a'+i)), 1))
	}
	if _, err := Build(watchWith(specs...)); !errors.Is(err, ErrInvalidWatch) {
		t.Errorf("error = %v, want ErrInvalidWatch", err)
	}
}

// Condition ids key the history rows, so two conditions sharing one would make an
// outcome from three weeks ago impossible to line up with what produced it.
func TestBuildRefusesDuplicateConditionIDs(t *testing.T) {
	w := watchWith(countThreshold(t, "same", 1), countThreshold(t, "same", 2))
	if _, err := Build(w); !errors.Is(err, ErrInvalidWatch) {
		t.Errorf("error = %v, want ErrInvalidWatch", err)
	}
}

// Coalescing: identical rows at identical resolution are one fetch, and a
// sibling with a longer span widens it rather than adding a second.
func TestPlanCoalescesQueriesOverTheSameRows(t *testing.T) {
	now := nowAfter(60, time.Minute)
	b := mustBuild(t, watchWith(
		countThreshold(t, "c_1", 10),
		countThreshold(t, "c_2", 20),
		ConditionSpec{ID: "c_3", Type: KindSpike, Source: SourceTraces, Metric: "traces",
			Params: params(t, SpikeParams{BaselineBuckets: 30})},
	))

	plan := b.Plan(now)
	if len(plan) != 1 {
		t.Fatalf("plan has %d queries, want 1 — three clauses on the same rows must share a scan", len(plan))
	}
	// The union spans the longest of them: the spike's baseline, not the
	// thresholds' five buckets.
	if got := plan[0].Buckets(); got < 30 {
		t.Errorf("the coalesced query spans %d buckets, which is shorter than the spike's baseline", got)
	}
}

func TestPlanKeepsDifferentRowsApart(t *testing.T) {
	now := nowAfter(60, time.Minute)
	scoped := countThreshold(t, "c_2", 10)
	scoped.Scope = Scope{AppName: "checkout"}
	logs := ConditionSpec{ID: "c_3", Type: KindThreshold, Source: SourceLogs, Metric: "events",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 1, WindowBuckets: 5, MinSamples: 1})}

	plan := mustBuild(t, watchWith(countThreshold(t, "c_1", 10), scoped, logs)).Plan(now)
	if len(plan) != 3 {
		t.Fatalf("plan has %d queries, want 3 — a different scope or source is a different fetch", len(plan))
	}
}

// Two scopes differing only in the order somebody typed their log levels are the
// same scope, and must share one fetch.
func TestScopeKeyIsOrderInsensitive(t *testing.T) {
	a := Scope{Levels: []string{"error", "warn"}, Labels: map[string]string{"x": "1", "y": "2"}}
	b := Scope{Levels: []string{"warn", "error"}, Labels: map[string]string{"y": "2", "x": "1"}}
	if a.key() != b.key() {
		t.Errorf("scope keys differ by ordering:\n%q\n%q", a.key(), b.key())
	}
}

func TestEvaluateCombinesAndCounts(t *testing.T) {
	now := nowAfter(5, time.Minute)
	s := counts(1, 2, 3, 4, 5) // sums to 15

	t.Run("all: one clause short is false", func(t *testing.T) {
		w := watchWith(countThreshold(t, "c_1", 10), countThreshold(t, "c_2", 100))
		b := mustBuild(t, w)
		eval := b.Evaluate(now, fetchAll(b, now, s))
		if eval.Verdict != False || eval.Status != StatusOK {
			t.Errorf("verdict %s status %s, want false/ok", eval.Verdict, eval.Status)
		}
		if eval.Matched != 1 || eval.Total != 2 {
			t.Errorf("matched %d of %d, want 1 of 2", eval.Matched, eval.Total)
		}
	})

	t.Run("any: one clause is enough", func(t *testing.T) {
		w := watchWith(countThreshold(t, "c_1", 10), countThreshold(t, "c_2", 100))
		w.Combinator = CombineAny
		b := mustBuild(t, w)
		eval := b.Evaluate(now, fetchAll(b, now, s))
		if eval.Verdict != True || eval.Status != StatusFiring {
			t.Errorf("verdict %s status %s, want true/firing", eval.Verdict, eval.Status)
		}
	})
}

// The per-condition outcomes are what lets the UI say "fired because 2 of 3
// matched" and what lets a three-week-old row explain itself.
func TestEvaluateCarriesEveryOutcome(t *testing.T) {
	now := nowAfter(5, time.Minute)
	b := mustBuild(t, watchWith(countThreshold(t, "c_1", 10), countThreshold(t, "c_2", 100)))
	eval := b.Evaluate(now, fetchAll(b, now, counts(1, 2, 3, 4, 5)))

	if len(eval.Outcomes) != 2 {
		t.Fatalf("%d outcomes, want 2", len(eval.Outcomes))
	}
	for _, o := range eval.Outcomes {
		if o.Label == "" || o.Truth == "" || o.Observed == nil {
			t.Errorf("outcome %s does not explain itself: %+v", o.ConditionID, o)
		}
	}
	if eval.Outcomes[0].Threshold != 10 || eval.Outcomes[1].Threshold != 100 {
		t.Error("outcomes do not carry the thresholds they were judged against")
	}
}

// A backend that is down must not let an `all` watch fire on the strength of the
// clauses that did answer — and must not stop an `any` watch that has a genuinely
// satisfied clause.
func TestEvaluateWithAFailedFetch(t *testing.T) {
	now := nowAfter(5, time.Minute)
	s := counts(1, 2, 3, 4, 5)

	build := func(combinator Combinator) (*Built, map[string]Fetched) {
		hot := countThreshold(t, "c_1", 10) // would be true
		cold := countThreshold(t, "c_2", 10)
		cold.Scope = Scope{AppName: "other"} // its own fetch, which we fail
		w := watchWith(hot, cold)
		w.Combinator = combinator
		b := mustBuild(t, w)

		fetched := map[string]Fetched{}
		for _, q := range b.Plan(now) {
			if q.Scope.AppName == "other" {
				fetched[q.Key()] = Fetched{Err: errors.New("redis is unreachable")}
				continue
			}
			fetched[q.Key()] = Fetched{Series: s}
		}
		return b, fetched
	}

	t.Run("all is undecided", func(t *testing.T) {
		b, fetched := build(CombineAll)
		eval := b.Evaluate(now, fetched)
		if eval.Verdict != Unknown || eval.Status != StatusInsufficient {
			t.Errorf("verdict %s status %s, want unknown/insufficient", eval.Verdict, eval.Status)
		}
		if !eval.Degraded {
			t.Error("the evaluation does not record that it was blind in one eye")
		}
	})

	t.Run("any still fires", func(t *testing.T) {
		b, fetched := build(CombineAny)
		eval := b.Evaluate(now, fetched)
		if eval.Verdict != True || eval.Status != StatusFiring {
			t.Errorf("verdict %s status %s, want true/firing", eval.Verdict, eval.Status)
		}
		if !eval.Degraded {
			t.Error("a firing-while-degraded evaluation does not say so")
		}
	})
}

func TestEvaluateWithEveryFetchFailedIsAnError(t *testing.T) {
	now := nowAfter(5, time.Minute)
	b := mustBuild(t, watchWith(countThreshold(t, "c_1", 10)))
	fetched := map[string]Fetched{}
	for _, q := range b.Plan(now) {
		fetched[q.Key()] = Fetched{Err: errors.New("postgres is unreachable")}
	}
	eval := b.Evaluate(now, fetched)
	if eval.Status != StatusError {
		t.Errorf("status %s, want error — nothing about the watch could be judged", eval.Status)
	}
}

// The no-data policy belongs to the watch, not the condition: two clauses in one
// set must read an empty window the same way, or `all` means something different
// depending on which of them ran out of data.
func TestNoDataPolicyIsAppliedPerWatch(t *testing.T) {
	now := nowAfter(3, time.Minute)
	empty := Series{Step: time.Minute, StartMS: fixtureStart.UnixMilli(), Values: make([]*float64, 3)}
	spec := ConditionSpec{ID: "c_1", Type: KindThreshold, Source: SourceTraces, Metric: "duration_ns",
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 1, WindowBuckets: 3, MinSamples: 1})}

	for policy, want := range map[NoDataPolicy]Truth{NoDataOK: False, NoDataFire: True, NoDataKeep: Unknown} {
		w := watchWith(spec)
		w.OnNoData = policy
		b := mustBuild(t, w)
		eval := b.Evaluate(now, fetchAll(b, now, empty))
		if eval.Verdict != want {
			t.Errorf("policy %s gives %s, want %s", policy, eval.Verdict, want)
		}
	}
}

// Heterogeneous clauses are the point of the feature: nothing in the pipeline
// special-cases a source, so "error rate up AND memory above X" plans, fetches
// and combines like anything else.
func TestEvaluateAcrossDifferentSources(t *testing.T) {
	now := nowAfter(5, time.Minute)
	traces := countThreshold(t, "c_1", 10)
	pods := ConditionSpec{ID: "c_2", Type: KindThreshold, Source: SourcePodStats,
		Metric: "go_memstats_heap_inuse_bytes", Aggregate: AggMax,
		Scope:  Scope{DeploymentID: "d1"},
		Params: params(t, ThresholdParams{Op: OpGT, Threshold: 1, WindowBuckets: 5, MinSamples: 1})}

	b := mustBuild(t, watchWith(traces, pods))
	plan := b.Plan(now)
	if len(plan) != 2 {
		t.Fatalf("plan has %d queries, want one per source", len(plan))
	}
	sources := map[Source]bool{}
	for _, q := range plan {
		sources[q.Source] = true
	}
	if !sources[SourceTraces] || !sources[SourcePodStats] {
		t.Error("the plan does not reach both backends")
	}

	eval := b.Evaluate(now, fetchAll(b, now, counts(1, 2, 3, 4, 5)))
	if eval.Verdict != True {
		t.Errorf("verdict %s, want true — both clauses hold", eval.Verdict)
	}
}

// The fingerprint covers what changes the meaning of a pending clock, and
// nothing else. Renaming a watch mid-hold must not restart it.
func TestFingerprintCoversMeaningNotPresentation(t *testing.T) {
	base := watchWith(countThreshold(t, "c_1", 10))
	original, err := base.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}

	unchanged := []struct {
		name string
		mut  func(*Watch)
	}{
		{"renamed", func(w *Watch) { w.Name = "something else" }},
		{"described", func(w *Watch) { w.Description = "now with prose" }},
		{"re-severitied", func(w *Watch) { w.Severity = "critical" }},
		{"disabled", func(w *Watch) { w.Enabled = false }},
		{"re-addressed", func(w *Watch) {
			w.Actions = []ActionSpec{{ID: "a", Type: "email", Params: json.RawMessage(`{}`)}}
		}},
		{"renotified", func(w *Watch) { w.Renotify = time.Hour }},
	}
	for _, c := range unchanged {
		t.Run(c.name+" keeps the hold", func(t *testing.T) {
			w := base
			c.mut(&w)
			got, _ := w.Fingerprint()
			if got != original {
				t.Error("the fingerprint moved, so an in-flight hold would restart for no reason")
			}
		})
	}

	changed := []struct {
		name string
		mut  func(*Watch)
	}{
		{"retuned", func(w *Watch) { w.Conditions = []ConditionSpec{countThreshold(t, "c_1", 99)} }},
		{"recombined", func(w *Watch) { w.Combinator = CombineAny }},
		{"re-held", func(w *Watch) { w.For = time.Hour }},
		{"re-stepped", func(w *Watch) { w.Step = 5 * time.Minute }},
		{"re-intervalled", func(w *Watch) { w.Interval = 5 * time.Minute }},
		{"re-policied", func(w *Watch) { w.OnNoData = NoDataFire }},
	}
	for _, c := range changed {
		t.Run(c.name+" restarts the hold", func(t *testing.T) {
			w := base
			c.mut(&w)
			got, _ := w.Fingerprint()
			if got == original {
				t.Error("the fingerprint held, so a hold would carry across a change of meaning")
			}
		})
	}
}

func TestDownwardIsTrueIfAnyClauseIs(t *testing.T) {
	up := mustBuild(t, watchWith(countThreshold(t, "c_1", 10)))
	if up.Downward() {
		t.Error("an upward watch reported itself downward")
	}
	silence := ConditionSpec{ID: "c_2", Type: KindAbsence, Source: SourceTraces, Metric: "traces"}
	mixed := mustBuild(t, watchWith(countThreshold(t, "c_1", 10), silence))
	if !mixed.Downward() {
		t.Error("a watch with an absence clause is not recognised as downward")
	}
}
