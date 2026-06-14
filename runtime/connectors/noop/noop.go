package noop

import (
	"context"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// Connector is a no-op connector used as a baseline and for testing.
type Connector struct{}

func init() {
	core.MustRegisterConnector("noop", func() core.Connector {
		return &Connector{}
	})
}

// Start does nothing and always succeeds.
func (c *Connector) Start(context.Context, types.ConnectorConfig) error {
	return nil
}

// Stop does nothing and always succeeds.
func (c *Connector) Stop(context.Context) error {
	return nil
}
