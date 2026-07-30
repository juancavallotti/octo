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
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerJWTValidate()
}

func registerConnector() {
	core.MustRegisterConnector("http", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "http",
		Label:    "HTTP Server",
		Icon:     "Webhook",
		Settings: reflect.TypeFor[connectorSettings](),
		Sources: []core.SourceMeta{{
			Type:     "http",
			Label:    "HTTP route",
			Icon:     "Webhook",
			Settings: reflect.TypeFor[sourceSettings](),
		}},
	})
}

const (
	defaultPort = 8080
	// defaultHost binds every interface when neither the settings nor the
	// HTTP_HOST env var pin an address (matches the orchestrator's bindAllHost).
	defaultHost              = "0.0.0.0"
	defaultRequestTimeout    = 30 * time.Second
	defaultReadHeaderTimeout = 10 * time.Second
	// envHTTPHost and envHTTPPort are the unprefixed env vars the runtime binds
	// to when the connector is used config-less (the implicit/type-resolved path
	// starts it with empty settings). They mirror the contract the orchestrator
	// injects into pods (orchestrator/internal/deployment/envports.go).
	envHTTPHost = "HTTP_HOST"
	envHTTPPort = "HTTP_PORT"
)

// result is the outcome the event-bus handler delivers to a parked request.
type result struct {
	kind types.FlowEventKind
	msg  *types.Message
	err  error
}

// connectorSettings is the global config decoded from the connector's settings.
type connectorSettings struct {
	// Bind address; when unset, uses $HTTP_HOST, falling back to 0.0.0.0 (all interfaces).
	Host string `json:"host" octo:"label=Host"`
	// Bind port; when unset, uses $HTTP_PORT, falling back to 8080. 0 = OS-assigned.
	Port *int `json:"port" octo:"label=Port,default=8080"`
	// Prefix for all routes.
	BasePath string `json:"basePath" octo:"label=Base path"`
	// Enable HTTP keep-alives.
	KeepAlive *bool `json:"keepAlive" octo:"label=Keep-alive"`
	// How long a handler waits for the flow to finish.
	RequestTimeout duration `json:"requestTimeout" octo:"label=Request timeout,type=string,default=30s"`
	// Server read timeout.
	ReadTimeout duration `json:"readTimeout" octo:"label=Read timeout,type=string"`
	// Server write timeout.
	WriteTimeout duration `json:"writeTimeout" octo:"label=Write timeout,type=string"`
	// Server idle timeout.
	IdleTimeout duration `json:"idleTimeout" octo:"label=Idle timeout,type=string"`
	// Cross-origin resource sharing. Inert unless allowedOrigins is set.
	CORS corsSettings `json:"cors" octo:"label=CORS,type=object"`
}

// corsSettings is the global CORS policy applied to every route. It is off
// unless AllowedOrigins is non-empty; when set, the connector wraps its mux in
// a middleware that answers OPTIONS preflight and adds the headers below.
type corsSettings struct {
	// Origins allowed to make cross-origin requests. Exact matches; a single "*"
	// allows any origin.
	AllowedOrigins []string `json:"allowedOrigins" octo:"label=Allowed origins"`
	// Methods allowed on preflight; defaults to the common set when unset.
	AllowedMethods []string `json:"allowedMethods" octo:"label=Allowed methods"`
	// Request headers allowed on preflight; when unset, the requested headers are
	// echoed back.
	AllowedHeaders []string `json:"allowedHeaders" octo:"label=Allowed headers"`
	// Response headers exposed to the browser on actual responses.
	ExposedHeaders []string `json:"exposedHeaders" octo:"label=Exposed headers"`
	// Allow credentialed (cookie/authorization) requests. When true, the origin is
	// echoed rather than "*", per the CORS spec.
	AllowCredentials bool `json:"allowCredentials" octo:"label=Allow credentials"`
	// How long a browser may cache the preflight response.
	MaxAge duration `json:"maxAge" octo:"label=Max age,type=string"`
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
	cors       *corsConfig

	serveOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
	// serving is closed when the accept loop has returned — and so, since
	// Server.Serve closes the listener it was handed on the way out, when the
	// listener is really released. Stop waits on it: winning serveOnce only proves
	// ensureServing ran, not that the goroutine reached Serve.
	serving     chan struct{}
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
	c.cors = newCORSConfig(set.CORS)
	host := resolveHost(set.Host)
	port := resolvePort(set.Port)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
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
		Handler:           c.withCORS(c.mux),
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
	c.serving = make(chan struct{})
	c.pending = make(map[string]chan result)
	c.routes = make(map[string]struct{})

	c.unsubscribe = core.DefaultEventBus().Subscribe(c.onFlowEvent)
	return nil
}

// resolveHost picks the listen host. An explicit setting wins; otherwise the
// HTTP_HOST env var is used, falling back to defaultHost (all interfaces) so the
// config-less connector still binds somewhere predictable.
func resolveHost(setHost string) string {
	if setHost != "" {
		return setHost
	}
	if env, ok := os.LookupEnv(envHTTPHost); ok && env != "" {
		return env
	}
	return defaultHost
}

// resolvePort picks the listen port. An explicit setting wins (including an
// explicit 0, which lets the OS pick a free port — hence the pointer); otherwise
// the HTTP_PORT env var is used, falling back to defaultPort. An unparseable
// HTTP_PORT is logged and ignored rather than failing the connector.
func resolvePort(setPort *int) int {
	if setPort != nil {
		return *setPort
	}
	if env, ok := os.LookupEnv(envHTTPPort); ok && env != "" {
		if p, err := strconv.Atoi(env); err == nil {
			return p
		}
		slog.Warn("http connector: ignoring unparseable HTTP_PORT", "value", env, "default", defaultPort)
	}
	return defaultPort
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
	// Exactly one of ensureServing and this claims serveOnce, which decides who
	// owns the listener. Claiming it here means no source ever started serving and
	// none now can, so Shutdown will never see the listener and we must release it
	// ourselves — otherwise a failed config reload (connectors started, no request
	// yet) would leak the port. Losing the claim means Serve took the listener, and
	// Shutdown closes it; closing it here too would race the accept loop's untrack
	// and, whenever Shutdown won that race, surface the second close as
	// "use of closed network connection" — a clean shutdown reported as a failure.
	// Whichever path runs closes c.serving, so the wait below is the same wait
	// either way and a second Stop finds it already closed.
	c.serveOnce.Do(func() {
		if c.ln != nil {
			_ = c.ln.Close()
		}
		close(c.serving)
	})
	err := c.server.Shutdown(ctx)
	if c.serving != nil {
		// Shutdown closes the listeners it is tracking, but the accept loop may not
		// have handed it this one yet: claiming serveOnce is not entering Serve. A
		// Serve that starts after Shutdown returns ErrServerClosed immediately —
		// still after releasing the listener in its own defer. Waiting for that
		// return is what makes "Stop returned" mean "the port is free", so the next
		// generation can re-bind it.
		select {
		case <-c.serving:
		case <-ctx.Done():
		}
	}
	if err != nil {
		return fmt.Errorf("http connector shutdown: %w", err)
	}
	return nil
}

// ensureServing starts the accept loop exactly once, after every route has been
// registered. Sources call it from their Start. Stop races for the same once, so
// a connector stopped before any source started serving never starts one — see
// Stop, which claims the once to take the listener over.
func (c *Connector) ensureServing() {
	c.serveOnce.Do(func() { go c.serve() })
}

// serve runs the accept loop until the server is shut down, then signals Stop
// that the listener has been released.
func (c *Connector) serve() {
	defer close(c.serving)
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
