package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/juancavallotti/octo/runtime/services"
)

// Route paths served on the admin port.
const (
	pathIndex   = "/"
	pathHealthz = "/healthz"
	pathReadyz  = "/readyz"
	pathMetrics = "/metrics"
)

// handler builds the admin mux. It is a mux of its own, not the default one, so
// nothing a dependency registers globally leaks onto the admin port.
func (s *Service) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(pathHealthz, s.handleHealthz)
	mux.HandleFunc(pathReadyz, s.handleReadyz)
	if s.collectors != nil {
		mux.Handle(pathMetrics, promhttp.HandlerFor(s.collectors.registry, promhttp.HandlerOpts{
			// A collector that fails should say so in the response rather than
			// serving a half-scrape that reads as real data.
			ErrorHandling: promhttp.HTTPErrorOnError,
			Registry:      s.collectors.registry,
		}))
	}
	mux.HandleFunc(pathIndex, s.handleIndex)
	return mux
}

// handleHealthz answers liveness. It is unconditional by design: liveness asks
// whether the process is wedged, and this handler running at all is the answer. A
// gate here could only ever report "yes" or fail to be reached — and failing to be
// reached is exactly what a liveness probe already detects.
func (s *Service) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

// handleReadyz answers readiness: 200 once every connector and flow of the
// current generation started, and 503 naming the reason otherwise. The reason is
// in the body because "why is this pod not ready" is the next question, and a bare
// 503 sends whoever asked to the logs to find out.
func (s *Service) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	state := services.StateStarting
	if s.health != nil {
		state = s.health.State()
	}
	if state == services.StateReady {
		writePlain(w, http.StatusOK, string(state))
		return
	}
	writePlain(w, http.StatusServiceUnavailable, string(state))
}

// handleIndex lists what is mounted, so someone who curls the admin port finds the
// endpoints instead of a 404. Anything not matched by a real route lands here, so
// unknown paths 404 rather than being answered by the index.
func (s *Service) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != pathIndex {
		writePlain(w, http.StatusNotFound, "not found")
		return
	}
	writePlain(w, http.StatusOK, s.index())
}

// index is the body of the root page: one line per mounted endpoint. /metrics is
// listed only when it is served, so the page never advertises a 404.
func (s *Service) index() string {
	body := "octo admin\n\n" +
		pathHealthz + "  liveness\n" +
		pathReadyz + "   readiness\n"
	if s.collectors != nil {
		body += pathMetrics + "  prometheus metrics\n"
	}
	return body
}

// writePlain writes a text/plain response with a trailing newline, so the output
// is readable straight out of curl.
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// Probes are polled constantly and their answer changes with the runtime's
	// state; a cached readiness answer is worse than no answer.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
