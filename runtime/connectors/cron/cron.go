// Package cron provides a connector whose sources emit a message on a cron
// schedule. Each tick builds a message whose body comes from a CEL "payload"
// expression, evaluated with the fire time available as `now`.
package cron

import (
	"context"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("cron", func() core.Connector {
		return &Connector{}
	})

	// The cron connector carries no settings — it is source-only. Its Cron schedule
	// source fires on a schedule and builds each message from a CEL payload.
	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:  "cron",
		Label: "Cron",
		Icon:  "Clock",
		Sources: []core.SourceMeta{{
			Type:     "cron",
			Label:    "Cron schedule",
			Icon:     "Clock",
			Settings: reflect.TypeFor[settings](),
		}},
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
