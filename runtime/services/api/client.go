package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// Request-shaping constants.
const (
	// errorBodySnippet bounds how much of an error body is quoted back, so a server
	// returning an HTML error page does not fill the log with it.
	errorBodySnippet = 512
	// maxIdleConnsPerHost overrides net/http's default of 2. Every call this module
	// makes goes to one host, and a runtime with a few subscriptions long-polling
	// plus ordinary KV traffic exceeds 2 immediately — the excess would open a
	// fresh connection per request and quietly cost a handshake each time.
	maxIdleConnsPerHost = 32
	idleConnTimeout     = 90 * time.Second
)

// Retry policy. Deliberately short: this client sits on the hot path of flow
// execution, and a long retry chain turns a degraded platform into a hung
// runtime. The poll loops do their own, longer backoff.
const (
	retryAttempts = 3
	retryBase     = 200 * time.Millisecond
	retryCap      = 2 * time.Second
	retryJitter   = 0.2
)

// client is the module's shared HTTP client: one transport, one auth story, one
// error mapping, used by every sub-client.
type client struct {
	base    string
	http    *http.Client
	headers http.Header

	// timeout bounds an ordinary call; longTimeout bounds the ones that wait by
	// design — the queue and topic long polls, and memory search.
	timeout     time.Duration
	longTimeout time.Duration

	// tokenFile is re-read when its modification time changes, so a rotated
	// credential is picked up without a restart. Cloud Run secret volumes and k8s
	// projected tokens both rotate in place under a running process.
	tokenFile string
	mu        sync.RWMutex
	token     string
	tokenMod  time.Time
}

// newClient builds the client from the resolved configuration.
func newClient(cfg Config) (*client, error) {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("api: unexpected default transport %T", http.DefaultTransport)
	}
	tr := transport.Clone()
	tr.MaxIdleConnsPerHost = maxIdleConnsPerHost
	tr.IdleConnTimeout = idleConnTimeout
	tr.ForceAttemptHTTP2 = true
	if err := configureTLS(tr, cfg); err != nil {
		return nil, err
	}

	headers := http.Header{}
	for name, value := range cfg.Headers {
		headers.Set(name, value)
	}
	if cfg.DeploymentID != "" {
		headers.Set("X-Octo-Deployment", cfg.DeploymentID)
	}
	headers.Set("X-Octo-Instance", cfg.InstanceID)

	return &client{
		base: cfg.BaseURL,
		// The client-level timeout is only a backstop: do sets a per-call deadline,
		// and the long polls need more room than any single value would give.
		http: &http.Client{
			Transport:     tr,
			Timeout:       cfg.LongTimeout + cfg.Timeout,
			CheckRedirect: refuseInsecureRedirect,
		},
		headers:     headers,
		timeout:     cfg.Timeout,
		longTimeout: cfg.LongTimeout,
		tokenFile:   cfg.TokenFile,
		token:       cfg.Token,
	}, nil
}

// refuseInsecureRedirect stops a redirect from downgrading the connection.
//
// net/http drops the Authorization header when a redirect crosses to a different
// host, but not when it stays on the same one — so an https endpoint answering
// 302 to its own http:// URL would put the bearer token on the wire in the clear,
// and nothing in the runtime would notice. Loopback is exempt for the same reason
// plaintext loopback is allowed at all.
//
// The redirect count is left at net/http's default; the scheme is the part worth
// having an opinion about.
func refuseInsecureRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	from, to := via[len(via)-1].URL, req.URL
	if from.Scheme == schemeHTTPS && to.Scheme != schemeHTTPS && !isLoopback(to.Hostname()) {
		return fmt.Errorf("api: refusing a redirect from %s to %s: it would downgrade the "+
			"connection and send this runtime's credentials in the clear", from.Scheme, to.Scheme)
	}
	return nil
}

// configureTLS applies a custom CA and client certificate when configured. Both
// are optional; a sidecar on loopback needs neither.
func configureTLS(tr *http.Transport, cfg Config) error {
	if cfg.CAFile == "" && cfg.ClientCertFile == "" {
		return nil
	}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return fmt.Errorf("api: read %s: %w", envCAFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return fmt.Errorf("api: %s at %q holds no PEM certificates", envCAFile, cfg.CAFile)
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	if cfg.ClientCertFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.ClientCertFile, cfg.ClientKeyFile)
		if err != nil {
			return fmt.Errorf("api: load client certificate: %w", err)
		}
		tr.TLSClientConfig.Certificates = []tls.Certificate{cert}
	}
	return nil
}

func (c *client) close() { c.http.CloseIdleConnections() }

// url builds a route's URL, substituting {placeholders} positionally with escaped
// arguments and appending any query parameters.
func (c *client) url(r route, args ...string) string {
	path := r.path
	for _, arg := range args {
		open := strings.Index(path, "{")
		if open < 0 {
			break
		}
		end := strings.Index(path[open:], "}")
		if end < 0 {
			break
		}
		path = path[:open] + url.PathEscape(arg) + path[open+end+1:]
	}
	return c.base + path
}

// query appends query parameters to a URL built by url.
func query(endpoint string, pairs ...string) string {
	if len(pairs) < 2 {
		return endpoint
	}
	values := url.Values{}
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			values.Set(pairs[i], pairs[i+1])
		}
	}
	if len(values) == 0 {
		return endpoint
	}
	return endpoint + "?" + values.Encode()
}

// do issues one request with the module's headers and a per-call deadline,
// retrying when the route allows it.
//
// The cancel is not deferred: the caller reads the response body, and cancelling
// before that closes it underneath them. It rides on the body instead, so the
// deadline lives exactly as long as the response does.
func (c *client) do(
	ctx context.Context, r route, endpoint string, body []byte,
	headers map[string]string, timeout time.Duration,
) (*http.Response, error) {
	var lastErr error
	for attempt := range retryAttempts {
		if attempt > 0 {
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
		}
		resp, err := c.attempt(ctx, r, endpoint, body, headers, timeout)
		if err != nil {
			lastErr = err
			if r.idempotent && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		if r.idempotent && retryableStatus(resp.StatusCode) && attempt < retryAttempts-1 {
			lastErr = statusError(routeOp(r), resp)
			drainClose(resp.Body)
			continue
		}
		return resp, nil
	}
	return nil, lastErr
}

// attempt issues exactly one request.
func (c *client) attempt(
	ctx context.Context, r route, endpoint string, body []byte,
	headers map[string]string, timeout time.Duration,
) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, endpoint, reader)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("api: new request: %w", err)
	}
	for name, values := range c.headers {
		req.Header[name] = values
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("X-Octo-Request-Id", requestID())
	if token := c.bearer(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req) //nolint:bodyclose // the caller closes it; cancel rides along
	if err != nil {
		cancel()
		return nil, fmt.Errorf("api %s: %w", routeOp(r), err)
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// jsonBody encodes a value for a request that goes through do rather than json —
// the ones that also need response headers.
func jsonBody(in any) ([]byte, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("api: encode request: %w", err)
	}
	return body, nil
}

// json issues a request with an optional JSON body and decodes an optional JSON
// response, mapping the module's shared status semantics.
func (c *client) json(
	ctx context.Context, r route, endpoint string, in, out any, timeout time.Duration,
) error {
	var body []byte
	headers := map[string]string{}
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("api %s: encode request: %w", routeOp(r), err)
		}
		body = encoded
		headers[contentTypeHeader] = contentTypeJSON
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := c.do(ctx, r, endpoint, body, headers, timeout)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)

	if err := mapStatus(r, resp); err != nil {
		return err
	}
	// A 204 leaves out at its zero value rather than failing to decode. On the
	// long polls that is the whole point: 204 is how the server says the poll
	// expired with nothing to deliver, which is the normal case on an idle
	// subject and not an error.
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("api %s: decode response: %w", routeOp(r), err)
	}
	return nil
}

// errNotImplemented reports a 501 to the caller, which latches the feature off.
var errNotImplemented = errors.New("platform API route is not implemented")

// errAbsent reports a 404: the addressed thing is not there. Whether that is a
// failure depends on what was addressed — a missing KV key is a miss, a missing
// lease is a lost claim — so json returns it and each caller decides.
var errAbsent = errors.New("the platform API says this does not exist")

// isVersionConflict reports a 409, which several capabilities read as "somebody
// else got there" rather than as a failure.
func isVersionConflict(err error) bool { return errors.Is(err, core.ErrVersionConflict) }

// mapStatus turns a response status into the module's shared error vocabulary.
func mapStatus(r route, resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errAbsent
	case resp.StatusCode == http.StatusNotImplemented:
		return errNotImplemented
	case resp.StatusCode == http.StatusConflict:
		return core.ErrVersionConflict
	case resp.StatusCode >= http.StatusBadRequest:
		return statusError(routeOp(r), resp)
	}
	return nil
}

// bearer returns the current token, re-reading the token file when it has changed.
// A read failure keeps the last good token: a credential that momentarily cannot
// be read is better answered with the previous one than with none.
func (c *client) bearer() string {
	if c.tokenFile == "" {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.token
	}
	info, err := os.Stat(c.tokenFile)
	if err != nil {
		c.mu.RLock()
		defer c.mu.RUnlock()
		return c.token
	}
	c.mu.RLock()
	fresh := info.ModTime().Equal(c.tokenMod)
	token := c.token
	c.mu.RUnlock()
	if fresh {
		return token
	}
	raw, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return token
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = strings.TrimSpace(string(raw))
	c.tokenMod = info.ModTime()
	return c.token
}

// retryableStatus reports whether a status is worth another attempt: the ones
// that mean "not now" rather than "not ever".
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// backoff returns the delay before the given attempt: exponential from retryBase,
// capped, with jitter so a fleet of runtimes does not retry in lockstep.
func backoff(attempt int) time.Duration {
	d := retryBase << (attempt - 1)
	if d > retryCap {
		d = retryCap
	}
	return jitter(d)
}

// jitter spreads a delay by ±retryJitter.
func jitter(d time.Duration) time.Duration {
	spread := float64(d) * retryJitter
	return d + time.Duration(spread*(randUnit()*2-1))
}

// randUnit returns a value in [0,1). It uses crypto/rand because the runtime's
// linter set rejects math/rand, and this is called rarely enough that the cost
// does not matter.
func randUnit() float64 {
	// midpoint is the jitter-free answer used when the entropy source fails: no
	// jitter is a worse spread than some, but it is never wrong.
	const midpoint = 0.5
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return midpoint
	}
	const scale = 1 << 32
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	return float64(v) / scale
}

// sleep waits, or returns early when the context ends. A non-positive duration
// still checks the context, so a caller pacing itself against an already-elapsed
// window does not turn into a loop that never yields.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("api: %w", ctx.Err())
		default:
			return nil
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("api: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}

// requestID returns a fresh correlation id, so an implementer can tie their logs
// to one runtime call.
func requestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "octo"
	}
	return hex.EncodeToString(b[:])
}

// routeOp names a route for error messages: the feature and the path, which is
// what an implementer needs to find the handler.
func routeOp(r route) string { return string(r.feature) + " " + r.method + " " + r.path }

// statusError builds an error from an unexpected response, quoting a bounded
// snippet of the body.
func statusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodySnippet))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("api %s: unexpected status %s", op, resp.Status)
	}
	return fmt.Errorf("api %s: unexpected status %s: %s", op, resp.Status, msg)
}

// drainClose drains and closes a response body so the connection can be reused.
func drainClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

// cancelOnClose ties a request's deadline to the lifetime of its response body.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	if err != nil {
		return fmt.Errorf("api: close response: %w", err)
	}
	return nil
}
