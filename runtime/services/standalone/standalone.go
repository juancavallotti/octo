// Package standalone implements the single-process runtime services module:
// leader election always grants leadership (there is nothing to elect) and the KV
// store lives in process memory. It is the default module and requires no external
// infrastructure.
package standalone

import (
	"context"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/services"
)

// Module is this provider's name, matched against RUNTIME_SERVICES_MODULE.
const Module = "standalone"

func init() {
	services.Register(Module, func(_ context.Context, opts services.Options) (core.RuntimeServices, error) {
		return New(opts.ResourceRoot), nil
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
	resources core.ResourceLoader
}

// New returns a standalone services module with an empty in-memory store,
// in-process queues and topics, and a filesystem resource loader rooted at
// resourceRoot (the config directory).
func New(resourceRoot string) *Services {
	return &Services{
		kv:        newStore(),
		q:         newQueues(),
		t:         newTopics(),
		resources: newResourceLoader(resourceRoot),
	}
}

// LeaderElection returns the no-op (always-leader) election from core: with a
// single replica there is nothing to coordinate.
//
//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

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

// Close releases resources. The standalone module holds none.
func (s *Services) Close() error { return nil }
