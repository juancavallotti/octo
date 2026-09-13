package httpx

import (
	"net/http"
	"time"
)

const (
	// readHeaderTimeout bounds the time spent reading request headers, mitigating
	// slow-header denial-of-service attempts.
	readHeaderTimeout = 10 * time.Second
	// readTimeout bounds the headers and the body together. DecodeJSON caps how
	// large a body may be and not how slowly it may arrive, so without this a
	// client that trickles one byte at a time holds a connection and its goroutine
	// for as long as it likes.
	readTimeout = 30 * time.Second
	// writeTimeout bounds the response. Nothing here streams — every route answers
	// with a small JSON document — so one bound covers them all.
	writeTimeout = 30 * time.Second
	// idleTimeout bounds a kept-alive connection between requests, so a client
	// that opens many and then goes quiet does not hold them open indefinitely.
	idleTimeout = 120 * time.Second
)

// NewServer returns an *http.Server with the service's standard timeouts,
// serving handler on addr.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}
