package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

const defaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// sourceSettings configures one HTTP route bound to a flow.
type sourceSettings struct {
	// Path is the route pattern appended to the connector base path. It may use
	// net/http wildcards, e.g. /orders/{id}; each {name} lands in vars.<name>.
	Path string `json:"path"`
	// Headers names the request headers to copy into variables (always set,
	// empty string when absent), readable in CEL via index, e.g. vars["X-Tenant"].
	Headers []string `json:"headers"`
	// CorrelationIDHeader, when set, sources the message CorrelationID from that
	// request header.
	CorrelationIDHeader string `json:"correlationIdHeader"`
	// Timeout bounds how long the handler waits for the flow to finish; it
	// defaults to the connector's request timeout.
	Timeout duration `json:"timeout"`
	// MaxBodyBytes caps the request body size; defaults to 1 MiB.
	MaxBodyBytes int64 `json:"maxBodyBytes"`
}

// source is a single request/response HTTP route. It builds a message per
// request, sends it on the flow channel, and waits for the flow's terminal
// event to write the response.
type source struct {
	conn         *Connector
	out          chan<- *types.Message
	pattern      string
	params       []string
	headers      []string
	corrIDHeader string
	timeout      time.Duration
	maxBody      int64

	srcDone  chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	stopping bool
	sendWG   sync.WaitGroup
}

// NewSource builds an HTTP source and registers its route on the connector's
// shared mux. It is called during flow build, before any source starts, so all
// routes exist before the server begins accepting.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(cfg types.SourceConfig, out chan<- *types.Message) (core.MessageSource, error) {
	var set sourceSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	if strings.TrimSpace(set.Path) == "" {
		return nil, errors.New("http source requires a \"path\" setting")
	}

	timeout := time.Duration(set.Timeout)
	if timeout <= 0 {
		timeout = c.reqTimeout
	}
	maxBody := set.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}

	pattern := c.basePath + ensureLeadingSlash(set.Path)
	s := &source{
		conn:         c,
		out:          out,
		pattern:      pattern,
		params:       parsePathParams(set.Path),
		headers:      set.Headers,
		corrIDHeader: set.CorrelationIDHeader,
		timeout:      timeout,
		maxBody:      maxBody,
		srcDone:      make(chan struct{}),
	}

	if err := c.registerRoute(pattern, s.handle); err != nil {
		return nil, err
	}
	slog.Info("http endpoint available", "pattern", pattern, "url", c.endpointURL(pattern))
	return s, nil
}

// Start triggers the connector's accept loop (once across all sources). All
// routes are already registered by the time any source starts.
func (s *source) Start(context.Context) error {
	s.conn.ensureServing()
	return nil
}

// Stop refuses new sends and waits for in-flight sends to drain, so the runtime
// can safely close the output channel afterwards. It does not stop the shared
// server (other sources may still serve); the connector owns server shutdown.
func (s *source) Stop(context.Context) error {
	s.mu.Lock()
	s.stopping = true
	s.mu.Unlock()
	s.stopOnce.Do(func() { close(s.srcDone) })
	s.sendWG.Wait()
	return nil
}

// handle turns one HTTP request into a flow execution and writes the result.
func (s *source) handle(w http.ResponseWriter, r *http.Request) {
	body, status, ok := s.readBody(w, r)
	if !ok {
		writeError(w, status, "invalid request body")
		return
	}

	if !s.acquireSend() {
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}

	msg, err := s.buildMessage(r, body)
	if err != nil {
		s.releaseSend()
		writeError(w, http.StatusInternalServerError, "could not build message")
		return
	}

	ch := s.conn.track(msg.EventID)
	defer s.conn.forget(msg.EventID)

	sent := s.send(r, msg)
	s.releaseSend()
	if !sent {
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}

	s.awaitResult(w, r, ch)
}

// send delivers msg onto the flow channel, aborting if the request, the source,
// or the connector is shutting down. It runs while a send token is held, so the
// runtime will not close the channel underneath it.
func (s *source) send(r *http.Request, msg *types.Message) bool {
	select {
	case s.out <- msg:
		return true
	case <-r.Context().Done():
		return false
	case <-s.srcDone:
		return false
	case <-s.conn.done:
		return false
	}
}

// awaitResult blocks until the flow finishes, the request times out, the client
// disconnects, or the connector shuts down, and writes the matching response.
func (s *source) awaitResult(w http.ResponseWriter, r *http.Request, ch chan result) {
	select {
	case res := <-ch:
		s.writeResult(w, res)
	case <-time.After(s.timeout):
		writeError(w, http.StatusGatewayTimeout, "flow timed out")
	case <-r.Context().Done():
		// Client disconnected; nothing useful to write.
	case <-s.conn.done:
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
	}
}

// httpStatusVar is the message variable a flow may set to choose the HTTP
// response status; when absent or out of range the default status is used.
const httpStatusVar = "httpStatus"

// statusFor returns the response status for a completed flow: the message's
// httpStatus variable when it is a valid HTTP status code, otherwise fallback.
// This lets a flow (or its error path) set vars.httpStatus to control the code.
func statusFor(msg *types.Message, fallback int) int {
	if code, ok := msg.Variables.Int(httpStatusVar); ok && code >= 100 && code <= 599 {
		return code
	}
	return fallback
}

// writeResult maps a flow outcome to an HTTP response.
func (s *source) writeResult(w http.ResponseWriter, res result) {
	switch res.kind {
	case types.FlowEventCompleted:
		raw, err := res.msg.BodyJSON()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not encode response")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusFor(res.msg, http.StatusOK))
		_, _ = w.Write(raw)
	case types.FlowEventDropped:
		w.WriteHeader(http.StatusNoContent)
	case types.FlowEventFailed:
		msg := "flow processing failed"
		if res.err != nil {
			msg = res.err.Error()
		}
		writeError(w, http.StatusInternalServerError, msg)
	default:
		writeError(w, http.StatusInternalServerError, "unexpected flow outcome")
	}
}

// buildMessage constructs the message from the request: path params, method,
// query, and configured headers land in Variables; the JSON body becomes Body.
func (s *source) buildMessage(r *http.Request, body []byte) (*types.Message, error) {
	correlationID := ""
	if s.corrIDHeader != "" {
		correlationID = r.Header.Get(s.corrIDHeader)
	}
	msg, err := types.NewMessage(correlationID)
	if err != nil {
		return nil, err
	}

	for _, name := range s.params {
		msg.Variables.Set(name, r.PathValue(name))
	}
	msg.Variables.Set("method", r.Method)

	query := make(map[string]any, len(r.URL.Query()))
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}
	msg.Variables.Set("query", query)

	for _, name := range s.headers {
		msg.Variables.Set(name, r.Header.Get(name))
	}

	if len(body) > 0 {
		if err := msg.SetBodyJSON(body); err != nil {
			return nil, err
		}
	}
	return msg, nil
}

// readBody reads and size-limits the request body. It returns the raw bytes and,
// on failure, the HTTP status to report. An empty body is valid (Body stays nil).
func (s *source) readBody(w http.ResponseWriter, r *http.Request) ([]byte, int, bool) {
	if r.Body == nil {
		return nil, 0, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBody)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, http.StatusRequestEntityTooLarge, false
		}
		return nil, http.StatusBadRequest, false
	}
	if len(raw) == 0 {
		return nil, 0, true
	}
	// Validate it is JSON now, so a malformed body fails before the flow runs.
	if !json.Valid(raw) {
		return nil, http.StatusBadRequest, false
	}
	return raw, 0, true
}

// acquireSend reserves a send token unless the source is stopping. Gating Add
// behind the stopping flag keeps WaitGroup.Add from racing Stop's Wait.
func (s *source) acquireSend() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return false
	}
	s.sendWG.Add(1)
	return true
}

// releaseSend returns a send token.
func (s *source) releaseSend() { s.sendWG.Done() }

// writeError writes a JSON error body with the given status.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// ensureLeadingSlash returns path with a guaranteed single leading slash.
func ensureLeadingSlash(path string) string {
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// parsePathParams extracts the wildcard names from a net/http route path, e.g.
// "/orders/{id}/items/{rest...}" yields ["id", "rest"].
func parsePathParams(path string) []string {
	var params []string
	for {
		open := strings.IndexByte(path, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(path[open:], '}')
		if end < 0 {
			break
		}
		name := strings.TrimSuffix(path[open+1:open+end], "...")
		if name != "" {
			params = append(params, name)
		}
		path = path[open+end+1:]
	}
	return params
}
