// Package api is the stats sidecar's HTTP surface.
//
// It is deliberately smaller than the dev sidecar's, and unauthenticated, for
// one reason: nothing drives this sidecar. It has no commands, no peer that
// sends it anything, and no token in its environment to check one against. It
// samples the container beside it and writes to a cache. What it serves is the
// two probes the kubelet needs and one status page for whoever is debugging it.
//
// # Why /readyz is unconditional
//
// This is the load-bearing decision in the package, so it is stated here rather
// than only at the handler.
//
// The sidecar is injected as a native sidecar — a restartable init container —
// and Kubernetes folds a restartable init container's readiness into the POD's
// readiness. A /readyz that reported the sidecar's real state would therefore
// take the integration out of its Service endpoints whenever this sidecar was
// unhappy. A Redis outage would stop production traffic to every integration in
// the namespace at once, in order to protect the collection of statistics.
//
// That trade is never worth making. Observability must not be able to break the
// thing it observes, so both probes answer 200 whenever the process is running,
// and everything that actually went wrong is reported through /status and the
// log instead. The orchestrator declines to attach a readiness probe at all
// (orchestrator/internal/kube/statssidecar.go), which is belt to this brace.
package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

const (
	// probeBody is what the probes answer with. A body rather than an empty 200
	// so `kubectl exec ... curl` says which endpoint replied.
	probeBody = "ok"
)

// Reporter is the live state a status page shows. Declared here, in the
// consumer; satisfied by the sampler in package main.
type Reporter interface {
	Report() Status
}

// Status is what the sidecar can say about itself.
//
// Every counter is cumulative since process start, and the two Last* strings
// hold the most recent failure of each kind. Failures are reported rather than
// signalled, because none of them is a reason to fail a probe.
type Status struct {
	Pod          string `json:"pod"`
	DeploymentID string `json:"deploymentId"`
	MetricsURL   string `json:"metricsUrl"`

	SampleInterval string `json:"sampleInterval"`
	RollupInterval string `json:"rollupInterval"`
	Retention      string `json:"retention"`

	Scrapes       int64 `json:"scrapes"`
	ScrapeErrors  int64 `json:"scrapeErrors"`
	Writes        int64 `json:"writes"`
	WriteErrors   int64 `json:"writeErrors"`
	BucketsClosed int64 `json:"bucketsClosed"`
	Series        int   `json:"series"`
	Generation    int   `json:"generation"`

	// OpenBucketStart is the aligned start of the bucket in progress, RFC3339, or
	// empty before the first sample lands.
	OpenBucketStart string `json:"openBucketStart,omitempty"`
	OpenBucketRows  int    `json:"openBucketSamples"`

	LastScrapeError string `json:"lastScrapeError,omitempty"`
	LastWriteError  string `json:"lastWriteError,omitempty"`
	LastWriteAt     string `json:"lastWriteAt,omitempty"`
}

// Handler serves the sidecar's endpoints.
type Handler struct {
	reporter Reporter
}

// NewHandler returns a Handler reading live state from reporter.
func NewHandler(r Reporter) *Handler { return &Handler{reporter: r} }

// Endpoints lists the routes, for the startup log. There is no OpenAPI document
// for the sidecars, so the log line is the API's documentation — the same device
// sidecars/dev/internal/api uses.
func Endpoints() []string {
	return []string{"GET /healthz", "GET /readyz", "GET /status"}
}

// Register attaches the routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.probe)
	mux.HandleFunc("GET /readyz", h.probe)
	mux.HandleFunc("GET /status", h.status)
}

// probe answers both liveness and readiness, unconditionally. See the package
// doc: a truthful readiness here would let a cache outage stop production
// traffic, and the two questions have the same answer for a process whose
// failures are all recoverable.
func (h *Handler) probe(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(probeBody))
}

// status reports what the sidecar is doing. Unauthenticated because it carries
// nothing secret — counters, intervals, and the pod and deployment ids, both of
// which are already labels on the pod serving it.
func (h *Handler) status(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(h.reporter.Report()); err != nil {
		// The header is already written, so there is nowhere to report this but
		// the connection, which is already broken.
		return
	}
}

// Counters is the mutable half of a Status: the tallies the sampler bumps as it
// runs, behind a mutex because the HTTP server reads them from its own
// goroutines while the sampler writes them from its.
type Counters struct {
	mu sync.Mutex

	scrapes       int64
	scrapeErrors  int64
	writes        int64
	writeErrors   int64
	bucketsClosed int64

	lastScrapeError string
	lastWriteError  string
	lastWriteAt     time.Time
}

// Scraped records a successful scrape.
func (c *Counters) Scraped() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scrapes++
	c.lastScrapeError = ""
}

// ScrapeFailed records a failed scrape and its cause.
func (c *Counters) ScrapeFailed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scrapeErrors++
	c.lastScrapeError = err.Error()
}

// Wrote records a successful write.
func (c *Counters) Wrote(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes++
	c.lastWriteAt = at
	c.lastWriteError = ""
}

// WriteFailed records a failed write and its cause.
func (c *Counters) WriteFailed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeErrors++
	c.lastWriteError = err.Error()
}

// BucketClosed records a completed rollup bucket.
func (c *Counters) BucketClosed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bucketsClosed++
}

// Fill copies the counters into a Status.
func (c *Counters) Fill(s *Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s.Scrapes, s.ScrapeErrors = c.scrapes, c.scrapeErrors
	s.Writes, s.WriteErrors = c.writes, c.writeErrors
	s.BucketsClosed = c.bucketsClosed
	s.LastScrapeError, s.LastWriteError = c.lastScrapeError, c.lastWriteError
	if !c.lastWriteAt.IsZero() {
		s.LastWriteAt = c.lastWriteAt.UTC().Format(time.RFC3339)
	}
}
