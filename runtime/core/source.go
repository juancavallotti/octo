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

// SourceProvider is an optional capability a connector implements to supply
// sources for flows bound to it. The returned source closes over the connector's
// own resources (connections, transaction managers), which is why no separate
// globals registry is needed. cfg.Type selects which source the connector builds.
type SourceProvider interface {
	NewSource(cfg types.SourceConfig, out chan<- *types.Message) (MessageSource, error)
}
