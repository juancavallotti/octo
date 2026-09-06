package scrape

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

// exposition is a small but representative slice of what the runtime serves:
// one octo counter with labels, one gauge, one histogram, and a process
// collector series.
const exposition = `# HELP octo_flow_messages_total Messages a flow finished with, by outcome.
# TYPE octo_flow_messages_total counter
octo_flow_messages_total{flow="orders",outcome="ok"} 42
# HELP octo_flow_in_flight Messages currently being processed.
# TYPE octo_flow_in_flight gauge
octo_flow_in_flight{flow="orders"} 2
# HELP octo_flow_duration_seconds How long a flow took.
# TYPE octo_flow_duration_seconds histogram
octo_flow_duration_seconds_bucket{flow="orders",outcome="ok",le="0.005"} 3
octo_flow_duration_seconds_bucket{flow="orders",outcome="ok",le="+Inf"} 5
octo_flow_duration_seconds_sum{flow="orders",outcome="ok"} 0.11
octo_flow_duration_seconds_count{flow="orders",outcome="ok"} 5
# HELP process_resident_memory_bytes Resident memory size in bytes.
# TYPE process_resident_memory_bytes gauge
process_resident_memory_bytes 3.2768e+07
`

// serve starts a stub runtime admin endpoint and returns a Scraper aimed at it.
func serve(t *testing.T, h http.HandlerFunc) *Scraper {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(strings.TrimPrefix(srv.URL, "http://"))
}

func TestScrapeParsesExposition(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(exposition))
	})

	families, err := s.Scrape(context.Background())
	if err != nil {
		t.Fatalf("Scrape: %v", err)
	}

	tests := []struct {
		family string
		want   dto.MetricType
	}{
		{"octo_flow_messages_total", dto.MetricType_COUNTER},
		{"octo_flow_in_flight", dto.MetricType_GAUGE},
		{"octo_flow_duration_seconds", dto.MetricType_HISTOGRAM},
		{"process_resident_memory_bytes", dto.MetricType_GAUGE},
	}
	for _, tc := range tests {
		t.Run(tc.family, func(t *testing.T) {
			f, ok := families[tc.family]
			if !ok {
				t.Fatalf("family missing from scrape")
			}
			if f.GetType() != tc.want {
				t.Errorf("type = %v, want %v", f.GetType(), tc.want)
			}
		})
	}

	// The TYPE lines are the reason this parses rather than passing bytes
	// through, so confirm a value survives with them.
	h := families["octo_flow_duration_seconds"].GetMetric()[0].GetHistogram()
	if h.GetSampleCount() != 5 || h.GetSampleSum() != 0.11 {
		t.Errorf("histogram = count %d sum %v, want 5 / 0.11", h.GetSampleCount(), h.GetSampleSum())
	}
}

// A 404 means the runtime was started without --metrics. It gets its own error
// because it will not fix itself and should not be logged as a transient fault.
func TestScrapeMetricsDisabled(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, &http.Request{})
	})
	_, err := s.Scrape(context.Background())
	if !errors.Is(err, ErrMetricsDisabled) {
		t.Fatalf("err = %v, want ErrMetricsDisabled", err)
	}
}

func TestScrapeErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{
			name: "server error quotes the body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("registry gather failed"))
			},
			want: "registry gather failed",
		},
		{
			name: "malformed exposition",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("# TYPE bad counter\nbad{unclosed\n"))
			},
			want: "parse",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := serve(t, tc.handler).Scrape(context.Background())
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A response past the limit is refused rather than parsed.
//
// A prefix of a valid exposition is usually itself valid, so a plain
// io.LimitReader would hand the parser a truncated body that parses cleanly —
// and a sample silently missing its tail is worse than no sample, because the
// encoder reads the absent series as gaps and the history tier records that a
// flow stopped reporting.
func TestScrapeRefusesAnOversizedResponse(t *testing.T) {
	// Valid exposition, repeated until it is over the limit. Every prefix at a
	// line boundary parses, which is the trap being tested.
	one := "# TYPE big_%d_total counter\nbig_%d_total 1\n"
	var b strings.Builder
	for i := 0; b.Len() <= maxBytes; i++ {
		fmt.Fprintf(&b, one, i, i)
	}
	body := b.String()

	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})
	_, err := s.Scrape(context.Background())
	if err == nil {
		t.Fatal("expected an oversized response to be refused, not truncated")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("err = %q, want it to say the sample was refused as truncated", err)
	}
}

// Right up to the limit is fine; only past it is refused.
func TestScrapeAcceptsUpToTheLimit(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, exposition)
	})
	if _, err := s.Scrape(context.Background()); err != nil {
		t.Fatalf("an ordinary response was refused: %v", err)
	}
}

// A cancelled context stops a scrape rather than running it to the timeout.
func TestScrapeHonoursContext(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(exposition))
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Scrape(ctx); err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
}

// An unreachable runtime is an ordinary error naming the URL, not a panic: the
// admin port is not up yet during pod startup and the sampler retries.
func TestScrapeUnreachable(t *testing.T) {
	// Port 1 on loopback: reserved, and nothing in a test environment binds it.
	_, err := New("127.0.0.1:1").Scrape(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1/metrics") {
		t.Errorf("err = %q, want it to name the URL", err)
	}
}
