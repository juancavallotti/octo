package engine

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// observation is one turn's (estimate, measured) pair, in the order a run
// produces them.
type observation struct{ est, measured int }

func TestContextMeterFitsScaleAndOverhead(t *testing.T) {
	tests := []struct {
		name         string
		seedEst      int
		seedSize     int
		observations []observation
		predictEst   int
		want         int
	}{
		{
			// Nothing observed: the meter is the chars/4 estimate and nothing more,
			// which is what an agent has before its first turn.
			name:       "unfitted returns the raw estimate",
			predictEst: 400,
			want:       400,
		},
		{
			// One point cannot separate the two, so it credits the whole gap to
			// overhead — scale stays 1. That under-credits what dropping a message
			// saves, which errs towards compacting more rather than less.
			name:         "one point is all overhead",
			observations: []observation{{est: 400, measured: 1400}},
			predictEst:   400,
			want:         1400,
		},
		{
			// Two points separate them: 200 estimated tokens of new conversation cost
			// 400 measured, so scale is 2 and the constant 1000 is the system prompt
			// and tool schemas.
			name:         "two points separate scale from overhead",
			observations: []observation{{est: 400, measured: 1800}, {est: 600, measured: 2200}},
			predictEst:   500,
			want:         2000,
		},
		{
			// And the point of separating them: a prediction for a *smaller*
			// transcript keeps the whole overhead instead of scaling it away.
			name:         "shrinking the transcript keeps the overhead",
			observations: []observation{{est: 400, measured: 1800}, {est: 600, measured: 2200}},
			predictEst:   100,
			want:         1200,
		},
		{
			// A pair with no change in estimate divides by zero; keep the rate and
			// re-anchor the overhead on the newer measurement.
			name: "degenerate delta keeps the previous fit",
			observations: []observation{
				{est: 400, measured: 1800}, {est: 600, measured: 2200}, {est: 600, measured: 2300},
			},
			predictEst: 600,
			want:       2300,
		},
		{
			// A wild pair is clamped rather than trusted: 10x would compact a
			// conversation that has plenty of room left.
			name:         "an implausible rate is clamped",
			observations: []observation{{est: 100, measured: 200}, {est: 200, measured: 5200}},
			predictEst:   200,
			want:         5200, // overhead re-anchors to keep the newest point exact
		},
		{
			// A turn the provider did not account for teaches nothing.
			name:         "an unmeasured turn is ignored",
			observations: []observation{{est: 400, measured: 1400}, {est: 600, measured: 0}},
			predictEst:   400,
			want:         1400,
		},
		{
			// The estimate can over-count: it sizes an encrypted reasoning blob by its
			// bytes, which is far more than the tokens it costs. The fit has to take
			// that correction rather than clamp it away, or every later turn is
			// over-predicted and the agent compacts a conversation with room to spare.
			name:         "a measurement below the estimate is still reproduced",
			observations: []observation{{est: 400, measured: 200}},
			predictEst:   400,
			want:         200,
		},
		{
			// And the floor holds when the correction outweighs what is left.
			name:         "a negative residual cannot predict below zero",
			observations: []observation{{est: 400, measured: 200}},
			predictEst:   50,
			want:         0,
		},
		{
			// A stored transcript carries the rate its own run measured, so the first
			// turn back starts from real tokens rather than from chars/4.
			name:       "a seed sets the rate before any turn",
			seedEst:    100,
			seedSize:   250,
			predictEst: 200,
			want:       500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newContextMeter()
			m.seed(tt.seedEst, tt.seedSize)
			for _, o := range tt.observations {
				m.observe(o.est, o.measured)
			}
			if got := m.predict(tt.predictEst); got != tt.want {
				t.Errorf("predict(%d) = %d, want %d (scale %.3f, overhead %d)",
					tt.predictEst, got, tt.want, m.scale, m.overhead)
			}
		})
	}
}

// The newest measurement is always reproduced exactly. That is the property that
// makes the totals exact rather than merely close, and it has to survive
// clamping, a degenerate delta, and a seeded rate.
func TestContextMeterReproducesTheLastMeasurement(t *testing.T) {
	for _, obs := range [][]observation{
		{{est: 400, measured: 1400}},
		{{est: 400, measured: 1800}, {est: 600, measured: 2200}},
		{{est: 100, measured: 200}, {est: 200, measured: 9999}},
		{{est: 600, measured: 2200}, {est: 600, measured: 2300}},
		// The estimate over-counting, which a clamped residual could not reproduce.
		{{est: 400, measured: 200}},
		{{est: 800, measured: 300}, {est: 400, measured: 200}},
	} {
		m := newContextMeter()
		for _, o := range obs {
			m.observe(o.est, o.measured)
		}
		last := obs[len(obs)-1]
		if got := m.predict(last.est); got != last.measured {
			t.Errorf("predict(%d) = %d, want the measured %d", last.est, got, last.measured)
		}
		if m.measured() != last.measured {
			t.Errorf("measured() = %d, want %d", m.measured(), last.measured)
		}
	}
}

// sizeOf is what gets stored with a transcript, so it must leave the overhead
// out: the next run's system prompt and tool set are not this run's.
func TestContextMeterSizeOfExcludesOverhead(t *testing.T) {
	m := newContextMeter()
	m.observe(400, 1800)
	m.observe(600, 2200)
	if got := m.sizeOf(600); got != 1200 {
		t.Errorf("sizeOf(600) = %d, want 1200 (scale 2, no overhead)", got)
	}
	if got := m.predict(600) - m.sizeOf(600); got != m.overhead {
		t.Errorf("predict - sizeOf = %d, want the overhead %d", got, m.overhead)
	}
	if got := m.sizeOf(0); got != 0 {
		t.Errorf("sizeOf(0) = %d, want 0", got)
	}
}

// Reasoning is carried back to the provider on every turn, so a transcript that
// left it out of its own accounting would under-report itself by however much
// the model thought.
func TestEstimateTokensCountsReasoning(t *testing.T) {
	plain := []core.LLMMessage{{Role: core.LLMRoleAssistant, Text: "1234"}}
	withThinking := []core.LLMMessage{{
		Role: core.LLMRoleAssistant, Text: "1234",
		Thinking: []core.LLMThinkingBlock{{Text: "5678", Redacted: []byte("9012")}},
	}}
	if estimateTokens(withThinking) <= estimateTokens(plain) {
		t.Errorf("estimateTokens ignored reasoning: %d vs %d",
			estimateTokens(withThinking), estimateTokens(plain))
	}
	if got := estimateTokens(withThinking); got != 3 {
		t.Errorf("estimateTokens = %d, want 3 (12 chars / 4)", got)
	}
}
