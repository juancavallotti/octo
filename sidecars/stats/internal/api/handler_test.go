package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// stubReporter is a fixed Status. Declared here because the Reporter interface
// is declared here too — the handler's only dependency is one method.
type stubReporter struct{ status Status }

func (s stubReporter) Report() Status { return s.status }

func serve(t *testing.T, r Reporter) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(r).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The load-bearing property of this package: neither probe can ever fail.
//
// The sidecar is a restartable init container, and Kubernetes folds such a
// container's readiness into the pod's — so a readiness probe that told the
// truth about a Redis outage would pull every integration out of its Service.
// Observability must not be able to break the thing it observes.
func TestProbesAlwaysSucceed(t *testing.T) {
	// A reporter describing a sidecar in every kind of trouble at once.
	broken := stubReporter{status: Status{
		ScrapeErrors: 9000, WriteErrors: 9000,
		LastScrapeError: "connection refused", LastWriteError: "redis is gone",
	}}
	srv := serve(t, broken)

	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200 even with everything failing", resp.StatusCode)
			}
			if got := resp.Header.Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestStatusReportsFailures(t *testing.T) {
	srv := serve(t, stubReporter{status: Status{
		Pod: "octo-dep-1-abc", DeploymentID: "dep-1",
		Scrapes: 10, ScrapeErrors: 1, Writes: 9, WriteErrors: 2,
		LastWriteError: "redis: connection refused",
	}})

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got Status
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Pod != "octo-dep-1-abc" || got.DeploymentID != "dep-1" {
		t.Errorf("identity = %s/%s, want the pod and deployment", got.DeploymentID, got.Pod)
	}
	if got.WriteErrors != 2 || got.LastWriteError == "" {
		t.Errorf("failures = %d writes / %q, want them reported", got.WriteErrors, got.LastWriteError)
	}
}

func TestEndpointsAreRegistered(t *testing.T) {
	srv := serve(t, stubReporter{})
	for _, ep := range Endpoints() {
		t.Run(ep, func(t *testing.T) {
			// Endpoints are "METHOD /path"; the path is the half to request.
			path := ep[len("GET "):]
			resp, err := http.Get(srv.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusNotFound {
				t.Errorf("%s is advertised by Endpoints() but not registered", ep)
			}
		})
	}
}

// The sampler writes these from its goroutine while the status endpoint reads
// them from the server's, so the counters carry their own lock.
func TestCountersAreConcurrencySafe(t *testing.T) {
	var c Counters
	var wg sync.WaitGroup

	const writers = 8
	const each = 200
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < each; j++ {
				c.Scraped()
				c.Wrote(time.Now())
				c.ScrapeFailed(errors.New("boom"))
				c.WriteFailed(errors.New("bang"))
				c.BucketClosed()
			}
		}()
	}
	// Read concurrently, which is what the status endpoint does.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < writers*each; i++ {
			var s Status
			c.Fill(&s)
		}
	}()
	wg.Wait()

	var got Status
	c.Fill(&got)
	want := int64(writers * each)
	for _, tc := range []struct {
		name string
		got  int64
	}{
		{"scrapes", got.Scrapes}, {"scrape errors", got.ScrapeErrors},
		{"writes", got.Writes}, {"write errors", got.WriteErrors},
		{"buckets", got.BucketsClosed},
	} {
		if tc.got != want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, want)
		}
	}
}

// A success clears the previous failure, so /status shows the current state
// rather than the worst thing that ever happened.
func TestSuccessClearsTheLastError(t *testing.T) {
	var c Counters
	c.ScrapeFailed(errors.New("connection refused"))
	c.WriteFailed(errors.New("redis is gone"))

	var during Status
	c.Fill(&during)
	if during.LastScrapeError == "" || during.LastWriteError == "" {
		t.Fatal("failures should be reported while they are current")
	}

	c.Scraped()
	c.Wrote(time.Now())

	var after Status
	c.Fill(&after)
	if after.LastScrapeError != "" || after.LastWriteError != "" {
		t.Errorf("errors = %q / %q, want both cleared after a success",
			after.LastScrapeError, after.LastWriteError)
	}
	// The tallies are cumulative and must survive the recovery.
	if after.ScrapeErrors != 1 || after.WriteErrors != 1 {
		t.Errorf("error counts = %d / %d, want them to stay at 1",
			after.ScrapeErrors, after.WriteErrors)
	}
}
