package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/observability/internal/environment/current"
)

type stubPrincipal struct{ p Principal }

func (s stubPrincipal) Verify(_ context.Context, _ string) (Principal, error) { return s.p, nil }

func TestTheGuardJudgesRolesInTheRequestedEnvironment(t *testing.T) {
	person := Principal{Roles: []string{RoleMonitor}, EnvRoles: map[string][]string{"staging": {RoleDeveloper}}}
	machine := Principal{Roles: []string{RoleDeveloper}, Deployment: "d1", Environment: "staging"}

	cases := []struct {
		name, environment string
		p                 Principal
		status            int
	}{
		{"a monitor reads logs in the default", "", person, http.StatusOK},
		{"a developer reads logs in staging", "staging", person, http.StatusOK},
		{"no grant in qa", "qa", person, http.StatusForbidden},
		{"a pod reads in its own environment", "", machine, http.StatusOK},
		{"a pod cannot read the default", "octo", machine, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/logs", nil)
			req.Header.Set("Authorization", "Bearer t")
			if tc.environment != "" {
				req.Header.Set(current.Header, tc.environment)
			}
			rec := httptest.NewRecorder()
			ok := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
			current.Parse(Wrap(stubPrincipal{tc.p}, ok)).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
		})
	}
}
