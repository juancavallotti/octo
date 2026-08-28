package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/services"
)

// fake is an in-process implementation of the platform API contract, served over
// httptest. It is the module's primary test fixture: capability tests drive the
// real client against it and assert semantics, the way a correct implementation
// would behave.
//
// It is deliberately a whole implementation rather than a set of stub handlers.
// Wire-shape assertions belong in the per-capability tests, which inspect
// recorded requests; what this gives them is a server that behaves, so a test can
// say "a conflicting write returns ErrVersionConflict" instead of "the client
// sends a PUT".
type fake struct {
	t   *testing.T
	srv *httptest.Server
	mux *http.ServeMux

	// discovery is what GET /v1/discovery answers. A test sets it before starting.
	discovery discoveryDocument
	// discoveryFails, when positive, refuses that many discovery calls before
	// answering, for the startup-retry tests.
	discoveryFails int
	// requireToken, when set, refuses any request without this bearer token.
	requireToken string

	mu       sync.Mutex
	requests []recorded
	calls    map[string]int
}

// recorded is one request the fake received, for wire-shape assertions.
type recorded struct {
	method string
	path   string
	query  string
	header http.Header
	body   []byte
}

// newFake starts a fake platform API implementing everything in doc.
func newFake(t *testing.T, doc discoveryDocument) *fake {
	t.Helper()
	f := &fake{t: t, mux: http.NewServeMux(), discovery: doc, calls: map[string]int{}}
	f.mux.HandleFunc("GET /v1/discovery", f.handleDiscovery)
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

// fullDiscovery is a document declaring every feature supported, which most tests
// start from and then narrow.
func fullDiscovery() discoveryDocument {
	return discoveryDocument{
		SpecVersion:    specVersion,
		Implementation: implementation{Name: "fake", Version: "test"},
		Features: featureDocument{
			KV:             kvFeature{featureFlags: featureFlags{Supported: true}},
			Secrets:        secretsFeature{featureFlags: featureFlags{Supported: true}, EncryptedAtRest: true},
			Resources:      featureFlags{Supported: true},
			Leases:         leaseFeature{featureFlags: featureFlags{Supported: true}},
			LeaderElection: leaderFeature{featureFlags: featureFlags{Supported: true}},
			Queues:         queueFeature{featureFlags: featureFlags{Supported: true}, RequestReply: true},
			Topics:         topicFeature{featureFlags: featureFlags{Supported: true}},
			AgentMemory: agentMemoryFeature{
				featureFlags: featureFlags{Supported: true},
				ListThreads:  true, ReadThread: true, Search: true,
			},
			Traces: traceFeature{featureFlags: featureFlags{Supported: true}},
			Logs:   featureFlags{Supported: true},
		},
	}
}

// ServeHTTP records the request, applies the auth gate, then routes it.
func (f *fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.record(r)
	if f.requireToken != "" && r.Header.Get("Authorization") != "Bearer "+f.requireToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	f.mux.ServeHTTP(w, r)
}

// record stores the request for later assertions.
func (f *fake) record(r *http.Request) {
	body := readAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recorded{
		method: r.Method, path: r.URL.Path, query: r.URL.RawQuery,
		header: r.Header.Clone(), body: body,
	})
	f.calls[r.Method+" "+r.URL.Path]++
}

// handleDiscovery answers the discovery call, failing the first discoveryFails
// attempts.
func (f *fake) handleDiscovery(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	if f.discoveryFails > 0 {
		f.discoveryFails--
		f.mu.Unlock()
		http.Error(w, "starting up", http.StatusServiceUnavailable)
		return
	}
	doc := f.discovery
	f.mu.Unlock()
	writeJSON(w, http.StatusOK, doc)
}

// url is the fake's base URL.
func (f *fake) url() string { return f.srv.URL }

// last returns the most recent request whose path ends with suffix, failing the
// test when there is none.
func (f *fake) last(suffix string) recorded {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if strings.HasSuffix(f.requests[i].path, suffix) {
			return f.requests[i]
		}
	}
	f.t.Fatalf("no request recorded with path ending %q; got %v", suffix, f.paths())
	return recorded{}
}

// count returns how many times a "METHOD /path" was called.
func (f *fake) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}

// paths lists what was called, for failure messages.
func (f *fake) paths() []string {
	out := make([]string, 0, len(f.requests))
	for _, r := range f.requests {
		out = append(out, r.method+" "+r.path)
	}
	return out
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// readAll drains a request body.
func readAll(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	body, _ := io.ReadAll(r.Body)
	return body
}

// newTestServices builds the module against the fake, with the environment the
// test needs. It returns the concrete type so tests can reach past the interface.
func newTestServices(t *testing.T, f *fake, env map[string]string) *Services {
	t.Helper()
	t.Setenv(envURL, f.url())
	t.Setenv(envInstanceID, "test-instance")
	t.Setenv(envDeploymentID, "test-deployment")
	for k, v := range env {
		t.Setenv(k, v)
	}
	svc, err := New(t.Context(), services.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	concrete, ok := svc.(*Services)
	if !ok {
		t.Fatalf("New returned %T, want *Services", svc)
	}
	return concrete
}
