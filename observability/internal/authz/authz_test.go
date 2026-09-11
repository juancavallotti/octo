package authz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		req.Header.Set("Authorization", "Bearer "+token)
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
	rec, _ := call(t, stubChecker{}, "GET", "/logs", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
}

// The scheme is case-insensitive per RFC 6750, and clients differ.
func TestTheAuthorizationSchemeIsReadCaseInsensitively(t *testing.T) {
	req := httptest.NewRequest("GET", "/logs", nil)
	req.Header.Set("Authorization", "bearer a.b.c")
	rec := httptest.NewRecorder()
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleMonitor}}}
	Wrap(c, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestARefusedTokenIs401(t *testing.T) {
	rec, _ := call(t, stubChecker{err: ErrUnauthenticated}, "GET", "/logs", "a.b.c")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// Being unable to decide is not the same as deciding no. A caller told
// "forbidden" because iam was unreachable goes and changes permissions that were
// never the problem.
func TestAnUnreachableKeysetIs503(t *testing.T) {
	rec, _ := call(t, stubChecker{err: ErrUnavailable}, "GET", "/logs", "a.b.c")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestACallerHoldingTheRoleIsServedAndSeen(t *testing.T) {
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleMonitor}}}
	rec, principal := call(t, c, "GET", "/traces", "a.b.c")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if principal == nil || principal.Subject != "user-1" {
		t.Fatalf("principal = %+v, want the verified caller on the context", principal)
	}
}

func TestACallerWithoutTheRoleIs403(t *testing.T) {
	c := stubChecker{principal: Principal{Subject: "user-1", Roles: []string{RoleMonitor}}}
	rec, _ := call(t, c, "PUT", "/settings/retention", "a.b.c")
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	// The roles required are not named: what this install expects is not something
	// an unauthorized caller should learn by asking.
	if body := rec.Body.String(); containsAnyRole(body) {
		t.Errorf("the refusal named a role: %s", body)
	}
}

// A token minted for a deployment carries platform:runtime and, unless somebody
// asked for more, nothing else. Every rule here lists people's roles, so such a
// token reads none of this installation's history.
func TestABareRuntimeTokenReachesNothingHere(t *testing.T) {
	c := stubChecker{principal: Principal{
		Subject: "user-1", Roles: []string{"platform:runtime"}, Deployment: "dep-1",
	}}
	for _, path := range []string{"/logs", "/traces", "/stats/dep-1/pods", "/alerts/watches"} {
		if rec, _ := call(t, c, "GET", path, "a.b.c"); rec.Code != http.StatusForbidden {
			t.Errorf("a pod read %s: %d", path, rec.Code)
		}
	}
}

func containsAnyRole(body string) bool {
	for _, role := range []string{RoleAdmin, RoleOperator, RoleDeveloper, RoleMonitor} {
		if strings.Contains(body, role) {
			return true
		}
	}
	return false
}
