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

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
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
