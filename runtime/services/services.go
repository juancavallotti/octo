// Package services is one of the runtime's two extension points — the other being
// connectors. A runtime service lives in a subpackage (standalone, k8s,
// observability, and future ones such as redis) and self-registers from an init
// function, the way connectors do. A binary chooses which it ships by which
// packages it blank-imports.
//
// A runtime service may implement either facet, or both:
//
// Provider — supplies core.RuntimeServices: leader election, KV, secrets, queues,
// topics, resources. Providers are module-selected by the RUNTIME_SERVICES_MODULE
// environment variable and call Register with their module name: only the one whose
// name matches becomes active, the rest are no-ops, so every imported provider may
// register unconditionally. New builds whichever selected itself. A provider may
// also expose optional capabilities the interface does not name, by implementing a
// side interface the caller type-asserts — see core.LogShipper.
//
// Hosted — runs for the life of the process instead of supplying capabilities:
// it owns CLI flags rather than YAML settings, starts before the first flow
// generation and stops after the last. Hosted services call RegisterHosted and are
// not module-selected, so every one compiled in runs. See HostedService.
//
// New capability belongs in one of these two facets, in a connector, or in the
// engine. It does not belong in a third registry: a reader looking for how the
// runtime is extended should find two answers, not four.
package services

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
)

// ModuleEnvVar names the environment variable selecting the runtime services
// provider.
const ModuleEnvVar = "RUNTIME_SERVICES_MODULE"

// DefaultModule is the provider selected when RUNTIME_SERVICES_MODULE is unset: a
// single-process, no-dependency default.
const DefaultModule = "standalone"

// Options carries construction inputs a provider may need beyond the context.
// ResourceRoot roots the standalone module's filesystem resource loader (the
// config directory); providers that resolve resources elsewhere (k8s) ignore it.
type Options struct {
	ResourceRoot string
}

// Factory constructs a runtime services provider. Construction may do real work
// (e.g. building an in-cluster Kubernetes client) and so may fail.
type Factory func(ctx context.Context, opts Options) (core.RuntimeServices, error)

// selected is the module name chosen by the environment, resolved once. Because
// every provider package imports this package, this initializes before any
// provider's init runs and so is set when they call Register.
var selected = resolveSelected()

func resolveSelected() string {
	if m := os.Getenv(ModuleEnvVar); m != "" {
		return m
	}
	return DefaultModule
}

var (
	mu         sync.Mutex
	active     Factory
	activeName string
)

// Module returns the selected module name (RUNTIME_SERVICES_MODULE, or the default
// when unset), for logging.
func Module() string { return selected }

// Register offers factory as the provider for module. It becomes the active
// provider only when module matches the selected module; otherwise the call is a
// no-op, so every imported provider may register unconditionally from its init.
func Register(module string, factory Factory) {
	if factory == nil {
		panic("services: nil factory for module " + module)
	}
	if module != selected {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if active != nil {
		panic(fmt.Sprintf("services: module %q already registered as the active provider", activeName))
	}
	active = factory
	activeName = module
}

// New builds the active provider. It errors when no imported provider selected
// itself for the chosen module (typically a missing blank import). The returned
// services are owned by the caller, which must Close them on shutdown.
//
//nolint:ireturn // returns the RuntimeServices interface the caller wires in
func New(ctx context.Context, opts Options) (core.RuntimeServices, error) {
	mu.Lock()
	factory := active
	mu.Unlock()
	if factory == nil {
		return nil, fmt.Errorf(
			"no runtime services provider registered for module %q "+
				"(set %s to a built-in module and ensure its package is imported)", selected, ModuleEnvVar)
	}
	return factory(ctx, opts)
}
