package user

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/iam/internal/authz"
)

// newTestServer wires the real Service and Handler over the in-memory repository,
// so these exercise routing, status mapping and the wire shape rather than a
// stubbed handler.
func newTestServer(t *testing.T) (*http.ServeMux, *Service, *memRepo) {
	t.Helper()
	repo := newMemRepo()
	svc := NewService(repo)
	mux := http.NewServeMux()
	handler := NewHandler(svc)
	// A stand-in caller rather than a real token: what a guard admits is the authz
	// package's business and is tested there. What these cover is routing, status
	// mapping and the wire shape, which need a principal on the request and not a
	// keyset behind it.
	handler.Register(mux, asTestCaller, asTestCaller)
	return mux, svc, repo
}

// testCaller is the subject every grant in these tests is attributed to.
const testCaller = "00000000-0000-0000-0000-00000000ca11"

func asTestCaller(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(
			authz.NewContext(r.Context(), authz.Principal{Subject: testCaller})))
	})
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
func TestUserResponseCarriesTheSubjectForDebugging(t *testing.T) {
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
	// It is the answer to "why is this person not getting in": whether their row
	// has been claimed, and by which account at the provider. Nothing outside
	// this service addresses a user by it.
	if got := raw["subject"]; got != "provider|abc123" {
		t.Errorf("subject = %v, want the one the provider presented", got)
	}
	if got := raw["id"]; got != u.ID {
		t.Errorf("id = %v, want %q", got, u.ID)
	}
}

// Somebody provisioned who has not arrived has no subject, and the field says so
// with an empty string rather than going missing — a reader of this list is
// asking exactly that question.
func TestAProvisionedUserReportsNoSubject(t *testing.T) {
	mux, svc, _ := newTestServer(t)
	u, err := svc.Create(context.Background(), "waiting@example.com", "Waiting", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	rec := do(t, mux, http.MethodGet, "/users/"+u.ID)
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := raw["subject"]; got != "" {
		t.Errorf("subject = %v, want it empty", got)
	}
	if raw["lastLoginAt"] != nil {
		t.Errorf("lastLoginAt = %v, want null", raw["lastLoginAt"])
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
	// Provisioned first: after the first user, this platform is an allowlist and
	// signing in is not by itself a way to get an account.
	if _, err := svc.Create(ctx, "second@example.com", "Second", nil, nil); err != nil {
		t.Fatalf("Create(second): %v", err)
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
	// Provisioned first: after the first user, this platform is an allowlist and
	// signing in is not by itself a way to get an account.
	if _, err := svc.Create(ctx, "second@example.com", "Second", nil, nil); err != nil {
		t.Fatalf("Create(second): %v", err)
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
		// A conflict rather than a bad request: the request is perfectly formed,
		// and what refuses it is the state of the platform.
		{"revoking the last admin", http.MethodDelete, "/users/" + u.ID + "/roles/" + string(RoleAdmin),
			http.StatusConflict},
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

// An empty install must answer with an empty array rather than null, for the
// same reason an unroled user must — and with no cursor, because there is no
// next page to ask for.
func TestListIsAnEmptyArrayOnAFreshInstall(t *testing.T) {
	mux, _, _ := newTestServer(t)

	rec := do(t, mux, http.MethodGet, "/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /users = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"items\":[]}\n" {
		t.Errorf("GET /users body = %q, want an empty page", got)
	}
}

// createUser posts a user and returns them, for the cases that need somebody to
// act on.
func createUser(t *testing.T, mux *http.ServeMux, email string) Response {
	t.Helper()
	body := `{"email":"` + email + `","name":""}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /users = %d (%s), want 201", rec.Code, rec.Body.String())
	}
	return decode[Response](t, rec)
}

// The note this replaced said grants were recorded with nobody behind them,
// because there was nobody to record. There is now, and it has to reach the row.
func TestGrantIsAttributedToTheCaller(t *testing.T) {
	mux, _, repo := newTestServer(t)
	created := createUser(t, mux, "a@example.com")

	rec := do(t, mux, http.MethodPut, "/users/"+created.ID+"/roles/"+string(RoleMonitor))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT role = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if got := repo.grantedBy[created.ID+"|"+string(RoleMonitor)]; got == nil || *got != testCaller {
		t.Errorf("granted_by = %v, want the caller %q", got, testCaller)
	}
}
