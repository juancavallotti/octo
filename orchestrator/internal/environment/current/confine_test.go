package current

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	prodIntegration    = "11111111-1111-1111-1111-111111111111"
	stagingIntegration = "22222222-2222-2222-2222-222222222222"
	stagingFolder      = "33333333-3333-3333-3333-333333333333"
	missing            = "99999999-9999-9999-9999-999999999999"
)

type rows map[string]string

func (r rows) EnvironmentOf(_ context.Context, _, id string) (string, bool, error) {
	env, ok := r[id]
	return env, ok, nil
}

func TestConfine(t *testing.T) {
	owners := rows{prodIntegration: "", stagingIntegration: "staging", stagingFolder: "staging"}
	cases := []struct {
		name, environment, path string
		status                  int
	}{
		{"its own environment", "staging", "/integrations/" + stagingIntegration + "/files", http.StatusNoContent},
		{"another environment's integration", "staging", "/integrations/" + prodIntegration, http.StatusNotFound},
		{"the default reaching into staging", "", "/integrations/" + stagingIntegration + "/resources", http.StatusNotFound},
		{"a missing row is the handler's to report", "staging", "/deployments/" + missing, http.StatusNoContent},
		{"a non-id segment", "staging", "/integrations/bundle", http.StatusNoContent},
		{"both ids checked", "staging", "/folders/" + stagingFolder + "/integrations/" + prodIntegration, http.StatusNotFound},
		{"both ids in the environment", "staging", "/folders/" + stagingFolder + "/integrations/" + stagingIntegration, http.StatusNoContent},
		{"a collection", "staging", "/integrations", http.StatusNoContent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := Confine(owners, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req = req.WithContext(With(req.Context(), tc.environment, tc.environment != ""))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}
