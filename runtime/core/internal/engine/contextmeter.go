// Sizing an agent's context from what the provider says it read.
//
// The runtime has no tokenizer, and the chars/4 estimate next door is only ever
// a proportion. But every turn comes back with the exact size of the prompt the
// provider read, so the two together give what neither gives alone: an exact
// total, and a proportional split of it across the messages that made it up.
package engine

import (
	"math"

	"github.com/juancavallotti/octo/runtime/core"
)

// The bounds a fitted scale is held to. A fit is arithmetic over two provider
// figures, and a bad pair — a retried turn, a provider that miscounts, a prompt
// whose cache state changed underneath it — should degrade the estimate rather
// than produce a budget that compacts everything or nothing.
const (
	minTokenScale = 0.5
	maxTokenScale = 4.0
)

// contextMeter converts a chars/4 estimate into measured tokens.
//
// It separates the two things that make up a prompt, because they behave
// differently. The system prompt and the tool schemas are byte-for-byte
// identical on every turn of a run, so they are a constant: dropping a message
// does not shrink them. The conversation is the part that varies. Fitting one
// ratio over the sum of both would fold the constant into the per-message rate,
// which over-credits every dropped message by its share of the overhead — so
// compaction stops cutting while the prompt is still too big, which is the one
// outcome this whole mechanism exists to prevent.
//
// Two turns of one run are enough to separate them, because the difference
// between consecutive prompts is pure conversation:
//
//	scale    = (measured₂ − measured₁) / (est₂ − est₁)
//	overhead = measured₂ − scale·est₂
//
// The zero value is a usable meter that reports the raw estimate, which is what
// an agent gets before its first turn and what one talking to a provider that
// reports no usage keeps.
type contextMeter struct {
	// overhead is the measured constant: system prompt, tool schemas, and
	// whatever framing the provider adds. Zero until the first measurement.
	overhead int
	// scale is measured tokens per estimated token. One means "the chars/4
	// estimate was right", which is where it starts.
	scale float64
	// last is the most recent measured prompt, and prev* is the point before it,
	// held only to take the difference against.
	last                  int
	prevEst, prevMeasured int
	havePrev              bool
}

// newContextMeter returns a meter that trusts the raw estimate until it is told
// otherwise.
func newContextMeter() *contextMeter { return &contextMeter{scale: 1} }

// seed applies a scale carried over from a stored transcript: size is the
// measured contribution of msgs on the run that saved them, so their ratio is
// the same quantity a fit produces, minus the overhead that run's system prompt
// happened to have. It gives the first turn of a resumed conversation a rate
// learned from real tokens instead of the chars/4 default.
func (m *contextMeter) seed(est, size int) {
	if est <= 0 || size <= 0 {
		return
	}
	m.scale = clampScale(float64(size) / float64(est))
}

// observe records one turn: est is what estimateTokens said about the messages
// sent, measured is what the provider says it read. A turn the provider did not
// account for teaches nothing and is skipped.
func (m *contextMeter) observe(est, measured int) {
	if est <= 0 || measured <= 0 {
		return
	}
	// A pair with no difference in estimate cannot separate scale from overhead —
	// the arithmetic divides by zero — so keep the rate already fitted and use the
	// newer point only to re-anchor the overhead.
	if m.havePrev && est != m.prevEst {
		m.scale = clampScale(float64(measured-m.prevMeasured) / float64(est-m.prevEst))
	}
	// A signed residual, not a clamped one. It is usually the system prompt and
	// tool schemas, but it can legitimately come out negative: estimateTokens
	// counts an encrypted reasoning blob by its bytes, which is far more than the
	// tokens it actually costs, so a transcript carrying one estimates larger than
	// the prompt the provider read. Clamping that to zero would lose the
	// correction and over-predict every turn after it.
	m.overhead = measured - m.sizeOf(est)
	m.prevEst, m.prevMeasured, m.havePrev = est, measured, true
	m.last = measured
}

// observeResponse records what a finished turn read, ignoring a provider that
// accounted for nothing. It is the meter's own business whether a response
// carries usage, so the loop does not have to ask.
func (m *contextMeter) observeResponse(est int, resp *core.LLMResponse) {
	if resp == nil || resp.Usage == nil {
		return
	}
	m.observe(est, resp.Usage.PromptTokens)
}

// predict is the whole prompt a request carrying messages of this estimate would
// be: the conversation at the fitted rate, plus the run's constant overhead.
//
// Floored at zero, which is only reachable through a negative residual (see
// observe) on a transcript much smaller than the one that was measured.
func (m *contextMeter) predict(est int) int { return max(0, m.overhead+m.sizeOf(est)) }

// sizeOf is the conversation's own contribution, with the overhead left out. It
// is what gets stored with a transcript, since the next run's system prompt and
// tool set may not be this one's.
func (m *contextMeter) sizeOf(est int) int {
	if est <= 0 {
		return 0
	}
	scale := m.scale
	if scale == 0 {
		scale = 1
	}
	return int(math.Round(scale * float64(est)))
}

// measured is the last prompt size a provider reported, or zero if none has.
func (m *contextMeter) measured() int { return m.last }

// sizeOfMessages is the convenience the callers actually want: the fitted size
// of a transcript, estimate and all.
func (m *contextMeter) sizeOfMessages(msgs []core.LLMMessage) int {
	return m.sizeOf(estimateTokens(msgs))
}

func clampScale(scale float64) float64 {
	if math.IsNaN(scale) || math.IsInf(scale, 0) {
		return 1
	}
	return min(max(scale, minTokenScale), maxTokenScale)
}
