package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestServer wires the real Service and Handler over the in-memory repository,
// so these exercise routing, status mapping and the wire shape rather than a
// stubbed handler.
func newTestServer(t *testing.T) (*http.ServeMux, *Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := NewService(repo)
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return mux, svc, repo
}

func do(t *testing.T, mux *http.ServeMux, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

// The catalogue is served from the code, so a role nobody has been granted is
// still offered — which is what an admin screen needs to render a picker.
func TestCatalogueListsEveryRoleWithADescription(t *testing.T) {
	mux, _, _ := newTestServer(t)

	rec := do(t, mux, http.MethodGet, "/roles")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /roles = %d, want 200", rec.Code)
	}

	got := decode[[]struct {
		Role        Role   `json:"role"`
		Description string `json:"description"`
	}](t, rec)
	if len(got) != len(AllRoles()) {
		t.Fatalf("GET /roles returned %d roles, want %d", len(got), len(AllRoles()))
	}
	for _, r := range got {
		if !ValidRole(r.Role) {
			t.Errorf("catalogue offers %q, which is not grantable", r.Role)
		}
		if r.Description == "" {
			t.Errorf("role %q is offered with no description", r.Role)
		}
	}
}

// The OIDC subject identifies the account at the identity provider and nothing
// outside this service has a use for it, so it must not ride along on a read.
func TestUserResponseDoesNotLeakTheOIDCSubject(t *testing.T) {
	mux, svc, _ := newTestServer(t)
	u, err := svc.SignIn(context.Background(), "provider|abc123", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	rec := do(t, mux, http.MethodGet, "/users/"+u.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users/{id} = %d, want 200", rec.Code)
	}

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := raw["subject"]; present {
		t.Errorf("response carries the OIDC subject: %s", rec.Body.String())
	}
	if got := raw["id"]; got != u.ID {
		t.Errorf("id = %v, want %q", got, u.ID)
	}
}

// A user with no grants must serialize as [] and not null, so a caller never has
// to distinguish two encodings of the same fact.
func TestRolesSerializeAsAnEmptyArrayNotNull(t *testing.T) {
	mux, svc, _ := newTestServer(t)
	ctx := context.Background()
	if _, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}
	second, err := svc.SignIn(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("SignIn(second): %v", err)
	}

	rec := do(t, mux, http.MethodGet, "/users/"+second.ID)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := string(raw["roles"]); got != "[]" {
		t.Errorf("roles = %s, want []", got)
	}
}

func TestGrantAndRevokeReturnTheUpdatedUser(t *testing.T) {
	mux, svc, _ := newTestServer(t)
	ctx := context.Background()
	if _, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First"); err != nil {
		t.Fatalf("SignIn(first): %v", err)
	}
	second, err := svc.SignIn(ctx, "sub-2", "second@example.com", "Second")
	if err != nil {
		t.Fatalf("SignIn(second): %v", err)
	}

	rec := do(t, mux, http.MethodPut, "/users/"+second.ID+"/roles/"+string(RoleOperator))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT grant = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	granted := decode[Response](t, rec)
	if len(granted.Roles) != 1 || granted.Roles[0] != RoleOperator {
		t.Errorf("roles after grant = %v, want [%s]", granted.Roles, RoleOperator)
	}

	rec = do(t, mux, http.MethodDelete, "/users/"+second.ID+"/roles/"+string(RoleOperator))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE revoke = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	revoked := decode[Response](t, rec)
	if len(revoked.Roles) != 0 {
		t.Errorf("roles after revoke = %v, want none", revoked.Roles)
	}
}

func TestHandlerStatusMapping(t *testing.T) {
	mux, svc, _ := newTestServer(t)
	ctx := context.Background()
	u, err := svc.SignIn(ctx, "sub-1", "first@example.com", "First")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{"unknown user", http.MethodGet, "/users/deadbeef", http.StatusNotFound},
		{"grant to unknown user", http.MethodPut, "/users/deadbeef/roles/" + string(RoleMonitor),
			http.StatusNotFound},
		{"role outside the catalogue", http.MethodPut, "/users/" + u.ID + "/roles/platform:root",
			http.StatusBadRequest},
		{"revoking the last admin", http.MethodDelete, "/users/" + u.ID + "/roles/" + string(RoleAdmin),
			http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, mux, tt.method, tt.path)
			if rec.Code != tt.want {
				t.Errorf("%s %s = %d (%s), want %d",
					tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
			}
		})
	}
}

// An empty install must answer with [] rather than null, for the same reason an
// unroled user must.
func TestListIsAnEmptyArrayOnAFreshInstall(t *testing.T) {
	mux, _, _ := newTestServer(t)

	rec := do(t, mux, http.MethodGet, "/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("GET /users body = %q, want an empty array", got)
	}
}
