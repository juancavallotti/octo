// Package http provides a connector that turns synchronous HTTP requests into
// flow executions and returns the result to the caller. The connector owns a
// single net/http server (host, port, base path, server timeouts); its sources
// register routes on that server. Each request builds a message, waits for the
// flow to finish, and writes the final message body back as JSON.
//
// Request/response correlation rides the process-wide flow-event bus: the
// connector subscribes once, and every terminal FlowEvent carries the message
// (types.FlowEvent.Result) keyed by EventID, which the parked request handler
// matches against its pending registry.
package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterConnector("http", func() core.Connector {
		return &Connector{}
	})
}

const (
	defaultPort              = 8080
	defaultRequestTimeout    = 30 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
)

// result is the outcome the event-bus handler delivers to a parked request.
type result struct {
	kind types.FlowEventKind
	msg  *types.Message
	err  error
}

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	Host string `json:"host"`
	// Port is a pointer so an explicit 0 (let the OS pick a free port) is
	// distinguishable from an unset value (which defaults to 8080).
	Port           *int     `json:"port"`
	BasePath       string   `json:"basePath"`
	KeepAlive      *bool    `json:"keepAlive"`
	RequestTimeout duration `json:"requestTimeout"`
	ReadTimeout    duration `json:"readTimeout"`
	WriteTimeout   duration `json:"writeTimeout"`
	IdleTimeout    duration `json:"idleTimeout"`
}

// Connector owns the shared HTTP server and the request/response registry. The
// sources it builds register routes on its mux and rendezvous with completed
// flows through its pending map.
type Connector struct {
	mux        *http.ServeMux
	server     *http.Server
	ln         net.Listener
	basePath   string
	reqTimeout time.Duration

	serveOnce   sync.Once
	stopOnce    sync.Once
	done        chan struct{}
	unsubscribe func()

	mu      sync.Mutex
	pending map[string]chan result
	routes  map[string]struct{}
}

// Start decodes the global settings, binds the listener eagerly (so a port
// conflict fails fast), builds the server, and subscribes once to the flow-event
// bus. It does not begin serving: routes are registered by NewSource, which the
// runtime calls after Start, so accepting is deferred until the first source
// starts (see ensureServing).
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}

	c.basePath = normalizeBasePath(set.BasePath)
	c.reqTimeout = time.Duration(set.RequestTimeout)
	if c.reqTimeout <= 0 {
		c.reqTimeout = defaultRequestTimeout
	}
	port := defaultPort
	if set.Port != nil {
		port = *set.Port
	}

	addr := net.JoinHostPort(set.Host, strconv.Itoa(port))
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("http connector listen on %s: %w", addr, err)
	}

	readTimeout := time.Duration(set.ReadTimeout)
	readHeaderTimeout := readTimeout
	if readHeaderTimeout <= 0 {
		readHeaderTimeout = defaultReadHeaderTimeout
	}
	c.mux = http.NewServeMux()
	c.server = &http.Server{
		Handler:           c.mux,
		ReadTimeout:       readTimeout,
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      time.Duration(set.WriteTimeout),
		IdleTimeout:       time.Duration(set.IdleTimeout),
	}
	if set.KeepAlive != nil {
		c.server.SetKeepAlivesEnabled(*set.KeepAlive)
	}
	c.ln = ln
	c.done = make(chan struct{})
	c.pending = make(map[string]chan result)
	c.routes = make(map[string]struct{})

	c.unsubscribe = core.DefaultEventBus().Subscribe(c.onFlowEvent)
	return nil
}

// Stop unblocks any parked request handlers (they return 503) and then shuts the
// server down, draining in-flight requests within ctx's deadline.
func (c *Connector) Stop(ctx context.Context) error {
	c.stopOnce.Do(func() { close(c.done) })
	if c.unsubscribe != nil {
		c.unsubscribe()
	}
	if c.server == nil {
		return nil
	}
	// Close the listener explicitly. Serving is deferred until the first source
	// calls ensureServing, so on a failed config reload (connectors started but
	// no request yet) server.Shutdown never sees the listener and the port would
	// leak. If serving did start, Shutdown already closed it and this is a no-op
	// "use of closed network connection" we ignore.
	if c.ln != nil {
		_ = c.ln.Close()
	}
	if err := c.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("http connector shutdown: %w", err)
	}
	return nil
}

// ensureServing starts the accept loop exactly once, after every route has been
// registered. Sources call it from their Start.
func (c *Connector) ensureServing() {
	c.serveOnce.Do(func() { go c.serve() })
}

// serve runs the accept loop until the server is shut down.
func (c *Connector) serve() {
	if err := c.server.Serve(c.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("http connector serve failed", "error", err)
	}
}

// registerRoute installs handler at pattern, failing on a duplicate rather than
// letting net/http.ServeMux panic.
func (c *Connector) registerRoute(pattern string, handler http.HandlerFunc) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.routes[pattern]; exists {
		return fmt.Errorf("http route %q already registered", pattern)
	}
	c.routes[pattern] = struct{}{}
	c.mux.HandleFunc(pattern, handler)
	return nil
}

// track registers a buffered reply channel under eventID and returns it. The
// buffer of one lets onFlowEvent deliver without ever blocking the flow worker.
func (c *Connector) track(eventID string) chan result {
	ch := make(chan result, 1)
	c.mu.Lock()
	c.pending[eventID] = ch
	c.mu.Unlock()
	return ch
}

// forget removes the pending entry for eventID; safe to call more than once.
func (c *Connector) forget(eventID string) {
	c.mu.Lock()
	delete(c.pending, eventID)
	c.mu.Unlock()
}

// onFlowEvent delivers a terminal flow event to the matching parked request.
// It runs synchronously on the flow worker, so it never blocks: the reply
// channel is buffered and the send is non-blocking. Started events carry no
// result and are ignored.
func (c *Connector) onFlowEvent(ev types.FlowEvent) {
	if ev.Kind == types.FlowEventStarted {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[ev.EventID]
	c.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- result{kind: ev.Kind, msg: ev.Result, err: ev.Err}:
	default:
	}
}

// endpointURL builds a best-effort browsable URL for a registered route pattern,
// using the bound listener address. It is for logging only.
func (c *Connector) endpointURL(pattern string) string {
	if c.ln == nil {
		return pattern
	}
	return "http://" + c.ln.Addr().String() + pattern
}

// normalizeBasePath trims a trailing slash and ensures a leading slash, so it
// joins cleanly with a source path. An empty base path stays empty.
func normalizeBasePath(base string) string {
	base = strings.TrimSpace(base)
	if base == "" || base == "/" {
		return ""
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return strings.TrimRight(base, "/")
}

// duration decodes either a Go duration string ("5s") or a numeric nanosecond
// count from settings, since settings round-trip through JSON.
type duration time.Duration

// UnmarshalJSON parses a duration from a quoted string ("250ms") or a number.
func (d *duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		parsed, err := time.ParseDuration(strings.Trim(s, `"`))
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*d = duration(parsed)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = duration(n)
	return nil
}
