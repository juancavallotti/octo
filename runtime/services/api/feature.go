package api

import (
	"fmt"
	"log/slog"
	"sync"
)

// Feature names one platform capability the API server may or may not implement.
// It is the unit discovery negotiates, the unit a route belongs to, and the unit
// the 501 latch turns off — one vocabulary for all three, so a reader tracing "is
// KV available" finds a single answer.
type Feature string

// The negotiated features. FeatureCore is not negotiated: discovery itself must
// work or there is nothing to talk to.
const (
	FeatureCore           Feature = "core"
	FeatureKV             Feature = "kv"
	FeatureSecrets        Feature = "secrets"
	FeatureResources      Feature = "resources"
	FeatureLeases         Feature = "leases"
	FeatureLeaderElection Feature = "leaderElection"
	FeatureQueues         Feature = "queues"
	FeatureTopics         Feature = "topics"
	FeatureAgentMemory    Feature = "agentMemory"
	FeatureTraces         Feature = "traces"
	FeatureLogs           Feature = "logs"
)

// Policy says what the runtime does with calls into a feature the server does not
// implement.
type Policy string

const (
	// PolicyNoop degrades to core's no-op: reads miss, deletes succeed, and writes
	// return the capability's sentinel error.
	PolicyNoop Policy = "noop"
	// PolicyError refuses every call with a named error, so a misconfiguration
	// surfaces instead of running as if the capability were there.
	PolicyError Policy = "error"
)

// defaultPolicy is what an unsupported feature does when the server states no
// preference.
//
// Everything degrades to a no-op except leases and leader election, and those two
// are the deliberate exception. core.NoopLeaderElection grants leadership
// unconditionally and core.NoopLeases grants every claim — single-process
// semantics, correct where there is exactly one process. Behind this module there
// usually is not: two Cloud Run instances or two pods would each be told they hold
// the claim, and both would run the work the claim exists to run once. That is the
// failure core.Leases documents, it is silent, and it is a correctness bug rather
// than a missing feature. So refuse loudly, and let a single-instance deployment
// opt back in with "unsupported": "noop".
var defaultPolicy = map[Feature]Policy{
	FeatureKV:             PolicyNoop,
	FeatureSecrets:        PolicyNoop,
	FeatureResources:      PolicyNoop,
	FeatureLeases:         PolicyError,
	FeatureLeaderElection: PolicyError,
	FeatureQueues:         PolicyNoop,
	FeatureTopics:         PolicyNoop,
	FeatureAgentMemory:    PolicyNoop,
	FeatureTraces:         PolicyNoop,
	FeatureLogs:           PolicyNoop,
}

// fixedPolicy names the features where PolicyError would be unreachable, so the
// server's stated preference is ignored rather than half-honoured.
//
// Agent memory is gated by Enabled(): the engine takes an entirely separate path
// for a runtime that keeps no memory and never reaches the write that would
// error, so an erroring store would differ from the no-op only in code nobody
// runs. Traces are best-effort by construction — a publisher that returns errors
// helps nobody, and core.TracePublisher has nowhere to put one. Logs are the same
// story: LogSink() returning nil is already how a module says it ships no logs.
var fixedPolicy = map[Feature]Policy{
	FeatureAgentMemory: PolicyNoop,
	FeatureTraces:      PolicyNoop,
	FeatureLogs:        PolicyNoop,
}

// policyFor resolves the policy for an unsupported feature: the server's stated
// preference when it is one we recognize, else the default above. An unrecognized
// value is a typo in someone's discovery document, and defaulting is friendlier
// than refusing to start over one.
func policyFor(f Feature, stated string) Policy {
	if fixed, ok := fixedPolicy[f]; ok {
		if stated != "" && Policy(stated) != fixed {
			slog.Debug("api: this feature has no erroring mode; degrading instead",
				"feature", f, "requested", stated)
		}
		return fixed
	}
	switch Policy(stated) {
	case PolicyNoop:
		return PolicyNoop
	case PolicyError:
		return PolicyError
	}
	if stated != "" {
		slog.Warn("api: ignoring unrecognized unsupported policy",
			"feature", f, "value", stated, "default", defaultPolicy[f])
	}
	return defaultPolicy[f]
}

// unsupportedError is what a PolicyError feature returns. It names the feature and
// the fix, because the reader of this error is the person implementing the server.
func unsupportedError(f Feature) error {
	return fmt.Errorf("api: the platform API does not implement %s; "+
		"implement its routes, or declare \"unsupported\": \"noop\" for it in the discovery "+
		"document to degrade instead of failing", f)
}

// latch records that a feature turned out to be unimplemented at runtime, after
// discovery said otherwise.
//
// Discovery is fetched once, so it cannot catch the case that actually happens: a
// runtime rolled forward onto routes the server has not implemented yet. A 501
// from any route latches its feature off for the life of the process, said once
// and loudly. It is the same move services/k8s makes when the orchestrator does
// not know the agent-memory routes, for the same reason — the alternative is every
// call failing against a capability the runtime was told it had.
type latch struct {
	feature Feature
	mu      sync.RWMutex
	off     bool
}

// mark turns the feature off, logging the first time only.
func (l *latch) mark() {
	l.mu.Lock()
	first := !l.off
	l.off = true
	l.mu.Unlock()
	if first {
		slog.Warn("api: the platform API answered 501; this capability is now off for the "+
			"life of this process", "feature", l.feature,
			"hint", "implement the feature's routes, or declare it unsupported in the discovery document")
	}
}

// live reports whether the feature is still believed usable.
func (l *latch) live() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return !l.off
}
