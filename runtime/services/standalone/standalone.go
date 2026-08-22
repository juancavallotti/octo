// Package standalone implements the single-process runtime services module:
// leader election always grants leadership (there is nothing to elect) and the KV
// store lives in process memory. It is the default module and requires no external
// infrastructure.
package standalone

import (
	"context"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
)

// Module is this provider's name, matched against RUNTIME_SERVICES_MODULE.
const Module = "standalone"

func init() {
	services.Register(Module, func(_ context.Context, opts services.Options) (core.RuntimeServices, error) {
		return New(opts.ResourceRoot, opts.Tracing), nil
	})
}

// Services is the standalone runtime-services module. One in-memory store backs
// both KV and secrets: the secret store routes to dedicated namespaces, and a single
// process has nothing to encrypt them against. Queues and topics are in-process,
// and resources are read from the filesystem rooted at the config directory.
type Services struct {
	kv        *store
	q         *queues
	t         *topics
	leases    *leases
	resources core.ResourceLoader
	traces    core.TracePublisher
}

// New returns a standalone services module with an empty in-memory store,
// in-process queues and topics, a filesystem resource loader rooted at
// resourceRoot (the config directory), and — when tracing asks for it — a trace
// publisher writing to a file.
//
// A trace file that cannot be opened is logged and the run continues untraced,
// rather than failing startup. Tracing is something a runtime carries, not
// something it is: a bad --traces-file must not stop flows that were perfectly
// startable, and in a deployment it must not take the service down. The error is
// logged at error level because the alternative — a silent no-op — is how someone
// spends an afternoon wondering where their trace file went.
func New(resourceRoot string, tracing core.TraceOptions) *Services {
	traces, err := newTracePublisher(tracing, time.Now())
	if err != nil {
		slog.Error("standalone: tracing is enabled but its file could not be opened, "+
			"continuing without traces", "file", tracing.File, "error", err)
		traces = core.NoopTracer()
	}
	return &Services{
		kv:        newStore(),
		q:         newQueues(),
		t:         newTopics(),
		leases:    newLeases(time.Now),
		resources: newResourceLoader(resourceRoot),
		traces:    traces,
	}
}

// LeaderElection returns the no-op (always-leader) election from core: with a
// single replica there is nothing to coordinate.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

// Leases returns the in-process claims. Unlike leader election above, this is a
// real implementation rather than a grant-everything one: a single process still
// has runs competing for the same name, and a mutex settles that exactly.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Leases() core.Leases { return s.leases }

// KV returns the in-memory key/value store.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Secrets returns the secret store layered over the in-memory KV.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Secrets() core.SecretStore { return core.NewSecretStore(s.kv) }

// Queues returns the in-process message queues.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Queues() core.Queues { return s.q }

// Topics returns the in-process broadcast pub/sub.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Topics() core.Topics { return s.t }

// Resources returns the filesystem resource loader rooted at the config directory.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Resources() core.ResourceLoader { return s.resources }

// Traces returns the module's trace publisher: a buffered writer over a JSONL
// file when tracing is on, and the no-op otherwise.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) Traces() core.TracePublisher { return s.traces }

// Close releases resources: outstanding leases, so their renewal goroutines stop,
// and the trace file, which is drained and closed here — so a graceful stop
// leaves a complete file rather than one missing whatever was still queued.
func (s *Services) Close() error {
	s.leases.closeAll()
	if closer, ok := s.traces.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
