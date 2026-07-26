// Package observability is the runtime service that lets a deployment see the
// process from the outside: liveness and readiness probes, and (behind a flag)
// Prometheus metrics.
//
// It is a hosted runtime service — it supplies no core.RuntimeServices, runs for
// the life of the process, and configures itself from `octo run` flags rather than
// from a config file. It sits beside the standalone and k8s providers because it
// is the same service under either: sharing it is the only reason it is not
// duplicated into both.
//
// It serves on an admin port of its own rather than on the http connector's, so a
// probe never collides with a user route, never inherits a flow's CORS or
// timeouts, and answers whether or not the integration has an HTTP source at all.
package observability

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// serviceName identifies the service in logs and startup errors.
	serviceName = "observability"

	// defaultAddr is the admin listen address. The port is deliberately far from
	// anything a flow would bind, so enabling probes by default cannot take a port
	// a user's http connector wanted.
	defaultAddr = ":39999"

	// readHeaderTimeout matches the http connector's default. A probe endpoint is
	// as reachable as any other, so it gets the same slow-loris guard.
	readHeaderTimeout = 10 * time.Second

	// shutdownTimeout bounds the drain on stop. Probes and scrapes are short; a
	// server that has not drained in this long is not going to.
	shutdownTimeout = 5 * time.Second
)

// Environment variables, each the default for the flag of the same name. They are
// read from the process environment only: a runtime's .env file is loaded during
// config load, which happens after the flags are parsed.
const (
	envEnabled      = "OCTO_OBSERVABILITY"
	envAddr         = "OCTO_OBSERVABILITY_ADDR"
	envMetrics      = "OCTO_METRICS"
	envMetricBlocks = "OCTO_METRICS_BLOCKS"
)

func init() {
	services.RegisterHosted(New())
}

// Service is the observability runtime service.
type Service struct {
	enabled      bool
	addr         string
	metrics      bool
	metricBlocks string

	health      *services.Health
	collectors  *metrics
	unsubscribe func()
	server      *http.Server
	ln          net.Listener
}

// New returns the service with nothing resolved yet; Flags fills it in.
func New() *Service { return &Service{} }

// Name implements services.HostedService.
func (s *Service) Name() string { return serviceName }

// Flags registers the service's `octo run` flags. Each default is the
// environment-resolved value, so passing the flag wins over the environment
// without any precedence code, and --help prints what is actually in effect.
func (s *Service) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&s.enabled, "observability", envBool(envEnabled, true),
		"serve liveness and readiness probes on the admin port")
	fs.StringVar(&s.addr, "observability-addr", envString(envAddr, defaultAddr),
		"admin listen address for probes and metrics")
	fs.BoolVar(&s.metrics, "metrics", envBool(envMetrics, false),
		"serve Prometheus metrics on the admin port")
	fs.StringVar(&s.metricBlocks, "metrics-blocks", envString(envMetricBlocks, ""),
		"comma-separated block addresses to report per-block timings for, or * for all")
}

// Usage implements services.HostedService.
func (s *Service) Usage() string {
	return `Observability flags (octo run):
  --observability=false
                     do not serve probes at all (default: serve them)
  --observability-addr <addr>
                     admin listen address (default :39999)

  --metrics          serve Prometheus metrics (default: off)
  --metrics-blocks <addrs>
                     comma-separated block addresses to report per-block timings
                     for, in the same grammar "invoke --spies" accepts, or * for
                     every block

  The admin port is separate from any http connector, so probes never collide with
  a user route and answer even for a runtime with no HTTP source at all:

    GET /healthz     200 while the process is alive
    GET /readyz      200 once every connector and flow started; 503 with the
                     reason otherwise (starting, reloading, draining, stopped)
    GET /metrics     Prometheus exposition, with --metrics

  Readiness turns false the moment a shutdown signal arrives, before flows drain,
  so a load balancer stops sending work while there are still workers to finish
  what is in flight.

  --metrics exports per-flow rate, errors and duration, the in-flight count per
  flow, and the Go runtime and process collectors (goroutines, heap, GC, RSS, CPU,
  open files). Every label comes from the config — never from message data — so
  cardinality is bounded by what you wrote.

  --metrics-blocks adds per-block timings, for named blocks only:

    --metrics-blocks orders.charge,orders.fanout[audit].log-it

  It is opt-in because it is the expensive half: watching ANY block makes the
  engine emit an event around EVERY block, in every flow, not just the watched
  ones. Delivery is at-most-once, so under saturation the block counters
  under-report — octo_block_events_dropped_total is how much.

  Each flag's default is its environment variable, so a deployment can set
  OCTO_OBSERVABILITY / OCTO_OBSERVABILITY_ADDR / OCTO_METRICS instead. These are
  read from the process environment, not from a .env file.`
}

// Start binds the admin listener and serves on it. Binding is synchronous so a
// port conflict fails the run rather than becoming a log line nobody reads.
func (s *Service) Start(ctx context.Context, health *services.Health) error {
	if !s.enabled {
		if s.metrics {
			slog.Warn("observability: --metrics has no effect while --observability is false; " +
				"nothing serves /metrics")
		}
		slog.Debug("observability disabled")
		return nil
	}
	s.health = health

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.addr, err)
	}
	s.ln = ln

	if s.metrics {
		s.collectors = newMetrics(health)
		// Subscribing costs nothing until a flow publishes: the bus is already
		// publishing whether or not anyone listens, unlike the block-event
		// dispatcher, which is why per-flow metrics need no address list.
		s.unsubscribe = core.DefaultEventBus().Subscribe(s.collectors.onFlowEvent)
		s.watchBlocks()
	}

	s.server = &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       readHeaderTimeout,
	}

	go func() {
		if serveErr := s.server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Error("observability server stopped", "error", serveErr)
		}
	}()
	slog.Info("observability listening", "addr", ln.Addr().String())
	return nil
}

// watchBlocks registers the per-block listener when --metrics-blocks named any
// address. Nothing is registered otherwise, which is what keeps the block-event
// dispatcher inactive — and the engine's per-block emission off — by default.
//
// The listener is async, so a slow update costs telemetry rather than throughput,
// at the price of at-most-once delivery. It registers on the process-wide
// dispatcher once, for the life of the process, so a --watch reload does not
// accumulate a listener per generation (the dispatcher has no unsubscribe).
func (s *Service) watchBlocks() {
	addresses := parseAddresses(s.metricBlocks)
	if len(addresses) == 0 {
		return
	}

	events := core.DefaultBlockEvents()
	blocks := newBlockMetrics(s.collectors.registry, addresses, events)
	events.AddAsync(blocks.onBlockEvent)

	if blocks.all {
		slog.Warn("observability: --metrics-blocks '*' reports every block; " +
			"on a large config that is a lot of time series")
	}
	slog.Info("observability: reporting per-block metrics",
		"blocks", len(addresses), "all", blocks.all)
}

// Configured records each generation's shape and pre-creates its flows' series,
// so a flow that has taken no traffic reads as 0 rather than being absent. It
// implements services.ConfigAware, and is called again on every --watch reload.
func (s *Service) Configured(config types.Config) {
	if s.collectors == nil {
		return
	}
	s.collectors.observeConfig(config)
}

// Stop releases the flow-event subscription and shuts the admin server down,
// draining in-flight probes.
func (s *Service) Stop(ctx context.Context) error {
	if s.unsubscribe != nil {
		s.unsubscribe()
		s.unsubscribe = nil
	}
	if s.server == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down observability server: %w", err)
	}
	return nil
}

// Addr returns the address the admin server actually bound, which is not s.addr
// when the configured port was 0. It is empty when the service is not serving.
func (s *Service) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// envString returns the environment variable's value, or fallback when it is
// unset or empty.
func envString(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}

// envBool parses a boolean environment variable, accepting what strconv does
// (1/t/T/true/TRUE, 0/f/F/false/FALSE) plus the conversational on/off/yes/no. An
// unparseable value is logged and ignored rather than failing the run: a typo in
// a deployment's environment should not stop the runtime from starting.
func envBool(name string, fallback bool) bool {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback
	}
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "on", "yes":
		return true
	case "off", "no":
		return false
	}
	v, err := strconv.ParseBool(trimmed)
	if err != nil {
		slog.Warn("observability: ignoring unparseable environment variable",
			"var", name, "value", raw, "default", fallback)
		return fallback
	}
	return v
}
