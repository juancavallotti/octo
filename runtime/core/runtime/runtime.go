// Package runtime is the application layer that wires configured connectors and
// flows into a running service. It binds the public core contracts and registries
// to the internal engine: building each flow's processing tree, starting its
// source and workers, and tearing everything down on shutdown.
package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/internal/engine"
	"github.com/juancavallotti/eip-go/core/internal/pool"
	"github.com/juancavallotti/eip-go/types"
)

// Service runs the configured connectors and flows until its context is
// cancelled.
type Service struct {
	config     types.Config
	registry   *core.Registry
	blocks     *core.BlockRegistry
	bus        *core.EventBus
	flows      *flowRegistry
	invokeMode bool
	ready      chan struct{}
}

// ServiceOption customizes a Service at construction.
type ServiceOption func(*Service)

// WithInvokeMode makes every flow use an implicit source instead of its
// configured external source, and skips starting connectors that are only used as
// a flow source. Used by the CLI to call a flow directly without standing up
// sources (no ports bound, no schedules fired).
func WithInvokeMode() ServiceOption {
	return func(s *Service) { s.invokeMode = true }
}

// NewService builds a Service, falling back to the default registries and event
// bus when registry is nil.
func NewService(config types.Config, registry *core.Registry, opts ...ServiceOption) *Service {
	if registry == nil {
		registry = core.DefaultRegistry()
	}

	bus := core.DefaultEventBus()
	s := &Service{
		config:   config,
		registry: registry,
		blocks:   core.DefaultBlockRegistry(),
		bus:      bus,
		flows:    newFlowRegistry(bus),
		ready:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Flows returns the flow caller for this service, letting an embedder invoke a
// flow by name (the CLI uses this in invoke mode). Calls only resolve once Run
// has started the flows; use Started to wait for that.
//
//nolint:ireturn // exposes the FlowCaller interface intentionally
func (s *Service) Flows() core.FlowCaller {
	return s.flows
}

// Started returns a channel closed once all flows are started and callable. An
// embedder invoking a flow should wait on it before calling Flows().
func (s *Service) Started() <-chan struct{} {
	return s.ready
}

// Run starts all configured connectors and flows, then stops them when ctx is
// done. Connectors start first (flows bind to them); on shutdown flows drain
// before connectors release their resources.
func (s *Service) Run(ctx context.Context) error {
	// Release the flow registry's bus subscription when this generation stops, so
	// a hot-reloading embedder that builds a fresh Service per reload does not
	// accumulate stale handlers on the process-wide bus.
	defer s.flows.close()

	set, err := s.startConnectors(ctx)
	if err != nil {
		return err
	}

	// buildFlows may start additional default connectors on demand to back
	// sources without an explicit connector, appending them to set.running.
	flows, err := s.buildFlows(ctx, set)
	if err != nil {
		_ = stopConnectors(set.running)
		return err
	}

	started, err := s.startFlows(ctx, flows)
	if err != nil {
		_ = stopFlows(ctx, started)
		_ = stopConnectors(set.running)
		return err
	}

	close(s.ready)
	slog.Info("runtime ready", "connectors", len(set.running), "flows", len(started))

	<-ctx.Done()

	flowErr := stopFlows(ctx, started)
	connErr := stopConnectors(set.running)
	if flowErr != nil {
		return flowErr
	}
	return connErr
}

// connectorSet tracks the connectors a generation has started: those configured
// explicitly plus any default connectors started on demand to back a source whose
// connector was not explicitly configured. Its mutex serializes on-demand starts
// so flows resolving the same default type share one instance instead of each
// starting their own.
type connectorSet struct {
	mu       sync.Mutex
	registry *core.Registry
	configs  []types.ConnectorConfig // the generation's configured connectors
	running  []core.Connector        // ordered, for reverse teardown
	byName   map[string]core.Connector
}

// lookup returns a started connector by instance name, for block dependencies.
//
//nolint:ireturn // returns the Connector interface a block depends on
func (set *connectorSet) lookup(name string) (core.Connector, bool) {
	set.mu.Lock()
	defer set.mu.Unlock()
	c, ok := set.byName[name]
	return c, ok
}

// resolveConnector finds or starts the connector backing a source. An explicit,
// configured instance wins; otherwise a lone configured connector of the desired
// type is used (several is ambiguous); otherwise a default instance of that type
// is started on demand and shared under the type name. Connectors that genuinely
// need settings fail in their own Start when started with defaults.
//
//nolint:ireturn // returns the Connector interface the source binds to
func (set *connectorSet) resolveConnector(ctx context.Context, cfg types.SourceConfig) (core.Connector, error) {
	set.mu.Lock()
	defer set.mu.Unlock()

	// 1. An explicit binding to a configured instance wins.
	if cfg.Connector != "" {
		if c, ok := set.byName[cfg.Connector]; ok {
			return c, nil
		}
	}

	// The desired connector type: an unresolved Connector that names a registered
	// type (the editor's type-name fallback), else the source Type when it names
	// one. Empty means the binding was neither a known instance nor a known type.
	typeName := set.desiredType(cfg)
	if typeName == "" {
		return nil, fmt.Errorf("source connector %q is not configured", cfg.Connector)
	}

	// 2. A single configured connector of the type binds implicitly; several is
	// genuinely ambiguous and the source must name one.
	matches := set.configuredOfType(typeName)
	if len(matches) > 1 {
		return nil, fmt.Errorf(
			"source connector is ambiguous: %d connectors of type %q are configured; set the source's connector explicitly",
			len(matches), typeName)
	}
	if len(matches) == 1 {
		if c, ok := set.byName[matches[0]]; ok {
			return c, nil
		}
	}

	// 3. No configured connector of the type: start a default instance on demand,
	// sharing it under the type name so later sources of the same type reuse it.
	if c, ok := set.byName[typeName]; ok {
		return c, nil
	}
	connector, err := set.registry.New(typeName)
	if err != nil {
		return nil, err
	}
	slog.Info("starting implicit connector", "connector", typeName, "type", typeName)
	if err := connector.Start(ctx, types.ConnectorConfig{Name: typeName, Type: typeName}); err != nil {
		return nil, fmt.Errorf("start implicit connector %q: %w", typeName, err)
	}
	set.running = append(set.running, connector)
	set.byName[typeName] = connector
	return connector, nil
}

// desiredType derives the connector type a source wants: an unresolved Connector
// naming a registered type, else the source Type when it names one.
func (set *connectorSet) desiredType(cfg types.SourceConfig) string {
	if cfg.Connector != "" && set.registry.Has(cfg.Connector) {
		return cfg.Connector
	}
	if cfg.Type != "" && set.registry.Has(cfg.Type) {
		return cfg.Type
	}
	return ""
}

// configuredOfType returns the names of configured connectors of the given type.
func (set *connectorSet) configuredOfType(typeName string) []string {
	var names []string
	for _, c := range set.configs {
		if c.Type == typeName {
			names = append(names, c.Name)
		}
	}
	return names
}

// startConnectors creates and starts each configured connector in order,
// returning a connectorSet that holds them as an ordered slice (for reverse
// teardown) and keyed by instance name (for flow binding). The set also backs
// on-demand default connectors started later during flow build.
func (s *Service) startConnectors(ctx context.Context) (*connectorSet, error) {
	set := &connectorSet{
		registry: s.registry,
		configs:  s.config.Connectors,
		running:  make([]core.Connector, 0, len(s.config.Connectors)),
		byName:   make(map[string]core.Connector, len(s.config.Connectors)),
	}

	// In invoke mode, connectors used only as a flow source acquire no resources
	// (their sources are replaced by implicit sources), so skip starting them.
	var sourceConnectors map[string]struct{}
	if s.invokeMode {
		sourceConnectors = s.sourceConnectorNames()
	}

	for _, connectorConfig := range s.config.Connectors {
		if _, isSource := sourceConnectors[connectorConfig.Name]; isSource {
			slog.Info("skipping source connector (invoke mode)",
				"connector", connectorConfig.Name, "type", connectorConfig.Type)
			continue
		}
		connector, err := s.registry.New(connectorConfig.Type)
		if err != nil {
			_ = stopConnectors(set.running)
			return nil, err
		}
		slog.Info("starting connector", "connector", connectorConfig.Name, "type", connectorConfig.Type)
		if err := connector.Start(ctx, connectorConfig); err != nil {
			_ = stopConnectors(set.running)
			return nil, fmt.Errorf("start connector %q: %w", connectorConfig.Name, err)
		}
		set.running = append(set.running, connector)
		set.byName[connectorConfig.Name] = connector
	}

	return set, nil
}

// sourceConnectorNames collects the names of connectors referenced as a flow's
// source, so invoke mode can skip starting them.
func (s *Service) sourceConnectorNames() map[string]struct{} {
	names := make(map[string]struct{})
	for i := range s.config.Flows {
		if src := s.config.Flows[i].Source; src != nil && src.Connector != "" {
			names[src.Connector] = struct{}{}
		}
	}
	return names
}

// buildFlows assembles a boundFlow for each configured flow, resolving its source
// connector (starting a default one on demand when needed) and building its root
// block chain.
func (s *Service) buildFlows(ctx context.Context, set *connectorSet) ([]*boundFlow, error) {
	flows := make([]*boundFlow, 0, len(s.config.Flows))
	for i := range s.config.Flows {
		flow, err := s.buildFlow(ctx, s.config.Flows[i], set)
		if err != nil {
			return nil, fmt.Errorf("build flow %q: %w", s.config.Flows[i].Name, err)
		}
		flows = append(flows, flow)
	}
	return flows, nil
}

func (s *Service) buildFlow(ctx context.Context, cfg types.FlowConfig, set *connectorSet) (*boundFlow, error) {
	in := make(chan *types.Message, resolveBuffer(cfg.Buffer))

	// A flow with no configured source — or any flow in invoke mode — is driven
	// by an implicit source: it acquires no resources and becomes callable by
	// name through the flow registry.
	implicit := cfg.Source == nil || s.invokeMode
	source, err := s.buildSource(ctx, cfg, in, set)
	if err != nil {
		return nil, err
	}

	sourceDesc := "implicit"
	if !implicit {
		if cfg.Source.Connector != "" {
			sourceDesc = fmt.Sprintf("%s via connector %q", cfg.Source.Type, cfg.Source.Connector)
		} else {
			sourceDesc = fmt.Sprintf("%s via default connector", cfg.Source.Type)
		}
	}

	p := pool.New(cfg.Pool, 0)
	deps := core.BlockDeps{
		Connector: set.lookup,
		Flows:     s.flows,
	}
	root, err := engine.BuildRoot(cfg, s.blocks, p, s.config.Processors, deps)
	if err != nil {
		return nil, err
	}

	return &boundFlow{
		name:       cfg.Name,
		source:     source,
		root:       root,
		workers:    resolveWorkers(cfg.Workers),
		in:         in,
		bus:        s.bus,
		pool:       p,
		implicit:   implicit,
		sourceDesc: sourceDesc,
	}, nil
}

// buildSource picks the source for a flow: an implicit source when the flow has
// no configured source or the service is in invoke mode, otherwise the real
// source built by the flow's connector.
//
//nolint:ireturn // returns the MessageSource interface
func (s *Service) buildSource(
	ctx context.Context,
	cfg types.FlowConfig,
	in chan<- *types.Message,
	set *connectorSet,
) (core.MessageSource, error) {
	if cfg.Source == nil || s.invokeMode {
		if cfg.Name == "" {
			return nil, fmt.Errorf("flow without a source requires a name to be callable")
		}
		return newImplicitSource(cfg.Name, in, s.flows), nil
	}
	return s.newSource(ctx, *cfg.Source, in, set)
}

// newSource resolves the source's connector (binding a configured instance or
// starting a default one on demand) and asks it to build a source that emits on
// the provided channel.
//
//nolint:ireturn // returns the MessageSource interface a connector provides
func (s *Service) newSource(
	ctx context.Context,
	cfg types.SourceConfig,
	in chan<- *types.Message,
	set *connectorSet,
) (core.MessageSource, error) {
	connector, err := set.resolveConnector(ctx, cfg)
	if err != nil {
		return nil, err
	}
	provider, ok := connector.(core.SourceProvider)
	if !ok {
		return nil, fmt.Errorf("connector for source type %q does not provide sources", cfg.Type)
	}

	source, err := provider.NewSource(cfg, in)
	if err != nil {
		return nil, fmt.Errorf("new source %q: %w", cfg.Type, err)
	}
	return source, nil
}

// startFlows starts each flow, returning those successfully started so the caller
// can tear them down on a later failure. Implicit-source flows start first so
// they are registered (callable by name) before any source-backed flow begins
// admitting traffic that may flow-ref them; reverse-order teardown then stops the
// source-backed flows first.
func (s *Service) startFlows(ctx context.Context, flows []*boundFlow) ([]*boundFlow, error) {
	started := make([]*boundFlow, 0, len(flows))
	for _, flow := range orderForStart(flows) {
		slog.Info("starting flow", "flow", flow.name, "workers", flow.workers, "source", flow.sourceDesc)
		if err := flow.start(ctx); err != nil {
			return started, fmt.Errorf("start flow %q: %w", flow.name, err)
		}
		started = append(started, flow)
	}
	return started, nil
}

// orderForStart returns flows with implicit-source flows first, preserving each
// group's original relative order.
func orderForStart(flows []*boundFlow) []*boundFlow {
	ordered := make([]*boundFlow, 0, len(flows))
	for _, flow := range flows {
		if flow.implicit {
			ordered = append(ordered, flow)
		}
	}
	for _, flow := range flows {
		if !flow.implicit {
			ordered = append(ordered, flow)
		}
	}
	return ordered
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
func stopConnectors(connectors []core.Connector) error {
	var firstErr error
	for i := len(connectors) - 1; i >= 0; i-- {
		if err := connectors[i].Stop(context.Background()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
