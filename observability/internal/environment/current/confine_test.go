package current

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type deployments map[string]string

func (d deployments) DeploymentEnvironment(_ context.Context, id string) (string, bool, error) {
	env, ok := d[id]
	return env, ok, nil
}

func TestConfineStats(t *testing.T) {
	const staging = "22222222-2222-2222-2222-222222222222"
	owners := deployments{staging: "staging"}
	cases := []struct {
		environment, path string
		status            int
	}{
		{"staging", "/stats/" + staging + "/pods", http.StatusNoContent},
		{"", "/stats/" + staging + "/pods", http.StatusNotFound},
		{"", "/logs", http.StatusNoContent},
		// Spellings Postgres reads as the same uuid are checked as that deployment.
		{"", "/stats/" + strings.ReplaceAll(staging, "-", "") + "/pods", http.StatusNotFound},
		{"", "/stats/{" + staging + "}/pods", http.StatusNotFound},
		// A deleted deployment's stats outlive its row, and nothing else says whose.
		{"", "/stats/33333333-3333-3333-3333-333333333333/pods", http.StatusNotFound},
	}
	for _, tc := range cases {
		h := Confine(owners, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req = req.WithContext(With(req.Context(), tc.environment, tc.environment != ""))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Errorf("%q in %q: status = %d, want %d", tc.path, tc.environment, rec.Code, tc.status)
		}
	}
}
