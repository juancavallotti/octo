package core

import (
	"context"

	"github.com/juancavallotti/octo/types"
)

// MessageSource is a flow's entry point, created and owned by a connector. It
// responds to connector events by building a *types.Message and sending it on
// the output channel handed to it at construction.
//
// The runtime owns the channel's lifecycle: Start must not send after Stop
// returns, and the runtime closes the channel only after Stop completes. Start
// must not block; it acquires resources and returns, doing its work on its own
// goroutine(s).
type MessageSource interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// SourceDeps carries build-time services a source may need beyond its own config.
// Most sources ignore it. Resources loads the resources a source's expressions may
// reach — a cron payload calling templateResource(id) renders through it. It is
// never nil: the runtime substitutes a no-op loader when none is wired.
type SourceDeps struct {
	Resources ResourceLoader
}

// SourceProvider is an optional capability a connector implements to supply
// sources for flows bound to it. The returned source closes over the connector's
// own resources (connections, transaction managers), which is why no separate
// globals registry is needed. cfg.Type selects which source the connector builds.
type SourceProvider interface {
	NewSource(cfg types.SourceConfig, out chan<- *types.Message, deps SourceDeps) (MessageSource, error)
}
