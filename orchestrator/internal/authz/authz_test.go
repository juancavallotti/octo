package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/caller"
)

// stubChecker answers with whatever the case set up, so these cover what the
// guard does with an answer rather than how a signature is checked.
type stubChecker struct {
	principal Principal
	err       error
}

func (s stubChecker) Verify(context.Context, string) (Principal, error) {
	return s.principal, s.err
}

// call drives one request through the guard and reports what happened.
func call(t *testing.T, c checker, method, path, token string) (*httptest.ResponseRecorder, *Principal) {
	t.Helper()
	var seen *Principal
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if p, ok := FromContext(r.Context()); ok {
			seen = &p
		}
	})
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req = req.WithContext(caller.With(req.Context(), token))
	}
	rec := httptest.NewRecorder()
	Wrap(c, next).ServeHTTP(rec, req)
	return rec, seen
}

func TestAnExemptRouteIsServedWithoutAToken(t *testing.T) {
	rec, principal := call(t, stubChecker{}, "GET", "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if principal != nil {
		t.Error("an exempt route produced a principal, and nothing verified one")
	}
}

func TestNoTokenIsRefusedWithAChallenge(t *testing.T) {
	rec, _ := call(t, stubChecker{}, "GET", "/integrations", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}

func TestARefusedTokenIs401(t *testing.T) {
	rec, _ := call(t, stubChecker{err: ErrUnauthenticated}, "GET", "/integrations", "a.b.c")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Being unable to decide is not the same as deciding no. A caller told
// "forbidden" because iam was unreachable goes and changes permissions that were
// never the problem.
func TestAnUnreachableKeysetIs503(t *testing.T) {
	rec, _ := call(t, stubChecker{err: ErrUnavailable}, "GET", "/integrations", "a.b.c")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestACallerHoldingTheRoleIsServedAndSeen(t *testing.T) {
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleDeveloper}}}
	rec, principal := call(t, c, "POST", "/integrations", "a.b.c")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if principal == nil || principal.Subject != "user-1" {
		t.Fatalf("principal = %+v, want the verified caller on the context", principal)
	}
}

func TestACallerWithoutTheRoleIs403(t *testing.T) {
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleMonitor}}}
	rec, _ := call(t, c, "POST", "/integrations", "a.b.c")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	// The roles required are not named: what this install expects is not something
	// an unauthorized caller should learn by asking.
	if body := rec.Body.String(); containsAnyRole(body) {
		t.Errorf("the refusal named a role: %s", body)
	}
}

// The pair the whole policy exists for: a deployment's token reaches its own
// stores and nothing else.
func TestAMachineTokenReachesItsStoresAndNotTheInstallation(t *testing.T) {
	c := stubChecker{principal: Principal{
		Subject: "user-1", Roles: []string{RoleRuntime}, Deployment: "dep-1",
	}}
	if rec, _ := call(t, c, "PUT", "/deployments/dep-1/kv/ns/k", "a.b.c"); rec.Code != http.StatusOK {
		t.Errorf("a pod was refused its own kv: %d", rec.Code)
	}
	if rec, _ := call(t, c, "GET", "/secrets", "a.b.c"); rec.Code != http.StatusForbidden {
		t.Errorf("a pod reached the installation's secrets: %d", rec.Code)
	}
	if rec, _ := call(t, c, "POST", "/integrations", "a.b.c"); rec.Code != http.StatusForbidden {
		t.Errorf("a pod created an integration: %d", rec.Code)
	}
}

// The half the roles cannot express: platform:runtime says a pod may reach a
// key/value namespace, not whose. A token mounted into one pod must not open
// another deployment's stores — it is a file on a filesystem, so "somebody else
// is holding it" is the case to design for.
func TestAMachineTokenCannotReachAnotherDeployment(t *testing.T) {
	c := stubChecker{principal: Principal{
		Subject: "user-1", Roles: []string{RoleRuntime}, Deployment: "dep-1",
	}}

	for _, path := range []string{
		"/deployments/dep-2/kv/ns/k",
		"/deployments/dep-2/objects/thing",
		"/deployments/dep-2/agent-memory/working",
		"/deployments/dep-2/namespaces",
	} {
		if rec, _ := call(t, c, "GET", path, "a.b.c"); rec.Code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403 — dep-1's token reached dep-2", path, rec.Code)
		}
		if rec, _ := call(t, c, "PUT", path, "a.b.c"); rec.Code != http.StatusForbidden {
			t.Errorf("PUT %s = %d, want 403 — dep-1's token wrote to dep-2", path, rec.Code)
		}
	}
}

// A person's token names no deployment, so nothing above applies to it: an
// operator looking at any deployment they like is the ordinary case.
func TestAPersonIsNotConstrainedToOneDeployment(t *testing.T) {
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleOperator}}}

	if rec, _ := call(t, c, "GET", "/deployments/dep-2/pods", "a.b.c"); rec.Code != http.StatusOK {
		t.Errorf("an operator was refused a deployment they did not deploy: %d", rec.Code)
	}
}

func containsAnyRole(body string) bool {
	for _, role := range []string{RoleAdmin, RoleOperator, RoleDeveloper, RoleMonitor, RoleRuntime} {
		if strings.Contains(body, role) {
			return true
		}
	}
	return false
}
