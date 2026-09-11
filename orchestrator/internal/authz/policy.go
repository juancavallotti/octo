package authz

import "strings"

// What each part of this API requires of a caller.
//
// Rules are matched in order and the first that matches decides, so they are
// written most specific first. Anything no rule matches requires platform:admin,
// which is what makes a route added without a thought about access fail closed
// rather than open.
//
// Reads and writes are separated because most of this API is readable by anyone
// who works here and writable by fewer. GET and HEAD are reads; everything else
// is a write.

// Roles, mirroring iam's catalogue. Named here rather than imported because the
// two services share no code, and a role is a string on a token either way.
const (
	RoleAdmin     = "platform:admin"
	RoleMonitor   = "platform:monitor"
	RoleDeveloper = "platform:developer"
	RoleOperator  = "platform:operator"
	// RoleRuntime is what a deployed integration's own token carries. No person
	// is ever granted it, and it reaches only the routes a running pod needs.
	RoleRuntime = "platform:runtime"
)

// anyone is every role a person can hold — a read that tells you nothing you
// could not see in the UI you are already entitled to open.
var anyone = []string{RoleAdmin, RoleOperator, RoleDeveloper, RoleMonitor}

// builds is who may create and change the things this platform is for.
var builds = []string{RoleAdmin, RoleOperator, RoleDeveloper}

// runs is who may act on a deployment: the two people roles that deploy, plus
// the pods themselves for the stores they own.
var runs = []string{RoleAdmin, RoleOperator, RoleRuntime}

// rule is one pattern and what it requires. Pattern segments match a path
// segment literally, or match any single segment when they are "*".
type rule struct {
	pattern string
	read    []string
	write   []string
}

// policy is the table, most specific first.
var policy = []rule{
	// --- what a running pod reaches -------------------------------------
	// Its own key/value store, its own object store, its own agent memory. A pod
	// holds platform:runtime and nothing else, so these are the only routes its
	// token opens — and they are scoped to a deployment id the handler checks.
	{"deployments/*/kv", runs, runs},
	{"deployments/*/namespaces", runs, runs},
	{"deployments/*/objects", append(builds, RoleRuntime), append(builds, RoleRuntime)},
	{"deployments/*/agent-memory", append(builds, RoleRuntime), append(builds, RoleRuntime)},
	// The frozen resources a deployment loads at start, by snapshot.
	{"snapshots/*/resources", append(anyone, RoleRuntime), builds},

	// --- operating ------------------------------------------------------
	// Reading a pod's logs is monitoring; creating and changing deployments is
	// not something a developer does here.
	{"deployments/*/pods", anyone, nil},
	{"deployments", anyone, []string{RoleAdmin, RoleOperator}},

	// --- building -------------------------------------------------------
	{"integrations", anyone, builds},
	{"folders", anyone, builds},
	{"snapshots", anyone, builds},
	{"devruns", anyone, builds},

	// --- the installation itself ----------------------------------------
	// Secrets, settings and outbound mail are the installation's own
	// configuration, and hold its credentials. Reads are as restricted as writes.
	{"secrets", []string{RoleAdmin}, []string{RoleAdmin}},
	{"settings", []string{RoleAdmin}, []string{RoleAdmin}},
	{"email", []string{RoleAdmin}, []string{RoleAdmin}},

	// --- each caller's own -----------------------------------------------
	// API keys are per-user and the handler scopes them to the id in the path;
	// what this rule says is only that a caller must be somebody.
	{"users", anyone, anyone},
	{"apikeys", anyone, anyone},
}

// bypass is the set of paths served without a token at all.
//
// Exact matches, so nothing under them is exempted by accident — /settings/health
// is open while the rest of /settings is admin-only. The last two disclose a
// little (whether Postgres is up, and which schema version it holds), which is
// already true of them and is what somebody looks at when authentication itself
// is what is broken.
var bypass = map[string]struct{}{
	"/healthz":            {},
	"/openapi.json":       {},
	"/openapi/operations": {},
	"/settings/health":    {},
	"/db-version":         {},
}

// selfAuthenticated are routes that carry a credential of their own and check it
// themselves, so this guard steps aside rather than refusing them.
//
// They are not open. A dev run's sidecar holds a token that authorises exactly
// one run and nothing else, and the handler verifies it — which is an
// authentication this service performs, just not with a platform token. Demanding
// one as well would mean a pod needing two credentials for a route whose whole
// design is that it needs a narrow one.
//
// Matched on the whole path rather than a prefix: stepping aside is not something
// to do for anything that merely starts the same way.
var selfAuthenticated = []string{
	"devruns/*/bundle",
	"devruns/*/expire",
}

// exempt reports whether path is served without a platform token — either
// because it is open, or because it authenticates itself.
func exempt(path string) bool {
	if _, ok := bypass[strings.TrimSuffix(path, "/")]; ok {
		return true
	}
	segments := split(path)
	for _, pattern := range selfAuthenticated {
		if len(strings.Split(pattern, "/")) == len(segments) && matches(pattern, segments) {
			return true
		}
	}
	return false
}

// required returns the roles that may perform method on path. An empty result
// means nobody: a route with no write rule is not writable by anyone, and an
// unmatched path is admin-only.
func required(method, path string) []string {
	write := method != "GET" && method != "HEAD"
	segments := split(path)
	for _, r := range policy {
		if !matches(r.pattern, segments) {
			continue
		}
		if write {
			return r.write
		}
		return r.read
	}
	// Fail closed. A route nobody wrote a rule for is the installation's own
	// business until somebody says otherwise.
	return []string{RoleAdmin}
}

// matches reports whether pattern describes the leading segments of path.
func matches(pattern string, segments []string) bool {
	want := strings.Split(pattern, "/")
	if len(segments) < len(want) {
		return false
	}
	for i, w := range want {
		if w != "*" && w != segments[i] {
			return false
		}
	}
	return true
}

// split breaks a path into its non-empty segments.
func split(path string) []string {
	out := make([]string, 0, 4)
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
