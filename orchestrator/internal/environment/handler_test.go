package environment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
)

func newTestMux() *http.ServeMux {
	svc, _, _ := newTestService()
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return mux
}

func do(mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerLifecycle(t *testing.T) {
	mux := newTestMux()

	rec := do(mux, http.MethodPost, "/environments", `{"name":"staging","displayName":"Staging"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created environmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Namespace != "octo-staging" || created.Default || created.CreatedAt == nil {
		t.Errorf("created = %+v", created)
	}

	rec = do(mux, http.MethodGet, "/environments", "")
	var list []environmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || !list[0].Default || list[0].CreatedAt != nil || list[1].Name != "staging" {
		t.Errorf("list = %+v", list)
	}

	if rec := do(mux, http.MethodPatch, "/environments/staging", `{"description":"pre-prod"}`); rec.Code != http.StatusOK {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := do(mux, http.MethodDelete, "/environments/staging", ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := do(mux, http.MethodGet, "/environments/staging", ""); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: %d", rec.Code)
	}
}

func TestHandlerStatuses(t *testing.T) {
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"invalid name", http.MethodPost, "/environments", `{"name":"Bad Name"}`, http.StatusBadRequest},
		{"reserved name", http.MethodPost, "/environments", `{"name":"octo"}`, http.StatusBadRequest},
		{"malformed body", http.MethodPost, "/environments", `{`, http.StatusBadRequest},
		{"delete the default", http.MethodDelete, "/environments/octo", "", http.StatusConflict},
		{"patch the default", http.MethodPatch, "/environments/octo", `{}`, http.StatusConflict},
		{"delete unknown", http.MethodDelete, "/environments/nope", "", http.StatusNotFound},
		{"get the default", http.MethodGet, "/environments/octo", "", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := do(newTestMux(), tc.method, tc.path, tc.body); rec.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// A caller is shown the environments it holds a role in and nothing else: a key
// bound to one is never offered another to pick and be refused in.
func TestListShowsWhatTheCallerReaches(t *testing.T) {
	mux := newTestMux()
	if rec := do(mux, http.MethodPost, "/environments", `{"name":"staging"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	as := func(p authz.Principal) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r.WithContext(authz.NewContext(r.Context(), p)))
		})
	}
	names := func(h http.Handler) []string {
		var list []environmentResponse
		if err := json.Unmarshal(do(h, http.MethodGet, "/environments", "").Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		var ret []string
		for _, e := range list {
			ret = append(ret, e.Name)
		}
		return ret
	}

	stagingKey := as(authz.Principal{APIKey: "k", Environment: "staging", Roles: []string{authz.RoleDeveloper}})
	if got := strings.Join(names(stagingKey), ","); got != "staging" {
		t.Errorf("a key bound to staging sees %q, want staging", got)
	}
	if rec := do(stagingKey, http.MethodGet, "/environments/octo", ""); rec.Code != http.StatusNotFound {
		t.Errorf("a key bound to staging reading the default: %d, want 404", rec.Code)
	}
	person := as(authz.Principal{EnvRoles: map[string][]string{"staging": {authz.RoleDeveloper}}})
	if got := strings.Join(names(person), ","); got != "staging" {
		t.Errorf("a person with a role only in staging sees %q, want staging", got)
	}
	admin := as(authz.Principal{Roles: []string{authz.RoleAdmin}})
	if got := strings.Join(names(admin), ","); got != "octo,staging" {
		t.Errorf("an administrator sees %q, want every environment", got)
	}
}
