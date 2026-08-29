package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// specVersion is the contract version this runtime speaks. It is compared against
// the server's declared version for a warning only — refusing a deployment over a
// version string is how a working system becomes an outage over a typo.
const specVersion = "1.0"

// discoveryDocument is what GET /v1/discovery returns.
//
// Unknown fields are ignored and a missing feature key means unsupported, which
// is what makes this forward- and backward-compatible in the only two directions
// that occur: a server that predates a feature simply omits it, and a server that
// declares something this runtime does not know about is not wrong, just newer.
type discoveryDocument struct {
	SpecVersion    string          `json:"specVersion"`
	Implementation implementation  `json:"implementation"`
	Features       featureDocument `json:"features"`
}

// implementation identifies the server, for the startup log line. It exists so a
// support conversation can start from "which adapter, which version" rather than
// from a URL.
type implementation struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// featureDocument is the per-feature block. Every field is optional.
//
// Durations are integers with the unit in the name rather than Go duration
// strings: the people implementing this write TypeScript and Python, "20s" would
// need a parser in each of those, and OpenAPI can type an integer but not a
// duration grammar.
type featureDocument struct {
	KV             kvFeature          `json:"kv"`
	Secrets        secretsFeature     `json:"secrets"`
	Resources      featureFlags       `json:"resources"`
	Leases         leaseFeature       `json:"leases"`
	LeaderElection leaderFeature      `json:"leaderElection"`
	Queues         queueFeature       `json:"queues"`
	Topics         topicFeature       `json:"topics"`
	AgentMemory    agentMemoryFeature `json:"agentMemory"`
	Traces         traceFeature       `json:"traces"`
	Logs           featureFlags       `json:"logs"`
}

// featureFlags is the part every feature block shares.
type featureFlags struct {
	Supported bool `json:"supported"`
	// Unsupported overrides what happens when Supported is false: "noop" to
	// degrade, "error" to refuse. Ignored when Supported is true.
	Unsupported string `json:"unsupported"`
}

type kvFeature struct {
	featureFlags
	Namespaces    []string `json:"namespaces"`
	MaxValueBytes int64    `json:"maxValueBytes"`
}

type secretsFeature struct {
	featureFlags
	// EncryptedAtRest is a claim the server makes about itself. The runtime cannot
	// verify it; it warns when it is absent, because writing secrets to a store
	// that does not encrypt them is worth saying out loud once.
	EncryptedAtRest bool `json:"encryptedAtRest"`
}

type leaseFeature struct {
	featureFlags
	MinTTLSeconds     int `json:"minTtlSeconds"`
	MaxTTLSeconds     int `json:"maxTtlSeconds"`
	DefaultTTLSeconds int `json:"defaultTtlSeconds"`
}

type leaderFeature struct {
	featureFlags
	LeaseTTLSeconds        int `json:"leaseTtlSeconds"`
	RenewIntervalSeconds   int `json:"renewIntervalSeconds"`
	ObserveIntervalSeconds int `json:"observeIntervalSeconds"`
}

type queueFeature struct {
	featureFlags
	RequestReply       bool `json:"requestReply"`
	PollTimeoutSeconds int  `json:"pollTimeoutSeconds"`
	MaxBatch           int  `json:"maxBatch"`
	AckDeadlineSeconds int  `json:"ackDeadlineSeconds"`
}

type topicFeature struct {
	featureFlags
	PollTimeoutSeconds int `json:"pollTimeoutSeconds"`
	MaxBatch           int `json:"maxBatch"`
}

type agentMemoryFeature struct {
	featureFlags
	Semantic          bool `json:"semantic"`
	ListThreads       bool `json:"listThreads"`
	ReadThread        bool `json:"readThread"`
	Search            bool `json:"search"`
	MaxTurnsPerAppend int  `json:"maxTurnsPerAppend"`
}

type traceFeature struct {
	featureFlags
	MaxBatch            int `json:"maxBatch"`
	FlushIntervalMillis int `json:"flushIntervalMillis"`
}

// discoveryBackoff bounds how fast the startup retry loop asks again.
const (
	discoveryRetryBase = 200 * time.Millisecond
	discoveryRetryCap  = 5 * time.Second
)

// fetchDiscovery asks the platform what it implements, retrying within the
// configured budget.
//
// The budget is what makes the sidecar deployment work. When the platform API runs
// as a second container beside the runtime, container start order is not
// guaranteed, so failing on the first refused connection would crash-loop the
// runtime until the sidecar happened to win the race. Backing off for half a
// minute costs nothing when the server is already up and removes the race when it
// is not.
func fetchDiscovery(ctx context.Context, c *client, cfg Config) (discoveryDocument, error) {
	deadline := time.Now().Add(cfg.DiscoveryBudget)
	delay := discoveryRetryBase
	var lastErr error
	for {
		// Bounded by whichever runs out first. A budget shorter than the request
		// timeout is the interesting case: a server that accepts the connection and
		// then says nothing would otherwise hold startup for the full 10-second
		// request timeout before anyone checked a 50ms budget.
		doc, err := requestDiscovery(ctx, c, min(cfg.Timeout, time.Until(deadline)))
		if err == nil {
			return doc, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return discoveryDocument{}, fmt.Errorf("api: discovery: %w", ctx.Err())
		}
		if time.Now().Add(delay).After(deadline) {
			return discoveryDocument{}, fmt.Errorf("api: the platform API at %s did not answer "+
				"discovery within %s: %w", cfg.BaseURL, cfg.DiscoveryBudget, lastErr)
		}
		slog.Debug("api: discovery not answered yet, retrying",
			"url", cfg.BaseURL, "error", err, "in", delay)
		if err := sleep(ctx, jitter(delay)); err != nil {
			return discoveryDocument{}, err
		}
		if delay *= 2; delay > discoveryRetryCap {
			delay = discoveryRetryCap
		}
	}
}

// requestDiscovery makes one discovery call.
func requestDiscovery(ctx context.Context, c *client, timeout time.Duration) (discoveryDocument, error) {
	var doc discoveryDocument
	err := c.json(ctx, routeDiscovery, c.url(routeDiscovery), nil, &doc, timeout)
	if err != nil {
		// A discovery endpoint answering 501 is a server saying it implements
		// nothing, which is a legitimate (if empty) answer rather than a failure.
		if errors.Is(err, errNotImplemented) {
			return discoveryDocument{SpecVersion: specVersion}, nil
		}
		return discoveryDocument{}, err
	}
	return doc, nil
}

// isNotImplemented reports whether an error came from a 501, so a sub-client can
// latch its feature off.
func isNotImplemented(err error) bool { return errors.Is(err, errNotImplemented) }
