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
)

// anyone is every role a person can hold.
var anyone = []string{RoleAdmin, RoleOperator, RoleDeveloper, RoleMonitor}

// admins is the installation's own business: its alerting configuration, how
// long it keeps what it stores, and what that storage costs.
var admins = []string{RoleAdmin}

// rule is one pattern and what it requires. Pattern segments match a path
// segment literally, or match any single segment when they are "*".
type rule struct {
	pattern string
	read    []string
	write   []string
}

// policy is the table, most specific first.
var policy = []rule{
	// --- the history this service stores --------------------------------
	// Reading logs, traces and pod stats is what platform:monitor is for, and
	// what the Logs, Traces and deployment views are built on. Nothing here is
	// writable through this API: records arrive over the broker, not over HTTP.
	//
	// A trace carries the request bodies of whatever was traced, so this is the
	// most revealing read in the platform — but it is revealing to exactly the
	// people the platform already shows it to, and narrowing it further would
	// mean a monitor who cannot see what they are monitoring.
	{"logs", anyone, nil},
	{"traces", anyone, nil},
	{"stats", anyone, nil},

	// --- alerting --------------------------------------------------------
	// Acknowledging an incident is the one write a monitor performs: it is a
	// statement that somebody is looking, which is the job. Everything else
	// about a watch — what it matches, who it notifies, whether it is muted —
	// is the installation's configuration.
	{"alerts/incidents/*/ack", anyone, anyone},
	{"alerts/incidents", anyone, nil},
	{"alerts/evaluations", anyone, nil},
	{"alerts/preview", anyone, admins},
	{"alerts/watches", anyone, admins},

	// --- the installation itself -----------------------------------------
	// Retention decides how long everything above is kept, and the storage
	// report says what the two stores underneath it are costing. Both are the
	// installation's own business.
	{"settings", admins, admins},
	{"retention", admins, admins},
}

// bypass is the set of paths served without a token at all.
//
// Exact matches, so nothing under them is exempted by accident. The API
// description is open because it describes rather than discloses, and /healthz
// because a probe holds no credential.
var bypass = map[string]struct{}{
	"/healthz":            {},
	"/openapi.json":       {},
	"/openapi/operations": {},
}

// exempt reports whether path is served without a platform token.
func exempt(path string) bool {
	_, ok := bypass[strings.TrimSuffix(path, "/")]
	return ok
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
	return admins
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
