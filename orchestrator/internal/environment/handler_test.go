package environment

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
