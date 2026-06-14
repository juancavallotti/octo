package core

import (
	"context"
	"fmt"

	"github.com/juancavallotti/eip-go/types"
)

// Service runs the configured connectors and flows until its context is
// cancelled.
type Service struct {
	config   types.Config
	registry *Registry
	blocks   *BlockRegistry
	bus      *EventBus
}

// NewService builds a Service, falling back to the default registries and event
// bus when registry is nil.
func NewService(config types.Config, registry *Registry) *Service {
	if registry == nil {
		registry = DefaultRegistry()
	}

	return &Service{
		config:   config,
		registry: registry,
		blocks:   DefaultBlockRegistry(),
		bus:      DefaultEventBus(),
	}
}

// Run starts all configured connectors and flows, then stops them when ctx is
// done. Connectors start first (flows bind to them); on shutdown flows drain
// before connectors release their resources.
func (s *Service) Run(ctx context.Context) error {
	connectors, byName, err := s.startConnectors(ctx)
	if err != nil {
		return err
	}

	flows, err := s.buildFlows(byName)
	if err != nil {
		_ = stopConnectors(connectors)
		return err
	}

	started, err := s.startFlows(ctx, flows)
	if err != nil {
		_ = stopFlows(ctx, started)
		_ = stopConnectors(connectors)
		return err
	}

	<-ctx.Done()

	flowErr := stopFlows(ctx, started)
	connErr := stopConnectors(connectors)
	if flowErr != nil {
		return flowErr
	}
	return connErr
}

// startConnectors creates and starts each configured connector in order,
// returning them both as an ordered slice (for reverse teardown) and keyed by
// instance name (for flow binding).
func (s *Service) startConnectors(ctx context.Context) ([]Connector, map[string]Connector, error) {
	running := make([]Connector, 0, len(s.config.Connectors))
	byName := make(map[string]Connector, len(s.config.Connectors))

	for _, connectorConfig := range s.config.Connectors {
		connector, err := s.registry.New(connectorConfig.Type)
		if err != nil {
			_ = stopConnectors(running)
			return nil, nil, err
		}
		if err := connector.Start(ctx, connectorConfig); err != nil {
			_ = stopConnectors(running)
			return nil, nil, fmt.Errorf("start connector %q: %w", connectorConfig.Name, err)
		}
		running = append(running, connector)
		byName[connectorConfig.Name] = connector
	}

	return running, byName, nil
}

// buildFlows assembles a boundFlow for each configured flow, resolving its source
// connector and building its root block chain.
func (s *Service) buildFlows(byName map[string]Connector) ([]*boundFlow, error) {
	flows := make([]*boundFlow, 0, len(s.config.Flows))
	for i := range s.config.Flows {
		flow, err := s.buildFlow(s.config.Flows[i], byName)
		if err != nil {
			return nil, fmt.Errorf("build flow %q: %w", s.config.Flows[i].Name, err)
		}
		flows = append(flows, flow)
	}
	return flows, nil
}

func (s *Service) buildFlow(cfg types.FlowConfig, byName map[string]Connector) (*boundFlow, error) {
	if cfg.Source == nil {
		return nil, fmt.Errorf("flow %q requires a source", cfg.Name)
	}

	in := make(chan *types.Message, resolveBuffer(cfg.Buffer))
	source, err := s.newSource(*cfg.Source, in, byName)
	if err != nil {
		return nil, err
	}

	defs, err := processorDefs(s.config.Processors)
	if err != nil {
		return nil, err
	}

	p := newPool(resolvePoolWorkers(cfg.Pool), defaultPoolQueue)
	deps := BlockDeps{Connector: func(name string) (Connector, bool) {
		connector, ok := byName[name]
		return connector, ok
	}}
	root, err := (&builder{reg: s.blocks, pool: p, defs: defs, deps: deps}).flow(cfg)
	if err != nil {
		return nil, err
	}

	return &boundFlow{
		name:    cfg.Name,
		source:  source,
		root:    root,
		workers: resolveWorkers(cfg.Workers),
		in:      in,
		bus:     s.bus,
		pool:    p,
	}, nil
}

// newSource resolves the source's connector and asks it to build a source that
// emits on the provided channel.
//
//nolint:ireturn // returns the MessageSource interface a connector provides
func (s *Service) newSource(
	cfg types.SourceConfig,
	in chan<- *types.Message,
	byName map[string]Connector,
) (MessageSource, error) {
	connector, ok := byName[cfg.Connector]
	if !ok {
		return nil, fmt.Errorf("source connector %q is not configured", cfg.Connector)
	}
	provider, ok := connector.(SourceProvider)
	if !ok {
		return nil, fmt.Errorf("connector %q does not provide sources", cfg.Connector)
	}

	source, err := provider.NewSource(cfg, in)
	if err != nil {
		return nil, fmt.Errorf("new source %q: %w", cfg.Type, err)
	}
	return source, nil
}

// startFlows starts each flow, returning those successfully started so the caller
// can tear them down on a later failure.
func (s *Service) startFlows(ctx context.Context, flows []*boundFlow) ([]*boundFlow, error) {
	started := make([]*boundFlow, 0, len(flows))
	for _, flow := range flows {
		if err := flow.start(ctx); err != nil {
			return started, fmt.Errorf("start flow %q: %w", flow.name, err)
		}
		started = append(started, flow)
	}
	return started, nil
}

// stopFlows stops flows in reverse order, returning the first error.
func stopFlows(ctx context.Context, flows []*boundFlow) error {
	var firstErr error
	for i := len(flows) - 1; i >= 0; i-- {
		if err := flows[i].stop(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop flow %q: %w", flows[i].name, err)
		}
	}
	return firstErr
}

// stopConnectors stops connectors in reverse order, returning the first error.
func stopConnectors(connectors []Connector) error {
	var firstErr error
	for i := len(connectors) - 1; i >= 0; i-- {
		if err := connectors[i].Stop(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
