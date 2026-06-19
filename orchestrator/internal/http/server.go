package httpx

import (
	"net/http"
	"time"
)

// readHeaderTimeout bounds the time spent reading request headers, mitigating
// slow-header denial-of-service attempts.
const readHeaderTimeout = 10 * time.Second

// NewServer returns an *http.Server with the orchestrator's standard timeouts,
// serving handler on addr.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
}
