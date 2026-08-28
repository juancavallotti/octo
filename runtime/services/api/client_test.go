package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// url substitutes placeholders positionally and escapes each argument, which is
// what keeps a subject or namespace containing a slash from inventing a path
// segment.
func TestURLBuildsFromTheRouteTemplate(t *testing.T) {
	c := &client{base: "https://platform.example"}
	cases := []struct {
		route route
		args  []string
		want  string
	}{
		{routeDiscovery, nil, "https://platform.example/v1/discovery"},
		{routeKVGet, []string{"user_secrets"}, "https://platform.example/v1/kv/user_secrets/entry"},
		{routeQueuePublish, []string{"orders/new"}, "https://platform.example/v1/queues/orders%2Fnew/publish"},
		{
			routeTopicUnsubscribe, []string{"events", "sub-1"},
			"https://platform.example/v1/topics/events/subscriptions/sub-1",
		},
	}
	for _, tc := range cases {
		if got := c.url(tc.route, tc.args...); got != tc.want {
			t.Errorf("url(%s, %v) = %s, want %s", tc.route.path, tc.args, got, tc.want)
		}
	}
}

// Empty query values are dropped rather than sent blank: an implementer reading
// userId="" cannot tell it from "not given", and the two mean different things.
func TestQueryOmitsEmptyValues(t *testing.T) {
	got := query("https://x/v1/kv/user/entry", "key", "a/b", "userId", "")
	if got != "https://x/v1/kv/user/entry?key=a%2Fb" {
		t.Fatalf("query = %s", got)
	}
	if got := query("https://x/v1/discovery", "userId", ""); got != "https://x/v1/discovery" {
		t.Fatalf("query with nothing to add = %s, want the bare URL", got)
	}
}

// Every request carries the identity headers, so an implementer can scope storage
// and correlate their logs without the runtime needing a per-call convention.
func TestRequestsCarryIdentityHeaders(t *testing.T) {
	f := newFake(t, fullDiscovery())
	svc := newTestServices(t, f, nil)
	_ = svc

	req := f.last("/v1/discovery")
	if got := req.header.Get("X-Octo-Instance"); got != "test-instance" {
		t.Errorf("X-Octo-Instance = %q, want test-instance", got)
	}
	if got := req.header.Get("X-Octo-Deployment"); got != "test-deployment" {
		t.Errorf("X-Octo-Deployment = %q, want test-deployment", got)
	}
	if req.header.Get("X-Octo-Request-Id") == "" {
		t.Error("X-Octo-Request-Id is empty; an implementer has nothing to correlate on")
	}
}

// A bearer token is the common case and rides on every request.
func TestBearerToken(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.requireToken = "s3cret"
	newTestServices(t, f, map[string]string{envToken: "s3cret"})

	if got := f.last("/v1/discovery").header.Get("Authorization"); got != "Bearer s3cret" {
		t.Fatalf("Authorization = %q", got)
	}
}

// A token file is re-read when it changes, so a rotated credential is picked up
// without a restart — Cloud Run secret volumes and projected service-account
// tokens both rotate in place under a running process.
func TestTokenFileIsRereadOnRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &client{tokenFile: path}

	if got := c.bearer(); got != "first" {
		t.Fatalf("bearer = %q, want first (and trimmed)", got)
	}
	// A modification time has to actually differ for the change to be seen.
	rotated := time.Now().Add(time.Second)
	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, rotated, rotated); err != nil {
		t.Fatal(err)
	}
	if got := c.bearer(); got != "second" {
		t.Fatalf("bearer after rotation = %q, want second", got)
	}
}

// An unreadable token file keeps the last good token: a credential that
// momentarily cannot be read is better answered with the previous one than with
// none.
func TestTokenFileFailureKeepsTheLastToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("good"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &client{tokenFile: path}
	if got := c.bearer(); got != "good" {
		t.Fatalf("bearer = %q", got)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := c.bearer(); got != "good" {
		t.Fatalf("bearer after the file vanished = %q, want the last good token", got)
	}
}

// Extra headers are the escape hatch that keeps the auth model small: an API key,
// a tenant id or a mesh header would each otherwise need its own variable.
func TestExtraHeaders(t *testing.T) {
	f := newFake(t, fullDiscovery())
	newTestServices(t, f, map[string]string{
		envHeaders: "X-Api-Key: abc123\nX-Tenant: acme",
	})
	req := f.last("/v1/discovery")
	if got := req.header.Get("X-Api-Key"); got != "abc123" {
		t.Errorf("X-Api-Key = %q", got)
	}
	if got := req.header.Get("X-Tenant"); got != "acme" {
		t.Errorf("X-Tenant = %q", got)
	}
}

func TestHeaderListRejectsMalformedEntries(t *testing.T) {
	if _, err := parseHeaders("X-Api-Key abc"); err == nil {
		t.Fatal("parseHeaders err = nil, want a failure naming the expected form")
	}
	got, err := parseHeaders(" X-A: 1 , X-B: 2 ")
	if err != nil {
		t.Fatal(err)
	}
	if got["X-A"] != "1" || got["X-B"] != "2" {
		t.Fatalf("parseHeaders = %v", got)
	}
}

// An idempotent route retries a transient failure, so a platform restarting
// behind a load balancer does not surface as a flow error.
func TestIdempotentRouteRetriesTransientFailures(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.discoveryFails = 0
	var attempts int
	f.mux.HandleFunc("GET /v1/resources/content", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 2 {
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	svc := newTestServices(t, f, nil)

	//nolint:bodyclose // drainClose below closes it
	resp, err := svc.client.do(t.Context(), routeResourceContent,
		svc.client.url(routeResourceContent), nil, nil, time.Second)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer drainClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %s, want 200 after a retry", resp.Status)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

// A non-idempotent route does not retry: publishing twice is worse than failing
// once, because only one of those is visible to the caller.
func TestNonIdempotentRouteDoesNotRetry(t *testing.T) {
	f := newFake(t, fullDiscovery())
	var attempts int
	f.mux.HandleFunc("POST /v1/traces", func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "try again", http.StatusServiceUnavailable)
	})
	svc := newTestServices(t, f, nil)

	err := svc.client.json(t.Context(), routeTraces, svc.client.url(routeTraces),
		map[string]any{"records": []any{}}, nil, time.Second)
	if err == nil {
		t.Fatal("json err = nil, want the failure surfaced")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: a retried publish would duplicate the record", attempts)
	}
}

// A 501 is its own signal, distinct from any other failure: it is how a server
// says "this route is not implemented", which latches the feature off.
func TestNotImplementedIsDistinguishable(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("GET /v1/resources/content", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusNotImplemented)
	})
	svc := newTestServices(t, f, nil)

	err := svc.client.json(t.Context(), routeResourceContent,
		svc.client.url(routeResourceContent), nil, nil, time.Second)
	if !isNotImplemented(err) {
		t.Fatalf("err = %v, want the not-implemented sentinel", err)
	}
}

// An error body is quoted back, bounded, because the reader is the person
// implementing the server and the body is where they put the reason.
func TestErrorsQuoteTheServersReason(t *testing.T) {
	f := newFake(t, fullDiscovery())
	f.mux.HandleFunc("GET /v1/resources/content", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bucket 'octo' does not exist", http.StatusInternalServerError)
	})
	svc := newTestServices(t, f, nil)

	err := svc.client.json(t.Context(), routeResourceContent,
		svc.client.url(routeResourceContent), nil, nil, time.Second)
	if err == nil || !strings.Contains(err.Error(), "bucket 'octo' does not exist") {
		t.Fatalf("err = %v, want the server's reason quoted", err)
	}
	if !strings.Contains(err.Error(), string(FeatureResources)) {
		t.Fatalf("err = %v, want the feature named so the reader knows which handler", err)
	}
}

// The latch says a feature is off once, loudly, and stays off.
func TestLatchIsSticky(t *testing.T) {
	l := &latch{feature: FeatureKV}
	if !l.live() {
		t.Fatal("a fresh latch reports not live")
	}
	l.mark()
	l.mark()
	if l.live() {
		t.Fatal("latch reports live after being marked")
	}
}
