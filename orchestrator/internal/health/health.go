// Package health answers one question about each thing this installation depends
// on: can the orchestrator reach it right now.
//
// It exists because the answer used to live only in pod logs. Every other admin
// page configures something; this one reports, and what it reports is the first
// thing anyone checks when the platform is behaving strangely — which of the four
// processes underneath it is actually up.
//
// Deliberately shallow. A check here proves the orchestrator can complete one
// round trip, not that the dependency is healthy in any deeper sense: Postgres
// answering a ping says nothing about replication lag, and Redis answering one
// says nothing about how full it is. A page that claimed more than that would be
// worse than this one, because it would be believed.
package health

import (
	"context"
	"time"
)

// Names of the dependencies reported, in the order the page shows them: storage
// first, because nothing works without it, then the two brokers, then the
// cluster, then the optional embedding server.
//
// Embeddings is last because it is the only one an installation may simply not
// have. The other four are how the platform runs; without this one, searching
// agent memory matches text instead of ranking by meaning and everything else is
// unchanged — which is why "not configured" here is the ordinary answer rather
// than a gap to explain.
const (
	Postgres   = "postgres"
	Redis      = "redis"
	NATS       = "nats"
	Kubernetes = "kubernetes"
	Embeddings = "embeddings"
)

// probeTimeout bounds one check. Short on purpose — this is a page an operator
// reloads while something is wrong, and four dependencies each taking their time
// to fail would make the page itself feel like another broken thing.
const probeTimeout = 3 * time.Second

// Dependency is one thing checked.
type Dependency struct {
	Name string
	// Configured reports whether this installation has the dependency at all. A
	// dependency that was never configured is not down: an orchestrator with no
	// cluster access is a supported way to run, and reporting it as a failure
	// would send someone looking for a fault that does not exist.
	Configured bool
	Reachable  bool
	// Detail is why it is unreachable, or empty. It carries no credentials — the
	// probes are written so that what reaches here is a transport error.
	Detail string
	// LatencyMs is how long the round trip took, when one was made.
	LatencyMs int64
}

// probe is one dependency's check. Returning an error means unreachable.
type probe func(ctx context.Context) error

// Service checks the dependencies this orchestrator was given.
//
// Each probe is optional. A nil one means "not configured", which is a different
// answer from "did not respond" and is reported as such.
type Service struct {
	probes []namedProbe
}

type namedProbe struct {
	name  string
	probe probe
}

// NewService returns a Service that checks whichever probes are non-nil, always
// reporting all five names so the page's shape does not change with the install.
func NewService(postgres, redis, nats, kubernetes, embeddings probe) *Service {
	return &Service{probes: []namedProbe{
		{Postgres, postgres},
		{Redis, redis},
		{NATS, nats},
		{Kubernetes, kubernetes},
		{Embeddings, embeddings},
	}}
}

// Check runs every probe and reports what came back.
//
// Sequential rather than concurrent. Five probes at three seconds each is a
// worst case nobody reaches — a dependency that is down refuses immediately —
// and the failure this page exists to diagnose is one dependency being wedged,
// which is exactly the case where four goroutines racing a shared context makes
// the result harder to read rather than faster.
func (s *Service) Check(ctx context.Context) []Dependency {
	out := make([]Dependency, 0, len(s.probes))
	for _, p := range s.probes {
		out = append(out, check(ctx, p))
	}
	return out
}

func check(ctx context.Context, p namedProbe) Dependency {
	if p.probe == nil {
		return Dependency{Name: p.name}
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	started := time.Now()
	err := p.probe(probeCtx)
	elapsed := time.Since(started).Milliseconds()

	dep := Dependency{Name: p.name, Configured: true, LatencyMs: elapsed}
	if err != nil {
		dep.Detail = err.Error()
		return dep
	}
	dep.Reachable = true
	return dep
}
