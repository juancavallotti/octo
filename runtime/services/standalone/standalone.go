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
	services.Register(Module, func(context.Context) (core.RuntimeServices, error) {
		return New(), nil
	})
}

// Services is the standalone runtime-services module.
type Services struct {
	kv *store
}

// New returns a standalone services module with an empty in-memory KV store.
func New() *Services {
	return &Services{kv: newStore()}
}

//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) LeaderElection() core.LeaderElection { return leaderElection{} }

//nolint:ireturn // satisfies core.RuntimeServices
func (s *Services) KV() core.KV { return s.kv }

// Close releases resources. The standalone module holds none.
func (s *Services) Close() error { return nil }

// leaderElection grants leadership unconditionally: with a single replica there is
// nothing to coordinate.
type leaderElection struct{}

//nolint:ireturn // satisfies core.LeaderElection
func (leaderElection) Acquire(context.Context, string) (core.Leadership, error) {
	return leadership{}, nil
}

// leadership is permanently the leader.
type leadership struct{}

func (leadership) IsLeader() bool { return true }
func (leadership) Close() error   { return nil }
