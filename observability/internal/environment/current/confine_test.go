package current

import (
	"context"
	"net/http"
	"net/http/httptest"
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
