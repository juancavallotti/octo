package podstats

import (
	"strconv"
	"time"
)

// Tier names one of the two resolutions a pod's stats are kept at.
type Tier string

const (
	// TierAuto picks live when the window fits inside it and rollup otherwise.
	TierAuto Tier = "auto"
	// TierLive is one row per sample, kept for one rollup interval.
	TierLive Tier = "live"
	// TierRollup is one collapsed row per rollup interval, kept for the
	// retention window.
	TierRollup Tier = "rollup"
)

// Defaults matching the sidecar's, used when a pod's meta hash is missing or
// unreadable. Meta is written best-effort and is advisory — every number in it
// can be re-derived or worked around — so a pod with no meta is still readable
// rather than an error.
const (
	defaultSampleInterval = time.Second
	defaultRollupInterval = time.Hour
	defaultRetention      = 7 * 24 * time.Hour
)

// Meta is a pod's tier configuration, as the sidecar recorded it.
type Meta struct {
	Pod          string
	DeploymentID string

	// Gen is the newest dictionary generation the sidecar has written. It can
	// lag by one: WriteDictionary and WriteMeta are separate transactions, and
	// a failed WriteMeta leaves this behind the dictionary that exists. The
	// generation on the newest row is the authority; this is the fallback.
	Gen int

	SampleInterval time.Duration
	RollupInterval time.Duration
	Retention      time.Duration

	LiveDepth   int64
	RollupDepth int64

	StartedAt time.Time

	// Present records whether a meta hash was actually found, so a caller can
	// tell a defaulted pod from a configured one.
	Present bool
}

// LiveReach is how far back the live tier can possibly go. A window starting
// before this cannot be answered from live rows no matter how many are read,
// which is what lets tier selection reject or redirect it before any row is
// fetched.
func (m Meta) LiveReach() time.Duration {
	if m.LiveDepth <= 0 || m.SampleInterval <= 0 {
		return 0
	}
	return time.Duration(m.LiveDepth) * m.SampleInterval
}

// Step is the nominal spacing between rows of a tier.
func (m Meta) Step(tier Tier) time.Duration {
	if tier == TierRollup {
		return m.RollupInterval
	}
	return m.SampleInterval
}

// Depth is the capped length of a tier's list.
func (m Meta) Depth(tier Tier) int64 {
	if tier == TierRollup {
		return m.RollupDepth
	}
	return m.LiveDepth
}

// parseMeta reads a meta hash, falling back rather than failing.
//
// Deliberately tolerant. Every field is a hint used to bound a read or to
// describe a pod, and none of them is worth refusing to serve a pod over: a
// garbled duration costs a slightly worse index estimate, which the verify pass
// corrects anyway. An empty map yields the sidecar's own defaults with
// Present false.
func parseMeta(pod string, fields map[string]string) Meta {
	m := Meta{
		Pod:            pod,
		SampleInterval: defaultSampleInterval,
		RollupInterval: defaultRollupInterval,
		Retention:      defaultRetention,
		Present:        len(fields) > 0,
	}

	if v, ok := fields["pod"]; ok && v != "" {
		m.Pod = v
	}
	m.DeploymentID = fields["deployment"]
	m.Gen = atoiOr(fields["gen"], 0)
	m.SampleInterval = durationOr(fields["sampleInterval"], m.SampleInterval)
	m.RollupInterval = durationOr(fields["rollupInterval"], m.RollupInterval)
	m.Retention = durationOr(fields["retention"], m.Retention)

	// The depths are derived when absent, on the sidecar's own arithmetic, so
	// a pod with a half-written meta still bounds its reads correctly.
	m.LiveDepth = int64(atoiOr(fields["liveDepth"], 0))
	if m.LiveDepth <= 0 && m.SampleInterval > 0 {
		m.LiveDepth = int64(m.RollupInterval / m.SampleInterval)
	}
	m.RollupDepth = int64(atoiOr(fields["rollupDepth"], 0))
	if m.RollupDepth <= 0 && m.RollupInterval > 0 {
		m.RollupDepth = int64(m.Retention / m.RollupInterval)
	}

	if v, err := time.Parse(time.RFC3339, fields["startedAt"]); err == nil {
		m.StartedAt = v
	}
	return m
}

func atoiOr(raw string, fallback int) int {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func durationOr(raw string, fallback time.Duration) time.Duration {
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
