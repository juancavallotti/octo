package authz

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPolicyPlacesEachRouteWhereItBelongs(t *testing.T) {
	cases := []struct {
		method, path string
		want         []string
	}{
		// A pod reaches its own stores and nothing else.
		{"PUT", "/deployments/d1/kv/ns/key", runs},
		{"GET", "/deployments/d1/namespaces", runs},
		{"PUT", "/deployments/d1/objects/a/b.txt", append(builds, RoleRuntime)},
		{"POST", "/deployments/d1/agent-memory/a1/search", append(builds, RoleRuntime)},
		{"GET", "/snapshots/s1/resources", append(anyone, RoleRuntime)},

		// Deploying is not something a developer does here; reading is.
		{"GET", "/deployments/d1", anyone},
		{"POST", "/deployments/d1/rollout", []string{RoleAdmin, RoleOperator}},
		{"DELETE", "/deployments/d1", []string{RoleAdmin, RoleOperator}},
		{"GET", "/deployments/d1/pods/p1/logs", anyone},

		// Building.
		{"GET", "/integrations", anyone},
		{"POST", "/integrations", builds},
		{"PUT", "/folders/f1", builds},
		{"POST", "/devruns", builds},

		// The installation's own configuration, readable by nobody else.
		{"GET", "/secrets", []string{RoleAdmin}},
		{"PUT", "/settings/llm", []string{RoleAdmin}},
		// Sending is the exception, and only sending: the server it goes through is
		// under /settings/email with the rest of the credentials. An unattended
		// repair has to be able to report what it did.
		{"POST", "/email/send", []string{RoleAdmin, RoleOperator}},
		{"GET", "/settings/email", []string{RoleAdmin}},
	}
	for _, tt := range cases {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			got := required(tt.method, tt.path)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("required = %v, want %v", got, tt.want)
			}
		})
	}
}

// The rule that makes a forgotten route safe: anything unmatched is the
// installation's own business.
func TestAnUnknownRouteIsAdminOnly(t *testing.T) {
	for _, path := range []string{"/something-new", "/queues", "/"} {
		if got := required("GET", path); len(got) != 1 || got[0] != RoleAdmin {
			t.Errorf("required(GET %s) = %v, want admin only", path, got)
		}
	}
}

// A rule with no write list is readable and not writable, by anyone at all.
func TestARouteWithNoWriteRuleIsWritableByNobody(t *testing.T) {
	if got := required("DELETE", "/deployments/d1/pods/p1/logs"); len(got) != 0 {
		t.Errorf("required = %v, want nobody", got)
	}
}

// Exemptions are exact: /settings/health is open while the rest of /settings is
// admin-only, and a prefix must not leak the difference.
func TestExemptionsAreExact(t *testing.T) {
	for _, path := range []string{"/healthz", "/settings/health", "/db-version", "/openapi.json"} {
		if !exempt(path) {
			t.Errorf("%s is not exempt and must be", path)
		}
	}
	for _, path := range []string{"/settings", "/settings/llm", "/settings/health/extra", "/healthzz"} {
		if exempt(path) {
			t.Errorf("%s is exempt and must not be", path)
		}
	}
}

// Every route this orchestrator registers has to be covered deliberately — by a
// rule or by an exemption. Unmatched routes already fail closed, so this is not
// about safety: it is about a route being admin-only because somebody meant it,
// rather than because they forgot.
func TestEveryRegisteredRouteIsCoveredByARuleOrAnExemption(t *testing.T) {
	handleFunc := regexp.MustCompile(`mux\.HandleFunc\("(GET|PUT|POST|DELETE|PATCH) (/[^"]*)"`)
	root := filepath.Join("..", "..")

	var uncovered []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, readErr := os.ReadFile(path) //nolint:gosec // walking this module's own sources
		if readErr != nil {
			return readErr
		}
		for _, m := range handleFunc.FindAllStringSubmatch(string(src), -1) {
			route := strings.ReplaceAll(m[2], "...", "")
			if exempt(route) || matchedByARule(route) {
				continue
			}
			uncovered = append(uncovered, m[1]+" "+route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking sources: %v", err)
	}
	if len(uncovered) > 0 {
		t.Errorf("these routes match no rule and are admin-only by default:\n  %s\n"+
			"Add a rule in policy.go, or an exemption, whichever you meant.",
			strings.Join(uncovered, "\n  "))
	}
}

// matchedByARule reports whether any rule claims this path. Path parameters are
// written {like this}, which stands in for exactly the single segment "*" matches.
func matchedByARule(route string) bool {
	segments := split(route)
	for _, r := range policy {
		if matches(r.pattern, segments) {
			return true
		}
	}
	return false
}

// The dev-run sidecar holds a token that authorises exactly one run, and the
// handler checks it. Gating those two routes on a platform token as well broke
// every dev run: the sidecar has no such token, so it retried a 401 forever and
// the pod never left Init.
func TestTheSidecarsOwnRoutesAreNotGated(t *testing.T) {
	for _, path := range []string{
		"/devruns/671b74fc-cfff-81cf/bundle",
		"/devruns/671b74fc-cfff-81cf/expire",
	} {
		if !exempt(path) {
			t.Errorf("%s is gated, and the sidecar has no platform token to pass it with", path)
		}
	}
}

// Stepping aside is for those two exactly, not for anything shaped like them.
func TestOnlyThoseTwoDevRunRoutesStepAside(t *testing.T) {
	for _, path := range []string{
		"/devruns",
		"/devruns/abc",
		"/devruns/abc/logs",
		"/devruns/abc/bundle/extra",
		"/devruns/abc/reload",
	} {
		if exempt(path) {
			t.Errorf("%s steps aside from the guard and must not", path)
		}
	}
}
