// Package services selects and constructs the runtime services provider chosen at
// startup by the RUNTIME_SERVICES_MODULE environment variable.
//
// Providers live in subpackages (standalone, k8s, and future ones such as redis).
// Each self-registers from an init function by calling Register with its module
// name: only the provider whose name matches the selected module becomes active,
// the rest are no-ops. A binary blank-imports the provider packages it ships
// (mirroring how connectors are wired), then calls New to build whichever one
// selected itself.
package services

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/juancavallotti/octo/core"
)

// ModuleEnvVar names the environment variable selecting the runtime services
// provider.
const ModuleEnvVar = "RUNTIME_SERVICES_MODULE"

// DefaultModule is the provider selected when RUNTIME_SERVICES_MODULE is unset: a
// single-process, no-dependency default.
const DefaultModule = "standalone"

// Factory constructs a runtime services provider. Construction may do real work
// (e.g. building an in-cluster Kubernetes client) and so may fail.
type Factory func(ctx context.Context) (core.RuntimeServices, error)

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
func New(ctx context.Context) (core.RuntimeServices, error) {
	mu.Lock()
	factory := active
	mu.Unlock()
	if factory == nil {
		return nil, fmt.Errorf(
			"no runtime services provider registered for module %q "+
				"(set %s to a built-in module and ensure its package is imported)", selected, ModuleEnvVar)
	}
	return factory(ctx)
}
