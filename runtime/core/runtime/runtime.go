// Package runtime is the application layer that wires configured connectors and
// flows into a running service. It binds the public core contracts and registries
// to the internal engine: building each flow's processing tree, starting its
// source and workers, and tearing everything down on shutdown.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	goruntime "runtime"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/engine"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// Service runs the configured connectors and flows until its context is
// cancelled.
type Service struct {
	config     types.Config
	registry   *core.Registry
	blocks     *core.BlockRegistry
	bus        *core.EventBus
	flows      *flowRegistry
	services   core.RuntimeServices
	invokeMode bool
	// breakpoint, when set, makes one addressed block record the message it
	// produced and halt the flow. Invoke-mode only; see WithBreakpoint.
	breakpoint *core.Breakpoint
	// spies, when set, makes each addressed block record what crosses it, without
	// otherwise changing the run. Invoke-mode only; see WithSpies.
	spies *core.Spies
	// mocks, when set, replaces each addressed block with a canned answer, so a flow
	// can run without the work its real blocks would do. Invoke-mode only; see
	// WithMocks.
	mocks *core.Mocks
	// events dispatches the pre/post-invoke events every block emits. It defaults to
	// the process-wide dispatcher; see WithBlockEvents.
	events *core.BlockEvents
	ready  chan struct{}
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

// WithBreakpoint makes the service break on the block the breakpoint addresses:
// the block is wrapped in an implicit breakpoint block that records the message it
// produced and halts the flow. It backs the CLI's `invoke --break-at`.
//
// It requires invoke mode. A breakpoint on a source-backed service would halt
// whichever production message happened to arrive first, so Run refuses it rather
// than letting a debugging aid loose on live traffic.
func WithBreakpoint(bp *core.Breakpoint) ServiceOption {
	return func(s *Service) { s.breakpoint = bp }
}

// WithSpies makes the service record what crosses each block the spies address: the
// block is wrapped in an implicit spy block that reports every message through it,
// and the flow otherwise runs exactly as it would have. It backs the CLI's `invoke
// --spies`.
//
// It requires invoke mode. A spy is read-only, but nothing drains the collector
// outside an invoke: on a source-backed service it would grow without bound, hoarding
// the body of every message that crossed the block for nobody to read.
func WithSpies(spies *core.Spies) ServiceOption {
	return func(s *Service) { s.spies = spies }
}

// WithMocks makes the service answer for each block the mocks address instead of
// running it: the block is replaced by an implicit mock block that returns a canned
// outcome. It backs the CLI's `invoke --mocks`, and it is what lets a flow whose
// blocks call an API, an LLM or a database be exercised without any of that.
//
// It requires invoke mode. A mock on a source-backed service would answer production
// traffic with a canned response.
func WithMocks(mocks *core.Mocks) ServiceOption {
	return func(s *Service) { s.mocks = mocks }
}

// WithBlockEvents wires the dispatcher the flows' blocks emit their pre- and
// post-invoke events to, in place of the process-wide default. Use it to give a
// Service a dispatcher of its own — a test that must not see another test's
// listeners, or an embedder running two Services in one process.
//
// It needs no invoke-mode guard, unlike the debug options above: block events
// retain nothing and cost nothing until something registers a listener, so they
// are safe on a source-backed service carrying live traffic.
func WithBlockEvents(events *core.BlockEvents) ServiceOption {
	return func(s *Service) { s.events = events }
}

// WithRuntimeServices wires the runtime services (leader election, KV) this
// generation exposes to connectors and blocks. The services are injected into the
// run context and the block dependencies. They are owned by the caller (the CLI
// constructs them once and reuses them across watch-mode reloads), so the Service
// never closes them. When unset, the no-op services apply.
func WithRuntimeServices(svc core.RuntimeServices) ServiceOption {
	return func(s *Service) { s.services = svc }
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
		events:   core.DefaultBlockEvents(),
		ready:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// resourceLoader returns the resource loader blocks use: the wired runtime
// services' loader (or the no-op loader when no services are set), wrapped so
// declared template aliases resolve to their underlying resource id.
//
//nolint:ireturn // returns the ResourceLoader interface a block depends on
func (s *Service) resourceLoader() core.ResourceLoader {
	var base core.ResourceLoader = core.NoopResourceLoader{}
	if s.services != nil {
		base = s.services.Resources()
	}
	return withTemplateAliases(base, s.config.Resources.Templates)
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

	// Carry the runtime services on the context so connector and source Start calls
	// (which both receive this ctx) can reach leader election and the KV store. The
	// services are owned by the caller; the Service does not close them.
	ctx = core.ContextWithRuntimeServices(ctx, s.services)

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
// block chain. The debug blocks are injected into the config first, so the flow
// builder sees the rewritten blocks like any others.
func (s *Service) buildFlows(ctx context.Context, set *connectorSet) ([]*boundFlow, error) {
	if err := s.applyDebug(); err != nil {
		return nil, err
	}

	flows := make([]*boundFlow, 0, len(s.config.Flows))
	// One state key must mean one block. Two blocks sharing one would read and
	// write each other's stored state — an aggregator would fold two unrelated
	// streams into the same groups and emit them twice — so the collision is
	// caught here, where the whole config is in view, rather than in production.
	stateKeys := make(map[string]string, len(s.config.Flows))
	for i := range s.config.Flows {
		flow, err := s.buildFlow(ctx, s.config.Flows[i], set)
		if err != nil {
			return nil, fmt.Errorf("build flow %q: %w", s.config.Flows[i].Name, err)
		}
		if err := claimStateKeys(stateKeys, s.config.Flows[i].Name, flow); err != nil {
			return nil, err
		}
		flows = append(flows, flow)
	}
	return flows, nil
}

// claimStateKeys records each of the flow's state keys against its owner,
// failing when one is already claimed.
func claimStateKeys(claimed map[string]string, name string, flow *boundFlow) error {
	keys := flow.root.StateKeys()
	if flow.errorPath != nil {
		keys = append(keys, flow.errorPath.StateKeys()...)
	}
	for _, key := range keys {
		if owner, taken := claimed[key]; taken {
			return fmt.Errorf(
				"flow %q: state key %q is already used by flow %q; give one of the blocks its own storeKey",
				name, key, owner)
		}
		claimed[key] = name
	}
	return nil
}

// applyDebug rewrites the config with the debug blocks the caller asked for. It is
// called once, before any flow is built, so both the root chain and the error path
// build from the already-injected config.
//
// The order is not arbitrary: mocks, then spies, then the breakpoint, so a block
// that is all three comes out as breakpoint[spy[mock]].
//
//   - The mock is innermost because it replaces its target. Injected after a spy it
//     would replace the spy, and the spy would be gone.
//   - The breakpoint is outermost because it halts the flow by writing a reserved
//     variable into the message. A spy wrapped around it would record that
//     bookkeeping as part of the block's output.
//
// It refuses any of them outside invoke mode. A breakpoint would halt whichever
// production message happened to arrive first, and a mock would answer it with a
// canned response. A spy is read-only, but nothing drains its collector outside an
// invoke, so it would grow without bound.
func (s *Service) applyDebug() error {
	if s.breakpoint == nil && s.spies == nil && s.mocks == nil {
		return nil
	}
	if !s.invokeMode {
		return errors.New(
			"breakpoints, spies and mocks require invoke mode: " +
				"they would halt, buffer, or fake live traffic on a source-backed flow")
	}

	// Checked before anything is rewritten, while every address still resolves: a
	// mock removes the blocks inside its target, so a spy addressed in there would
	// otherwise fail as "no such block" once the mock had gone in.
	if err := checkMockConflicts(s.mocks, s.debugAddresses()); err != nil {
		return err
	}

	if err := injectMocks(&s.config, s.mocks); err != nil {
		return err
	}
	if err := injectSpies(&s.config, s.spies); err != nil {
		return err
	}
	if s.breakpoint == nil {
		return nil
	}
	return injectBreakpoint(&s.config, s.breakpoint.Address())
}

// debugAddresses lists the addresses of the debug features that observe a block,
// rather than replace it — the ones a mock can strand.
func (s *Service) debugAddresses() []resolver {
	var addresses []resolver
	if s.spies != nil {
		for _, addr := range s.spies.Addresses() {
			addresses = append(addresses, resolver{kind: spyBlockType, addr: addr})
		}
	}
	if s.breakpoint != nil {
		addresses = append(addresses, resolver{kind: breakpointBlockType, addr: s.breakpoint.Address()})
	}
	return addresses
}

func (s *Service) buildFlow(ctx context.Context, cfg types.FlowConfig, set *connectorSet) (*boundFlow, error) {
	workers := resolveWorkers(cfg.Workers, goruntime.GOMAXPROCS(0))
	in := make(chan *types.Message, resolveBuffer(cfg.Buffer, workers))

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
		Connector:  set.lookup,
		Flows:      s.flows,
		Env:        s.config.ResolvedEnv,
		Services:   s.services,
		Resources:  s.resourceLoader(),
		Breakpoint: s.breakpoint,
		Spies:      s.spies,
		Mocks:      s.mocks,
		Events:     s.events,
	}
	root, err := engine.BuildRoot(cfg, s.blocks, p, s.config.Processors, deps)
	if err != nil {
		return nil, err
	}

	var errorPath *engine.Flow
	if len(cfg.Error) > 0 {
		// The error path is a root-level chain (it owns the same pool/deps), but its
		// blocks address off the flow's error chain rather than its process chain.
		errorPath, err = engine.BuildErrorPath(cfg, s.blocks, p, s.config.Processors, deps)
		if err != nil {
			return nil, fmt.Errorf("flow %q error path: %w", cfg.Name, err)
		}
	}

	bf := &boundFlow{
		name:           cfg.Name,
		source:         source,
		root:           root,
		errorPath:      errorPath,
		workers:        workers,
		workersDerived: cfg.Workers <= 0,
		in:             in,
		bus:            s.bus,
		pool:           p,
		implicit:       implicit,
		sourceDesc:     sourceDesc,
	}

	bf.installResume()
	return bf, nil
}

// installResume hands each root chain the hook its continuations resume through.
// It closes over the bound flow because scheduling a resumed tail needs the
// worker pool and reporting it needs the event bus and the error path — none of
// which the engine has. It runs well before the source admits any traffic.
func (bf *boundFlow) installResume() {
	bf.root.SetResume(bf.resume)
	if bf.errorPath != nil {
		bf.errorPath.SetResume(bf.resume)
	}
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

	source, err := provider.NewSource(cfg, in, core.SourceDeps{Resources: s.resourceLoader()})
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
		slog.Info("starting flow", "flow", flow.name, "workers", flow.workers,
			"workersOrigin", workersOrigin(flow.workersDerived), "source", flow.sourceDesc)
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
