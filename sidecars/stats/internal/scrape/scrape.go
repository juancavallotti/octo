// Package scrape reads the octo runtime's Prometheus endpoint over loopback and
// parses it into metric families.
//
// The two containers share a network namespace, so the runtime's admin port is
// reachable at 127.0.0.1 and needs no service, no credential and no exposure —
// the same relationship sidecars/dev/internal/runtimeprobe describes. What
// differs is what happens to the bytes: the dev sidecar passes /metrics through
// verbatim because it has no opinion about the contents, and this one parses,
// because the collapse rules downstream turn on whether a series is a counter or
// a gauge and only the exposition's TYPE lines carry that.
//
// The endpoint exists only when the runtime was started with --metrics
// (OCTO_METRICS), which defaults to off; the orchestrator sets it on any pod it
// gives this sidecar to. A 404 is therefore a misconfiguration rather than a
// transient fault, and is reported as its own error so the log says which.
package scrape

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

const (
	// scrapeTimeout bounds one scrape. This is loopback against a registry walk,
	// so anything approaching this means the runtime is in trouble; sampling
	// again next tick is better than blocking the sampler on a wedged handler.
	scrapeTimeout = 5 * time.Second
	// maxBytes bounds what one scrape reads, matching the dev sidecar's
	// passthrough limit (runtimeprobe.maxMetricsBytes). A runtime exposing more
	// than this has a cardinality problem, and reading it into memory once a
	// second would make that problem the sidecar's too.
	maxBytes = 4 << 20
	// errSnippetBytes bounds how much of an unexpected response body is quoted
	// back in an error.
	errSnippetBytes = 512

	metricsPath = "/metrics"
)

// nameValidation is how strictly a scraped metric name is checked.
//
// UTF-8 rather than legacy, which is prometheus/common's own default and the
// permissive one: it accepts every name legacy validation would and does not
// reject a runtime that starts emitting a name with a dot in it. Rejecting here
// would drop a whole scrape over a name this sidecar only ever stores and never
// interprets.
const nameValidation = model.UTF8Validation

// ErrMetricsDisabled is returned when the runtime answers 404, which is what it
// does when it was started without --metrics. Named because it is the one
// failure that will not fix itself: retrying forever would fill the log with a
// timeout's vocabulary for a configuration mistake.
var ErrMetricsDisabled = errors.New("scrape: runtime is not serving metrics (started without --metrics)")

// Scraper reads one runtime's admin port.
type Scraper struct {
	url  string
	http *http.Client
}

// New returns a Scraper for the admin address addr, e.g. "127.0.0.1:39999".
func New(addr string) *Scraper {
	return &Scraper{
		url:  "http://" + addr + metricsPath,
		http: &http.Client{Timeout: scrapeTimeout},
	}
}

// URL is the endpoint being scraped, for logs and diagnostics.
func (s *Scraper) URL() string { return s.url }

// Scrape fetches and parses the runtime's metrics.
//
// The returned map is keyed by metric family name, and every family carries the
// TYPE the runtime declared — which is the reason this parses at all.
func (s *Scraper) Scrape(ctx context.Context) (map[string]*dto.MetricFamily, error) {
	ctx, cancel := context.WithTimeout(ctx, scrapeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("scrape: build request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("scrape: get %s: %w", s.url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrMetricsDisabled
	}
	body := io.LimitReader(resp.Body, maxBytes)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("scrape: %s returned %d: %s", s.url, resp.StatusCode, snippet(body))
	}

	// The parser is per-call rather than reused: expfmt.TextParser carries the
	// state of one parse, and sharing one across scrapes would let a malformed
	// exposition poison every scrape that followed it. Its zero value is not
	// usable, so it is constructed rather than declared.
	parser := expfmt.NewTextParser(nameValidation)
	families, err := parser.TextToMetricFamilies(body)
	if err != nil {
		return nil, fmt.Errorf("scrape: parse %s: %w", s.url, err)
	}
	return families, nil
}

// snippet reads a bounded prefix of an unexpected body for an error message.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, errSnippetBytes))
	if err != nil || len(b) == 0 {
		return "<no body>"
	}
	return string(b)
}
