// Package events exposes the platform's broadcast pub/sub (core.Topics) to flows
// as first-class DSL constructs: an "event" message source that runs every message
// published to a subject through a flow, and the "publish-event" block that
// broadcasts the current message to a subject. Together they let flows fan work
// out to every interested subscriber — the broadcast counterpart to the queue
// connector's competing-consumer load balancing.
//
// The topic itself is a core runtime service (core.Topics, reached via
// core.RuntimeServicesFromContext), in-process in the standalone module and
// NATS-backed in the k8s module. This connector holds no transport of its own; a
// source subscribes to the core topics service and forwards each delivery into its
// flow, and the block publishes onto it.
package events

import (
	"context"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterConnector("events", func() core.Connector {
		return &Connector{}
	})
}

// Connector holds no shared resources: each source subscribes to the core topics
// service (from the context at start) and the publish-event block publishes onto
// it, so there is nothing to own here.
type Connector struct{}

// Start does nothing and always succeeds.
func (c *Connector) Start(context.Context, types.ConnectorConfig) error { return nil }

// Stop does nothing and always succeeds.
func (c *Connector) Stop(context.Context) error { return nil }
