package authz

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/caller"
	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
)

// callIn drives one request naming environment (none when empty) through the
// header parser and the guard, and reports the status and the environment the
// handler saw.
func callIn(t *testing.T, p Principal, method, path, environment string) (int, string) {
	t.Helper()
	var seen string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = current.From(r.Context())
	})
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(caller.With(req.Context(), "token"))
	if environment != "" {
		req.Header.Set(current.Header, environment)
	}
	rec := httptest.NewRecorder()
	current.Parse(Wrap(stubChecker{principal: p}, next)).ServeHTTP(rec, req)
	return rec.Code, seen
}

func TestTheGuardJudgesRolesInTheRequestedEnvironment(t *testing.T) {
	person := Principal{
		Subject:  "u1",
		Roles:    []string{RoleMonitor},
		EnvRoles: map[string][]string{"staging": {RoleDeveloper}},
	}
	admin := Principal{Subject: "a1", Roles: []string{RoleAdmin}}
	machine := Principal{Subject: "u1", Roles: []string{RoleRuntime}, Deployment: "d1", Environment: "staging"}
	legacyMachine := Principal{Subject: "u1", Roles: []string{RoleRuntime}, Deployment: "d1"}
	key := Principal{Subject: "u1", Roles: []string{RoleCICD}, APIKey: "k1", Environment: "staging"}

	cases := []struct {
		name         string
		p            Principal
		method, path string
		environment  string
		status       int
		seen         string
	}{
		{"a developer in staging builds there", person, "POST", "/integrations", "staging", http.StatusOK, "staging"},
		{"a monitor in the default cannot build there", person, "POST", "/integrations", "", http.StatusForbidden, ""},
		{"no grant in qa", person, "GET", "/integrations", "qa", http.StatusForbidden, ""},
		{"an admin anywhere", admin, "DELETE", "/secrets/X", "qa", http.StatusOK, "qa"},
		{"a pod without the header acts in its own environment", machine, "GET", "/deployments/d1/kv", "", http.StatusOK, "staging"},
		{"a pod naming its own environment", machine, "GET", "/deployments/d1/kv", "staging", http.StatusOK, "staging"},
		{"a pod naming the default", machine, "GET", "/deployments/d1/kv", "octo", http.StatusForbidden, ""},
		{"a pod naming another", machine, "GET", "/deployments/d1/kv", "qa", http.StatusForbidden, ""},
		{"a legacy pod is bound to the default", legacyMachine, "GET", "/deployments/d1/kv", "", http.StatusOK, ""},
		{"a legacy pod cannot reach staging", legacyMachine, "GET", "/deployments/d1/kv", "staging", http.StatusForbidden, ""},
		{"a staging key rolls out in staging", key, "POST", "/deployments/d1/rollout", "", http.StatusOK, "staging"},
		{"a staging key cannot roll out in the default", key, "POST", "/deployments/d1/rollout", "octo", http.StatusForbidden, ""},
		{"a staging-only developer sees the environments", Principal{Subject: "u2", EnvRoles: map[string][]string{"staging": {RoleDeveloper}}}, "GET", "/environments", "", http.StatusOK, ""},
		{"listing environments ignores a stale header", person, "GET", "/environments", "deleted", http.StatusOK, "deleted"},
		{"a bound token may list environments from anywhere", Principal{Roles: []string{RoleMonitor}, APIKey: "k2", Environment: "staging"}, "GET", "/environments", "qa", http.StatusOK, "qa"},
		{"only an admin creates one", person, "POST", "/environments", "", http.StatusForbidden, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, seen := callIn(t, tc.p, tc.method, tc.path, tc.environment)
			if status != tc.status {
				t.Fatalf("status = %d, want %d", status, tc.status)
			}
			if status == http.StatusOK && seen != tc.seen {
				t.Errorf("handler saw environment %q, want %q", seen, tc.seen)
			}
		})
	}
}
