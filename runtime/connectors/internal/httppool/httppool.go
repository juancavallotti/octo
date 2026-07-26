// Package httppool builds the outbound HTTP transports every connector that
// calls an external API shares.
//
// It exists because the obvious way to write an HTTP client in Go is a trap at
// this runtime's concurrency. &http.Client{Timeout: t} leaves Transport nil,
// which means http.DefaultTransport, whose MaxIdleConnsPerHost is 2. Past two
// concurrent requests to the same host every further connection is closed
// instead of parked, so the next request dials again — one TCP handshake, and
// against HTTPS one TLS handshake, per request. Reuse gets worse as concurrency
// rises, which is the opposite of what a pool is for, and a sustained workload
// walks through the ephemeral port range and then fails as though the upstream
// were down.
//
// A flow's workers setting is a request for exactly that concurrency: 512
// workers means up to 512 concurrent outbound requests. The pool has to be sized
// in the same units.
package httppool

import (
	"net/http"
	"time"
)

// Defaults for the pool. MaxIdleConns matches MaxIdleConnsPerHost because a
// connector is bound to one base URL and so, in practice, to one host: splitting
// the two would only cap the connector below its own per-host limit. 100 is Go's
// own MaxIdleConns default, and comfortably above any workers count that is not
// deliberately extreme.
const (
	DefaultMaxIdleConns        = 100
	DefaultMaxIdleConnsPerHost = 100
	DefaultIdleConnTimeout     = 90 * time.Second
)

// Settings are the pool knobs a connector may expose. A zero field takes its
// default; DisableKeepAlives is the escape hatch for an upstream that mishandles
// persistent connections, and costs a connection per request by design.
type Settings struct {
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
	DisableKeepAlives   bool
}

// New returns a transport configured from s. It clones http.DefaultTransport
// rather than building one from nothing, so the proxy resolution, dial and TLS
// handshake timeouts, and HTTP/2 negotiation stay whatever the standard library
// considers right — only the pool sizing is ours.
func New(s Settings) *http.Transport {
	var t *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		t = base.Clone()
	} else {
		t = &http.Transport{}
	}

	t.MaxIdleConns = orDefault(s.MaxIdleConns, DefaultMaxIdleConns)
	t.MaxIdleConnsPerHost = orDefault(s.MaxIdleConnsPerHost, DefaultMaxIdleConnsPerHost)
	if s.IdleConnTimeout > 0 {
		t.IdleConnTimeout = s.IdleConnTimeout
	} else {
		t.IdleConnTimeout = DefaultIdleConnTimeout
	}
	t.DisableKeepAlives = s.DisableKeepAlives
	return t
}

// NewClient returns a client with a pooled transport and the given timeout.
// Connectors that expose no pool settings of their own use this so they still
// get a usable pool rather than an idle pool of two.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: New(Settings{})}
}

// orDefault returns v when it is positive, else def.
func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}
