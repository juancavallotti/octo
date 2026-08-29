package api

import "net/http"

// Path templates reused by more than one route. A route's method distinguishes
// what it does with the resource; the path is the resource.
const (
	pathKVEntry        = "/v1/kv/{namespace}/entry"
	pathMemoryWorking  = "/v1/agent-memory/{agentId}/threads/{threadKey}/working"
	pathMemoryThread   = "/v1/agent-memory/{agentId}/threads/{threadKey}"
	pathMemoryMemories = "/v1/agent-memory/{agentId}/users/{userId}/memories"
	pathTopicSubs      = "/v1/topics/{subject}/subscriptions"
)

// route is one call this module makes against the platform API.
//
// The table below is the module's whole HTTP surface, in one place, and every
// sub-client builds its URLs through client.url rather than a local Sprintf. Two
// things depend on that. Retry policy is a property of the route (idempotent),
// not a condition repeated at each call site. And the spec drift test compares
// this table against the published OpenAPI document, which it can only do if the
// table is exhaustive.
//
// path is the OpenAPI template, with {placeholders} filled positionally by
// client.url in the order they appear.
type route struct {
	method  string
	path    string
	feature Feature
	// idempotent allows the client to retry the call after a network error or a
	// retryable status. It is false wherever a retry could duplicate an effect the
	// caller cannot see: publishing a message, appending turns, shipping a trace.
	// It is also false for the long polls, where a retry would double the
	// outstanding connections rather than the effects — the poll loop is the retry.
	idempotent bool
}

// Discovery.
var routeDiscovery = route{http.MethodGet, "/v1/discovery", FeatureCore, true}

// KV. The key rides as a query parameter, not a path segment: keys are path-like
// and %2F in a segment is silently normalized by several proxies and gateways,
// which would merge two distinct keys into one. The namespace is a segment and
// carries its full suffixed name (user_secrets, system_volatile) so the server
// routes on the name it was given.
var (
	routeKVGet    = route{http.MethodGet, pathKVEntry, FeatureKV, true}
	routeKVSet    = route{http.MethodPut, pathKVEntry, FeatureKV, true}
	routeKVDelete = route{http.MethodDelete, pathKVEntry, FeatureKV, true}
)

// Resources. Same reasoning for the query parameters: a resource name may contain
// a slash.
var routeResourceContent = route{http.MethodGet, "/v1/resources/content", FeatureResources, true}

// Leases.
var (
	routeLeaseAcquire = route{http.MethodPost, "/v1/leases/acquire", FeatureLeases, true}
	routeLeaseRenew   = route{http.MethodPost, "/v1/leases/{leaseId}/renew", FeatureLeases, true}
	routeLeaseRelease = route{http.MethodPost, "/v1/leases/{leaseId}/release", FeatureLeases, true}
)

// Leader election. One endpoint serves both the first claim and every renewal:
// to a stateless server they are the same question — "I claim this key; do I hold
// it?" — and splitting them would only invite an implementer to treat the second
// as an update that must find an existing row.
var (
	routeLeaderCampaign = route{http.MethodPost, "/v1/leader/{key}/campaign", FeatureLeaderElection, true}
	routeLeaderResign   = route{http.MethodPost, "/v1/leader/{key}/resign", FeatureLeaderElection, true}
)

// Queues.
var (
	routeQueuePublish = route{http.MethodPost, "/v1/queues/{subject}/publish", FeatureQueues, false}
	routeQueueRequest = route{http.MethodPost, "/v1/queues/{subject}/request", FeatureQueues, false}
	routeQueueReceive = route{http.MethodPost, "/v1/queues/{subject}/receive", FeatureQueues, false}
	routeQueueAck     = route{http.MethodPost, "/v1/queues/{subject}/ack", FeatureQueues, true}
	routeQueueNack    = route{http.MethodPost, "/v1/queues/{subject}/nack", FeatureQueues, true}
	routeQueueReply   = route{http.MethodPost, "/v1/queues/reply", FeatureQueues, false}
)

// Topics. The explicit subscription resource is what makes fan-out expressible
// over a pull API: every subscriber needs its own cursor, and there is no way to
// say "each of you receives every message" without naming each of you.
var (
	routeTopicPublish     = route{http.MethodPost, "/v1/topics/{subject}/publish", FeatureTopics, false}
	routeTopicSubscribe   = route{http.MethodPost, pathTopicSubs, FeatureTopics, false}
	routeTopicReceive     = route{http.MethodPost, "/v1/topics/{subject}/receive", FeatureTopics, false}
	routeTopicAck         = route{http.MethodPost, "/v1/topics/{subject}/ack", FeatureTopics, true}
	routeTopicUnsubscribe = route{http.MethodDelete, pathTopicSubs + "/{subscriptionId}", FeatureTopics, true}
)

// Agent memory. userId rides as a query parameter on the thread routes and never
// as a segment: a conversation is addressed by its thread key alone, and a user
// segment would give it a second address under which a write naming a different
// user could mint a duplicate.
var (
	routeMemoryLoadWorking = route{http.MethodGet, pathMemoryWorking, FeatureAgentMemory, true}
	routeMemorySaveWorking = route{http.MethodPut, pathMemoryWorking, FeatureAgentMemory, true}
	routeMemoryAppendTurns = route{
		http.MethodPost, "/v1/agent-memory/{agentId}/threads/{threadKey}/turns", FeatureAgentMemory, false,
	}
	routeMemoryListThreads  = route{http.MethodGet, "/v1/agent-memory/{agentId}/threads", FeatureAgentMemory, true}
	routeMemoryReadThread   = route{http.MethodGet, pathMemoryThread, FeatureAgentMemory, true}
	routeMemoryDeleteThread = route{http.MethodDelete, pathMemoryThread, FeatureAgentMemory, true}
	routeMemorySetTitle     = route{
		http.MethodPut, "/v1/agent-memory/{agentId}/threads/{threadKey}/title", FeatureAgentMemory, true,
	}
	routeMemoryList   = route{http.MethodGet, pathMemoryMemories, FeatureAgentMemory, true}
	routeMemoryPut    = route{http.MethodPut, pathMemoryMemories, FeatureAgentMemory, true}
	routeMemoryDelete = route{http.MethodDelete, pathMemoryMemories, FeatureAgentMemory, true}
	routeMemorySearch = route{http.MethodPost, "/v1/agent-memory/{agentId}/search", FeatureAgentMemory, true}
)

// Traces and logs. Neither is retried: a duplicated trace record or log line is a
// lie about what happened, and both are best-effort by design.
var (
	routeTraces = route{http.MethodPost, "/v1/traces", FeatureTraces, false}
	routeLogs   = route{http.MethodPost, "/v1/logs", FeatureLogs, false}
)

// routes is every route above, for the spec drift test and for any tooling that
// wants to enumerate the surface. Adding a route without adding it here fails
// that test, which is the point.
var routes = []route{
	routeDiscovery,
	routeKVGet, routeKVSet, routeKVDelete,
	routeResourceContent,
	routeLeaseAcquire, routeLeaseRenew, routeLeaseRelease,
	routeLeaderCampaign, routeLeaderResign,
	routeQueuePublish, routeQueueRequest, routeQueueReceive,
	routeQueueAck, routeQueueNack, routeQueueReply,
	routeTopicPublish, routeTopicSubscribe, routeTopicReceive,
	routeTopicAck, routeTopicUnsubscribe,
	routeMemoryLoadWorking, routeMemorySaveWorking, routeMemoryAppendTurns,
	routeMemoryListThreads, routeMemoryReadThread, routeMemoryDeleteThread,
	routeMemorySetTitle, routeMemoryList, routeMemoryPut, routeMemoryDelete,
	routeMemorySearch,
	routeTraces, routeLogs,
}
