package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A liveness probe that needs the database would restart the pod for the whole
// time Postgres is coming up, which is the failure this asserts against: the
// route has to answer with a nil handle, not merely with a live one.
func TestHealthzAnswersWithoutADatabase(t *testing.T) {
	srv, err := newServer(nil)
	if err != nil {
		t.Fatalf("newServer(nil): %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Errorf("GET /healthz body = %q, want %q", got, "ok")
	}
}

func TestEnvOr(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		fallback string
		want     string
	}{
		{"unset falls back", "", "8093", "8093"},
		{"empty falls back", "", "8093", "8093"},
		{"set wins", "9000", "8093", "9000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("IAM_TEST_ENV_OR", tt.env)
			if got := envOr("IAM_TEST_ENV_OR", tt.fallback); got != tt.want {
				t.Errorf("envOr() = %q, want %q", got, tt.want)
			}
		})
	}
}
