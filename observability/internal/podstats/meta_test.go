package podstats

import (
	"testing"
	"time"
)

func TestParseMetaReadsAPodsConfiguration(t *testing.T) {
	m := parseMeta("octo-dep-1-abc", map[string]string{
		"gen":            "3",
		"pod":            "octo-dep-1-abc",
		"deployment":     "dep-1",
		"sampleInterval": "1s",
		"rollupInterval": "1h0m0s",
		"retention":      "168h0m0s",
		"liveDepth":      "3600",
		"rollupDepth":    "168",
		"startedAt":      "2026-09-05T10:00:00Z",
	})

	if !m.Present {
		t.Error("Present is false for a meta hash that was found")
	}
	if m.Gen != 3 || m.DeploymentID != "dep-1" {
		t.Errorf("meta = %+v, want gen 3 of dep-1", m)
	}
	if m.SampleInterval != time.Second || m.RollupInterval != time.Hour {
		t.Errorf("intervals = %v/%v, want 1s/1h", m.SampleInterval, m.RollupInterval)
	}
	if m.LiveDepth != 3600 || m.RollupDepth != 168 {
		t.Errorf("depths = %d/%d, want 3600/168", m.LiveDepth, m.RollupDepth)
	}
	if m.StartedAt.IsZero() {
		t.Error("startedAt did not parse")
	}
	if want := time.Hour; m.LiveReach() != want {
		t.Errorf("LiveReach = %v, want %v", m.LiveReach(), want)
	}
}

// Meta is written best-effort and is advisory, so a pod with none is still
// readable. Refusing to serve it would lose a pod's stats over a hint.
func TestParseMetaFallsBackWhenAbsent(t *testing.T) {
	m := parseMeta("octo-dep-1-abc", nil)

	if m.Present {
		t.Error("Present is true for a pod with no meta hash")
	}
	if m.Pod != "octo-dep-1-abc" {
		t.Errorf("pod = %q, want the name it was looked up by", m.Pod)
	}
	if m.SampleInterval != time.Second || m.RollupInterval != time.Hour {
		t.Errorf("intervals = %v/%v, want the sidecar's defaults",
			m.SampleInterval, m.RollupInterval)
	}
	// Derived rather than left at zero, or every read would be unbounded.
	if m.LiveDepth != 3600 {
		t.Errorf("LiveDepth = %d, want 3600 derived from the intervals", m.LiveDepth)
	}
	if m.RollupDepth != 168 {
		t.Errorf("RollupDepth = %d, want 168 derived from the intervals", m.RollupDepth)
	}
}

// A garbled field costs a slightly worse index estimate, which the verify pass
// corrects. It is not worth refusing to serve the pod over.
func TestParseMetaToleratesGarbage(t *testing.T) {
	m := parseMeta("p", map[string]string{
		"gen":            "not-a-number",
		"sampleInterval": "banana",
		"rollupInterval": "0s",
		"liveDepth":      "",
		"startedAt":      "yesterday",
	})

	if m.Gen != 0 {
		t.Errorf("gen = %d, want 0", m.Gen)
	}
	if m.SampleInterval != time.Second {
		t.Errorf("sampleInterval = %v, want the default", m.SampleInterval)
	}
	// A zero duration is as unusable as an unparseable one — it would make the
	// depth arithmetic divide by zero.
	if m.RollupInterval != time.Hour {
		t.Errorf("rollupInterval = %v, want the default", m.RollupInterval)
	}
	if !m.StartedAt.IsZero() {
		t.Errorf("startedAt = %v, want the zero time", m.StartedAt)
	}
	if m.LiveDepth <= 0 {
		t.Errorf("LiveDepth = %d, want a derived positive depth", m.LiveDepth)
	}
}

func TestStepAndDepthFollowTheTier(t *testing.T) {
	m := parseMeta("p", map[string]string{
		"sampleInterval": "15s", "rollupInterval": "15m0s",
		"liveDepth": "60", "rollupDepth": "672",
	})

	if got := m.Step(TierLive); got != 15*time.Second {
		t.Errorf("live step = %v, want 15s", got)
	}
	if got := m.Step(TierRollup); got != 15*time.Minute {
		t.Errorf("rollup step = %v, want 15m", got)
	}
	if got := m.Depth(TierLive); got != 60 {
		t.Errorf("live depth = %d, want 60", got)
	}
	if got := m.Depth(TierRollup); got != 672 {
		t.Errorf("rollup depth = %d, want 672", got)
	}
	if got := m.LiveReach(); got != 15*time.Minute {
		t.Errorf("LiveReach = %v, want 15m", got)
	}
}

// LiveReach is what tier selection turns on, so a pod whose depth is unknown
// must report no reach rather than an infinite one.
func TestLiveReachIsZeroWithoutADepth(t *testing.T) {
	m := Meta{SampleInterval: time.Second}
	if got := m.LiveReach(); got != 0 {
		t.Errorf("LiveReach = %v, want 0", got)
	}
}
