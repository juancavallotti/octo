package services

import (
	"context"
	"flag"
	"strings"
	"sync"

	"github.com/juancavallotti/octo/runtime/types"
)

// HostedService is the second facet a runtime service may implement: a component
// that runs for the life of the process rather than supplying core.RuntimeServices.
//
// It is not declared in a config and has no YAML — it configures itself from the
// CLI, which is why it registers flags rather than reading settings. It starts
// before the first flow generation and stops after the last, so it spans every
// --watch reload the way the services provider does.
//
// The two facets are independent: a package may implement one, the other, or both.
// The observability service implements only this one, and is shared by every
// module rather than duplicated into each — which is why it sits beside them.
type HostedService interface {
	// Name identifies the service in logs and startup errors.
	Name() string
	// Flags registers the service's flags on the `octo run` flagset, before it is
	// parsed. Resolve environment defaults here: a flag whose default is the env
	// value gives "flag wins when passed" for free, and prints the effective value
	// in --help.
	Flags(fs *flag.FlagSet)
	// Usage returns the help section appended to `octo --help`, or "" for a service
	// with no flags worth documenting.
	Usage() string
	// Start brings the service up. It must not block: anything long-running belongs
	// on a goroutine of the service's own. An error here fails the run, so bind
	// ports and open files now rather than logging a failure later.
	Start(ctx context.Context, health *Health) error
	// Stop shuts the service down. It is called with a context that outlives
	// cancellation of the run, so a graceful drain has somewhere to happen.
	Stop(ctx context.Context) error
}

// ConfigAware is the optional half of the hosted facet: a service that wants to
// see each config generation the runtime starts. It is called once per generation,
// again on every --watch reload, after the config loads and before the flows run.
//
// A generation is not a fresh start: counters and other accumulated state must
// survive it, so treat this as "the config is now X", not "begin".
type ConfigAware interface {
	Configured(config types.Config)
}

var (
	hostedMu sync.Mutex
	hosted   []HostedService
)

// RegisterHosted adds a hosted service, to be started by `octo run` in
// registration order. Unlike Register it is never a no-op: hosted services are not
// module-selected, so every one compiled into the binary runs. A binary chooses
// which it ships by which packages it blank-imports.
func RegisterHosted(service HostedService) {
	if service == nil {
		panic("services: nil hosted service")
	}
	hostedMu.Lock()
	defer hostedMu.Unlock()
	hosted = append(hosted, service)
}

// Hosted returns the registered hosted services in registration order. The result
// is a copy, so a caller may hold it across the run.
func Hosted() []HostedService {
	hostedMu.Lock()
	defer hostedMu.Unlock()
	return append([]HostedService(nil), hosted...)
}

// HostedUsage returns every registered service's help section, concatenated in
// registration order, for appending to the CLI's usage page. It is empty when
// nothing registered or nothing documents itself.
func HostedUsage() string {
	var b strings.Builder
	for _, service := range Hosted() {
		usage := strings.TrimRight(service.Usage(), "\n")
		if usage == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(usage)
	}
	return b.String()
}
