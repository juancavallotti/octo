package runtimeprobe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newProber points a Prober at srv's host:port, the way it would be pointed at the
// runtime container's admin port over shared loopback.
func newProber(srv *httptest.Server) *Prober {
	return New(strings.TrimPrefix(srv.URL, "http://"))
}

// adminServer stands in for the runtime's admin port, answering /healthz and
// /readyz the way runtime/services/observability does.
func adminServer(readyStatus int, readyBody string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(readyStatus)
		_, _ = w.Write([]byte(readyBody + "\n"))
	})
	return httptest.NewServer(mux)
}

func TestStatusReady(t *testing.T) {
	srv := adminServer(http.StatusOK, "ready")
	defer srv.Close()

	got := newProber(srv).Status(context.Background())
	if !got.Reachable || !got.Live || !got.Ready {
		t.Errorf("status = %+v, want reachable, live and ready", got)
	}
	if got.State != "ready" {
		t.Errorf("State = %q, want ready", got.State)
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty", got.Error)
	}
}

// A 503 from /readyz is normal operation, and its body is the reason. Losing that
// reason would send whoever asked to the logs to find out what this call already
// knew.
func TestStatusNotReadyCarriesReason(t *testing.T) {
	srv := adminServer(http.StatusServiceUnavailable, "reloading")
	defer srv.Close()

	got := newProber(srv).Status(context.Background())
	if !got.Reachable || !got.Live {
		t.Errorf("status = %+v, want reachable and live", got)
	}
	if got.Ready {
		t.Error("Ready = true, want false on a 503")
	}
	if got.State != "reloading" {
		t.Errorf("State = %q, want reloading", got.State)
	}
}

// An unreachable runtime is an ANSWER, not a failure to answer: during startup it
// is the expected state, and a caller rendering /status needs "not reachable, here
// is why" rather than an error that says nothing about the runtime.
func TestStatusUnreachable(t *testing.T) {
	srv := adminServer(http.StatusOK, "ready")
	addr := strings.TrimPrefix(srv.URL, "http://")
	srv.Close() // nothing is listening now

	got := New(addr).Status(context.Background())
	if got.Reachable || got.Live || got.Ready {
		t.Errorf("status = %+v, want everything false", got)
	}
	if got.Error == "" {
		t.Error("Error is empty, want the connection failure explained")
	}
}

// Live but readiness unreadable: report the liveness result that just succeeded
// rather than discarding it.
func TestStatusLiveButReadyzBroken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	// /readyz not registered: ServeMux answers 404, which is not a readiness answer.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got := newProber(srv).Status(context.Background())
	if !got.Reachable || !got.Live {
		t.Errorf("status = %+v, want reachable and live", got)
	}
	if got.Ready {
		t.Error("Ready = true, want false")
	}
}

func TestMetricsPassthrough(t *testing.T) {
	const body = "octo_up 1\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	got, contentType, err := newProber(srv).Metrics(context.Background())
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if string(got) != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	// Passed through verbatim: the sidecar has no opinion about the exposition
	// format, and re-serialising it would only be a chance to corrupt it.
	if contentType != "text/plain; version=0.0.4" {
		t.Errorf("contentType = %q", contentType)
	}
}

// A runtime started without metrics enabled 404s /metrics. That is a statement
// about the runtime, so it surfaces as an error the caller reports rather than an
// empty success.
func TestMetricsNotEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	if _, _, err := newProber(srv).Metrics(context.Background()); err == nil {
		t.Error("Metrics: want an error when /metrics is not served")
	}
}

func TestAddr(t *testing.T) {
	if got := New("127.0.0.1:39999").Addr(); got != "127.0.0.1:39999" {
		t.Errorf("Addr = %q", got)
	}
}
