package current

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		{"a missing row is the handler's to report", "staging", "/integrations/" + missing, http.StatusNoContent},
		{"a missing deployment is refused, its stores have no owner", "staging", "/deployments/" + missing + "/kv/user/k", http.StatusNotFound},
		{"a non-id segment", "staging", "/integrations/bundle", http.StatusNoContent},
		{"both ids checked", "staging", "/folders/" + stagingFolder + "/integrations/" + prodIntegration, http.StatusNotFound},
		{"both ids in the environment", "staging", "/folders/" + stagingFolder + "/integrations/" + stagingIntegration, http.StatusNoContent},
		{"a collection", "staging", "/integrations", http.StatusNoContent},
		// Spellings Postgres reads as the same uuid; each is checked as that row.
		{"an id without hyphens", "staging", "/integrations/" + strings.ReplaceAll(prodIntegration, "-", ""), http.StatusNotFound},
		{"an id in braces", "staging", "/integrations/{" + prodIntegration + "}", http.StatusNotFound},
		{"an id in capitals", "staging", "/integrations/" + strings.ToUpper(prodIntegration), http.StatusNotFound},
		{"an id hyphenated elsewhere", "staging", "/snapshots/" + regroup(prodIntegration), http.StatusNotFound},
		{"its own id without hyphens", "staging", "/integrations/" + strings.ReplaceAll(stagingIntegration, "-", ""), http.StatusNoContent},
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

// regroup writes a uuid with a hyphen after every fourth digit, a spelling
// Postgres accepts.
func regroup(id string) string {
	hex := strings.ReplaceAll(id, "-", "")
	var ret []string
	for i := 0; i < len(hex); i += 4 {
		ret = append(ret, hex[i:i+4])
	}
	return strings.Join(ret, "-")
}
