package snapshot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
	"github.com/juancavallotti/octo/orchestrator/internal/environment/current"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
)

type knownEnvironments map[string]bool

func (k knownEnvironments) Exists(_ context.Context, name string) (bool, error) {
	return name == "" || k[name], nil
}

func TestPromoteRefuses(t *testing.T) {
	svc := NewService(&fakeRepo{}, fakeIntegrations{}).WithEnvironments(knownEnvironments{"staging": true})
	staging := current.With(context.Background(), "staging", true)
	cases := []struct {
		name     string
		ctx      context.Context
		target   string
		mayBuild bool
		want     error
	}{
		{"into its own environment", staging, "staging", true, ErrSameEnvironment},
		{"into the default by name, from the default", context.Background(), "octo", true, ErrSameEnvironment},
		{"into a name that is not an environment", context.Background(), "nowhere", true, ErrUnknownEnvironment},
		{"without a role that builds there", context.Background(), "staging", false, ErrMayNotBuild},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Promote(tc.ctx, "s1", tc.target, "", tc.mayBuild); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if out, err := svc.Promote(staging, "s1", "octo", "", true); err != nil || out.Environment != "" {
		t.Errorf("staging to the default: %+v, %v", out, err)
	}
}

func TestPromoteWithoutEnvironmentsIsRefused(t *testing.T) {
	svc := NewService(&fakeRepo{}, fakeIntegrations{})
	if _, err := svc.Promote(context.Background(), "s1", "staging", "", true); !errors.Is(err, ErrUnknownEnvironment) {
		t.Errorf("err = %v, want ErrUnknownEnvironment", err)
	}
}

// The guard judged the caller in the source; the handler judges them in the
// target, where the version will be written.
func TestPromoteIsJudgedByTheRolesInTheTarget(t *testing.T) {
	repo := newMemRepo()
	repo.snapshots["s1"] = Snapshot{ID: "s1", IntegrationID: "int-1", Tag: "v1"}
	mux := http.NewServeMux()
	NewHandler(NewService(repo, fakeIntegrations{it: integration.Integration{ID: "int-1"}}).
		WithEnvironments(knownEnvironments{"staging": true})).Register(mux)

	call := func(p authz.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/snapshots/s1/promote", strings.NewReader(`{"environment":"staging"}`))
		req = req.WithContext(authz.NewContext(req.Context(), p))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	monitorThere := authz.Principal{Subject: "u", Roles: []string{authz.RoleDeveloper},
		EnvRoles: map[string][]string{"staging": {authz.RoleMonitor}}}
	if rec := call(monitorThere); rec.Code != http.StatusForbidden {
		t.Errorf("a monitor in the target: status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	developerThere := authz.Principal{Subject: "u", Roles: []string{authz.RoleMonitor},
		EnvRoles: map[string][]string{"staging": {authz.RoleDeveloper}}}
	rec := call(developerThere)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"environment":"staging"`) {
		t.Errorf("a developer in the target: status = %d (%s)", rec.Code, rec.Body)
	}
	pod := authz.Principal{Subject: "u", Roles: []string{authz.RoleDeveloper}, Deployment: "d1"}
	if rec := call(pod); rec.Code != http.StatusForbidden {
		t.Errorf("a pod bound to the default: status = %d, want 403", rec.Code)
	}
}
