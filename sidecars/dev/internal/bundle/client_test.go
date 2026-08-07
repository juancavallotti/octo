package bundle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient points a Client at srv for dev run "run-1" with token "tok".
func newTestClient(srv *httptest.Server) *Client {
	return NewClient(srv.URL, "run-1", "tok")
}

func TestFetchReturnsBundle(t *testing.T) {
	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotAccept = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"definition": "name: demo\n",
			"generation": "g7",
			"resources": [{"name": ".env.dev", "kind": "env", "content": "A=1\n"}]
		}`))
	}))
	defer srv.Close()

	b, err := newTestClient(srv).Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if gotPath != "/devruns/run-1/bundle" {
		t.Errorf("path = %q, want /devruns/run-1/bundle", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q, want Bearer tok", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if b.Definition != "name: demo\n" {
		t.Errorf("Definition = %q", b.Definition)
	}
	if b.Generation != "g7" {
		t.Errorf("Generation = %q, want g7", b.Generation)
	}
	if len(b.Resources) != 1 || b.Resources[0].Name != ".env.dev" || b.Resources[0].Content != "A=1\n" {
		t.Errorf("Resources = %+v", b.Resources)
	}
}

// A trailing slash on ORCHESTRATOR_URL is the kind of thing a values file supplies,
// and it must not produce a double slash in the path.
func TestNewClientTrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL+"/", "run-1", "tok").Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if gotPath != "/devruns/run-1/bundle" {
		t.Errorf("path = %q, want /devruns/run-1/bundle", gotPath)
	}
}

// The status mapping is the contract the reload coordinator branches on, so each
// class is asserted rather than sampled: only 404 may ever be terminal.
func TestFetchStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"gone", http.StatusNotFound, ErrGone},
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized},
		{"forbidden", http.StatusForbidden, ErrUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			_, err := newTestClient(srv).Fetch(context.Background())
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A 5xx must be an ordinary error: treating it as terminal would let a rolling
// orchestrator delete every dev run in the cluster.
func TestFetchServerErrorIsRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).Fetch(context.Background())
	if err == nil {
		t.Fatal("Fetch: want error")
	}
	if errors.Is(err, ErrGone) || errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want a plain retryable error", err)
	}
	// The body is where the orchestrator explains itself; losing it leaves a bare
	// status code as the only clue.
	if !strings.Contains(err.Error(), "upstream down") {
		t.Errorf("err = %v, want it to carry the response body", err)
	}
}

func TestFetchRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"definition":`))
	}))
	defer srv.Close()

	if _, err := newTestClient(srv).Fetch(context.Background()); err == nil {
		t.Fatal("Fetch: want a decode error")
	}
}

func TestExpirePosts(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := newTestClient(srv).Expire(context.Background()); err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/devruns/run-1/expire" {
		t.Errorf("path = %q, want /devruns/run-1/expire", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

// Expiring something already gone is the outcome being asked for, not a failure —
// otherwise the caller retries a request that has already succeeded.
func TestExpireTreatsNotFoundAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if err := newTestClient(srv).Expire(context.Background()); err != nil {
		t.Errorf("Expire: %v, want nil for an already-gone run", err)
	}
}

func TestExpireReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := newTestClient(srv).Expire(context.Background()); err == nil {
		t.Error("Expire: want error on 500")
	}
}

// A failed Expire must carry the orchestrator's explanation, not just the status:
// the body has to survive the status check rather than being drained before it.
func TestExpireErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("run is still draining"))
	}))
	defer srv.Close()

	err := newTestClient(srv).Expire(context.Background())
	if err == nil {
		t.Fatal("Expire: want error on 409")
	}
	if !strings.Contains(err.Error(), "run is still draining") {
		t.Errorf("error = %q, want it to include the response body", err)
	}
}
