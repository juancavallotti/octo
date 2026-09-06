package podstats

import "strconv"

// The key layout, mirrored from sidecars/stats/internal/store.
//
// Deployment-first, which is what makes the question this data exists to answer
// — "how is this deployment behaving" — a ZSET read rather than a keyspace
// scan. A deployment is a set of pods that come and go, and a reader starting
// from a deployment id has to find every pod that ever reported without knowing
// any pod's name in advance.
//
// Nothing here may ever reach for KEYS or SCAN. The pods index is the only
// entry point, on a Redis shared with the trace folds and the volatile KV tier.
const (
	// Layout is the key shape as a single string, asserted against the writer's
	// copy by the contract test. It is the published contract between the two
	// modules, and a silent change to it is a silent break.
	Layout = "octo:stats:v0:{deployment}:{pod}:{tier}"

	prefix    = "octo:stats:v0"
	podsKey   = "pods"
	metaKey   = "meta"
	dictKey   = "dict"
	liveKey   = "live"
	rollupKey = "rollup"
	keySep    = ":"
)

// PodsKey is the deployment's pod index: member is the pod name, score is the
// unix-ms time of its last write. The score doubles as a liveness hint, so a
// pod that stopped reporting can be filtered out without fetching its rows.
func PodsKey(deploymentID string) string {
	return join(prefix, deploymentID, podsKey)
}

// MetaKey holds the pod's tier configuration and its newest generation.
func MetaKey(deploymentID, pod string) string {
	return join(prefix, deploymentID, pod, metaKey)
}

// DictKey holds one generation of the series dictionary: field is the decimal
// index, value is an Entry as JSON.
func DictKey(deploymentID, pod string, gen int) string {
	return join(prefix, deploymentID, pod, dictKey, strconv.Itoa(gen))
}

// LiveKey is the full-resolution tier, newest first.
func LiveKey(deploymentID, pod string) string {
	return join(prefix, deploymentID, pod, liveKey)
}

// RollupKey is the collapsed history tier, newest first.
func RollupKey(deploymentID, pod string) string {
	return join(prefix, deploymentID, pod, rollupKey)
}

// TierKey is LiveKey or RollupKey, whichever the tier names.
func TierKey(deploymentID, pod string, tier Tier) string {
	if tier == TierRollup {
		return RollupKey(deploymentID, pod)
	}
	return LiveKey(deploymentID, pod)
}

func join(parts ...string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += keySep + p
	}
	return out
}
