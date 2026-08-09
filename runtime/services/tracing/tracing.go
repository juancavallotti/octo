// Package tracing is the runtime service that records what happened to a single
// message: every flow it entered, every block it passed through, every model turn
// an agent took on its behalf, and the source that admitted it.
//
// It answers the question neither of the existing observation seams can. Metrics
// are aggregates whose labels come from the config and never from message data,
// so they can say a flow is failing but never which message failed or where.
// Spies record everything about one block, but only under `octo invoke`, so they
// say nothing about production. A trace is the missing middle: per-message, on
// live traffic, and joined end to end by an id that survives every boundary a
// message crosses.
//
// It is a hosted service, so it owns flags rather than YAML. What it does not own
// is the transport: where records go is the runtime-services module's business —
// a file for the standalone module, a NATS subject for the k8s one — reached
// through core.RuntimeServices.Traces like queues and the object store. This
// package holds the flags, the two listeners, and the capture policy, and
// nothing that knows what a file or a broker is.
//
// Tracing is a development and debugging tool. It is off by default and says so
// loudly when turned on: see the warning in Start.
package tracing

import (
	"context"
	"flag"
	"log/slog"
	"strings"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
)

const (
	// serviceName identifies the service in logs and startup errors.
	serviceName = "tracing"

	// watchAll is the --traces-blocks value that records every block rather than
	// a named set. It is the default, because the first thing anyone wants from
	// a trace is the whole path.
	watchAll = "*"

	// defaultMaxPayload caps a captured body or variable map. Big enough for the
	// JSON documents integrations actually pass around, small enough that one
	// pathological message cannot fill a disk on its own.
	defaultMaxPayload = 32 << 10

	// defaultBuffer is how many records the publisher queues before dropping.
	defaultBuffer = 4096
)

// Environment variables, each the default for the flag of the same meaning, read
// from the process environment only (see services.EnvBool): a runtime's .env file
// is loaded during config load, which happens after the flags are parsed.
//
// This is the only way to turn tracing on in a deployed pod, which passes no
// arguments — the image's CMD stands and the environment is what varies.
const (
	envEnabled    = "OCTO_TRACING"
	envFile       = "OCTO_TRACING_FILE"
	envBlocks     = "OCTO_TRACING_BLOCKS"
	envBodies     = "OCTO_TRACING_BODIES"
	envVars       = "OCTO_TRACING_VARS"
	envMaxPayload = "OCTO_TRACING_MAX_PAYLOAD"
	envBuffer     = "OCTO_TRACING_BUFFER"
)

// service is the registered instance. It is a package var so the CLI can read the
// resolved flags back out of it (see Options) to build the runtime services the
// publisher lives in — the one thing a hosted service cannot hand over through
// the HostedService interface, because the interface predates anything needing it.
var service = New()

func init() {
	services.RegisterHosted(service)
}

// Service is the tracing runtime service.
type Service struct {
	enabled    bool
	file       string
	blocks     string
	bodies     bool
	vars       bool
	maxPayload int
	buffer     int

	// watching guards listener registration, which the runtime's event streams
	// offer no way to undo.
	watching sync.Once
}

// New returns the service with nothing resolved yet; Flags fills it in.
func New() *Service { return &Service{} }

// Name implements services.HostedService.
func (s *Service) Name() string { return serviceName }

// Flags registers the service's `octo run` flags. Each default is the
// environment-resolved value, so passing the flag wins over the environment
// without any precedence code, and --help prints what is actually in effect.
func (s *Service) Flags(fs *flag.FlagSet) {
	fs.BoolVar(&s.enabled, "traces", services.EnvBool(envEnabled, false),
		"record a trace of every message (development feature; significant overhead)")
	fs.StringVar(&s.file, "traces-file", services.EnvString(envFile, ""),
		"where the standalone runtime writes traces (default: octo-trace-<time>-<pid>.jsonl)")
	fs.StringVar(&s.blocks, "traces-blocks", services.EnvString(envBlocks, watchAll),
		"comma-separated block addresses to trace, or * for every block")
	fs.BoolVar(&s.bodies, "traces-bodies", services.EnvBool(envBodies, true),
		"include message bodies in trace records")
	fs.BoolVar(&s.vars, "traces-vars", services.EnvBool(envVars, true),
		"include message variables in trace records")
	fs.IntVar(&s.maxPayload, "traces-max-payload", services.EnvInt(envMaxPayload, defaultMaxPayload),
		"drop a captured body or variable map larger than this many bytes")
	fs.IntVar(&s.buffer, "traces-buffer", services.EnvInt(envBuffer, defaultBuffer),
		"how many trace records to queue before dropping")
}

// Usage implements services.HostedService.
func (s *Service) Usage() string {
	return `Tracing flags (octo run):
  --traces           record a trace of every message (default: off)

  TRACING IS A DEVELOPMENT FEATURE. It is not recommended under load and will
  significantly reduce throughput: every traced block is recorded on the flow's
  own goroutine, and capturing payloads marshals the message once per block. Turn
  it on to understand a run, not to leave running.

  --traces-blocks <addrs>
                     comma-separated block addresses to trace, in the same
                     grammar "invoke --spies" accepts, or * for every block
                     (default: *)
  --traces-file <path>
                     where the standalone runtime writes its JSONL trace
                     (default: octo-trace-<time>-<pid>.jsonl in the working
                     directory). The k8s runtime ignores it and publishes to the
                     internal.traces subject instead.
  --traces-bodies=false / --traces-vars=false
                     record the sequence without message payloads
  --traces-max-payload <bytes>
                     drop a captured body or variable map larger than this
                     (default 32768). Oversized payloads are omitted whole and
                     flagged, never cut short: truncated JSON is not JSON.
  --traces-buffer <n>
                     how many records to queue before dropping (default 4096)

  A trace record names the flow, the block address, its type, timing, outcome and
  the ids that join it to everything else the message touched. Records that could
  not be kept are announced in the stream itself, so a trace with a hole says so.

  Payloads are recorded by default because "what did the message look like here"
  is the question tracing exists to answer. They can carry personal data and
  credentials — the http source copies configured request headers into variables
  — so the standalone trace file is created 0600, and --traces-bodies=false
  --traces-vars=false records the sequence alone.

  Where traces go is the runtime-services module's choice, not a flag: the
  standalone module writes a file, the k8s module publishes to a subject for an
  aggregator to consume. A deployed pod passes no arguments, so set OCTO_TRACING
  and its friends in the environment instead — each flag's default is the
  variable of the same meaning (OCTO_TRACING, OCTO_TRACING_FILE,
  OCTO_TRACING_BLOCKS, OCTO_TRACING_BODIES, OCTO_TRACING_VARS,
  OCTO_TRACING_MAX_PAYLOAD, OCTO_TRACING_BUFFER). These are read from the process
  environment, not from a .env file.`
}

// Options returns the resolved configuration, for the CLI to hand to
// services.New so the active module can build its publisher.
func Options() core.TraceOptions { return service.Options() }

// Options returns this service's resolved configuration.
func (s *Service) Options() core.TraceOptions {
	return core.TraceOptions{
		Enabled:    s.enabled,
		File:       s.file,
		Buffer:     s.buffer,
		Bodies:     s.bodies,
		Vars:       s.vars,
		MaxPayload: s.maxPayload,
	}
}

// Start registers the listeners that turn the runtime's event streams into trace
// records, and warns about what was just switched on.
//
// It registers nothing else and builds nothing else. The publisher does not exist
// yet — the runtime-services module builds it, later in startup — and it does not
// need to: core.Tracer() is the no-op until then, the listeners are guarded on it,
// and no flow runs until after it is installed.
func (s *Service) Start(_ context.Context, _ *services.Health) error {
	if !s.enabled {
		return nil
	}
	s.warn()
	s.watch()
	return nil
}

// warn says plainly what tracing costs. It is not a formality: with the defaults
// this records two events for every block of every message and marshals the
// payload for each, on the flow's own goroutine. That is the right trade while
// someone is watching a run and the wrong one under load, and the difference is
// not visible from the inside — a traced runtime looks healthy, just slower.
func (s *Service) warn() {
	slog.Warn("tracing is enabled: this is a development feature and is not recommended "+
		"under load — it will significantly reduce throughput. Every traced block is "+
		"recorded on the flow's own goroutine, and capturing payloads marshals the "+
		"message once per block.",
		"blocks", s.blocks, "bodies", s.bodies, "vars", s.vars)

	if s.blocks == watchAll {
		slog.Warn("tracing: --traces-blocks '*' records every block in every flow. " +
			"Name the addresses you care about to trace a live runtime.")
	}
	if s.bodies || s.vars {
		slog.Warn("tracing: message payloads are being written to the trace sink. " +
			"Bodies and variables can carry personal data and credentials — the http " +
			"source copies configured request headers into variables. Pass " +
			"--traces-bodies=false --traces-vars=false to record the sequence only.")
	}
}

// watch registers the two listeners: one on the flow-event bus for the shape of
// each invocation, one on the block dispatcher for what happened inside it.
//
// Neither has an unsubscribe, so registration happens once for the life of the
// process and the sync.Once enforces it rather than leaving it to how the CLI
// happens to call Start. A second registration would not fail or warn — it would
// quietly write every record twice, which is the kind of wrong that survives a
// long way. That is also why the listener re-checks core.Tracer().Enabled()
// rather than trusting that registration implies recording.
func (s *Service) watch() {
	s.watching.Do(func() {
		core.DefaultEventBus().Subscribe(s.onFlowEvent)

		events := core.DefaultBlockEvents()
		if s.blocks == watchAll {
			events.AddSync(s.onBlockEvent)
			return
		}
		addresses := parseAddresses(s.blocks)
		if len(addresses) == 0 {
			slog.Warn("tracing: --traces-blocks named no addresses, no blocks will be traced",
				"value", s.blocks)
			return
		}
		events.AddSyncFor(addresses, s.onBlockEvent)
		slog.Info("tracing: recording named blocks", "blocks", len(addresses))
	})
}

// Stop implements services.HostedService. Draining and closing belong to the
// module that built the publisher and are done in its Close, so there is nothing
// to do here: this service owns no transport and no goroutine.
func (s *Service) Stop(context.Context) error { return nil }

// parseAddresses splits a comma-separated block address list, ignoring blanks.
func parseAddresses(raw string) []string {
	var addresses []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			addresses = append(addresses, trimmed)
		}
	}
	return addresses
}
