package api

import (
	"strings"
	"testing"
)

// The route table is the module's whole HTTP surface, and two later things read
// it: the retry policy, and the drift test that compares it against the published
// OpenAPI document. Both are only as good as the table being well-formed.
func TestRouteTableIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range routes {
		key := r.method + " " + r.path
		if seen[key] {
			t.Errorf("route %s appears twice", key)
		}
		seen[key] = true

		if !strings.HasPrefix(r.path, "/v1/") {
			t.Errorf("route %s is not under /v1/: the contract is versioned in the path", key)
		}
		if r.feature == "" {
			t.Errorf("route %s names no feature, so nothing can turn it off", key)
		}
		if strings.Count(r.path, "{") != strings.Count(r.path, "}") {
			t.Errorf("route %s has an unbalanced placeholder", key)
		}
	}
}

// Every negotiated feature has at least one route, except the two that have none
// by design: secrets is a view over KV, and core is discovery itself.
func TestEveryFeatureHasRoutes(t *testing.T) {
	covered := map[Feature]bool{}
	for _, r := range routes {
		covered[r.feature] = true
	}
	for _, f := range featureOrder {
		if f == FeatureSecrets {
			continue // core.NewSecretStore maps namespaces client-side; there is no secret route
		}
		if !covered[f] {
			t.Errorf("feature %s has no routes", f)
		}
	}
}

// Retryability is a property of the route rather than a condition repeated at each
// call site, so the table is where it has to be right: anything that could
// duplicate an effect the caller cannot see must not be retried.
func TestSendRoutesAreNotRetried(t *testing.T) {
	mustNotRetry := []route{
		routeQueuePublish, routeQueueRequest, routeQueueReceive, routeQueueReply,
		routeTopicPublish, routeTopicReceive,
		routeMemoryAppendTurns, routeTraces, routeLogs,
	}
	for _, r := range mustNotRetry {
		if r.idempotent {
			t.Errorf("route %s %s is marked idempotent; retrying it would duplicate an effect",
				r.method, r.path)
		}
	}
}
