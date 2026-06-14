package core

import (
	"context"
	"fmt"

	"github.com/juancavallotti/eip-go/types"
)

type Service struct {
	config   types.Config
	registry *Registry
}

func NewService(config types.Config, registry *Registry) *Service {
	if registry == nil {
		registry = DefaultRegistry()
	}

	return &Service{config: config, registry: registry}
}

func (s *Service) Run(ctx context.Context) error {
	running := make([]Connector, 0, len(s.config.Connectors))

	for _, connectorConfig := range s.config.Connectors {
		connector, err := s.registry.New(connectorConfig.Type)
		if err != nil {
			return err
		}

		if err := connector.Start(ctx, connectorConfig); err != nil {
			return fmt.Errorf("start connector %q: %w", connectorConfig.Name, err)
		}

		running = append(running, connector)
	}

	<-ctx.Done()

	for i := len(running) - 1; i >= 0; i-- {
		if err := running[i].Stop(context.Background()); err != nil {
			return err
		}
	}

	return nil
}
