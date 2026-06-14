// Package cron provides a connector whose sources emit a message on a cron
// schedule. Each tick builds a message whose body comes from a CEL "payload"
// expression, evaluated with the fire time available as `now`.
package cron

import (
	"context"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterConnector("cron", func() core.Connector {
		return &Connector{}
	})
}

// Connector holds no shared resources; each source it builds owns its own
// schedule and compiled payload expression.
type Connector struct{}

// Start does nothing and always succeeds.
func (c *Connector) Start(context.Context, types.ConnectorConfig) error {
	return nil
}

// Stop does nothing and always succeeds.
func (c *Connector) Stop(context.Context) error {
	return nil
}
