package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func byName(deps []Dependency, name string) Dependency {
	for _, d := range deps {
		if d.Name == name {
			return d
		}
	}
	return Dependency{}
}

// The four rows are always there, whatever this installation has. A page whose
// shape changed with the install would make a missing dependency look like a
// rendering bug rather than a fact about the deployment.
func TestCheckAlwaysReportsEveryDependency(t *testing.T) {
	deps := NewService(nil, nil, nil, nil).Check(context.Background())

	if len(deps) != 4 {
		t.Fatalf("dependencies = %d, want 4", len(deps))
	}
	for _, name := range []string{Postgres, Redis, NATS, Kubernetes} {
		d := byName(deps, name)
		if d.Name != name {
			t.Errorf("%s is missing from the report", name)
		}
		// The distinction the whole type exists for: never configured is not down.
		if d.Configured {
			t.Errorf("%s reports configured with no probe", name)
		}
		if d.Reachable {
			t.Errorf("%s reports reachable with no probe", name)
		}
	}
}

func TestCheckReportsAReachableDependency(t *testing.T) {
	ok := func(context.Context) error { return nil }

	deps := NewService(ok, nil, nil, nil).Check(context.Background())

	pg := byName(deps, Postgres)
	if !pg.Configured || !pg.Reachable {
		t.Errorf("postgres = %+v, want configured and reachable", pg)
	}
	if pg.Detail != "" {
		t.Errorf("detail = %q, want empty for a reachable dependency", pg.Detail)
	}
}

// Configured AND unreachable is the interesting row, and the one an operator
// acts on — so the reason has to survive into the report rather than only the log.
func TestCheckReportsWhyADependencyIsUnreachable(t *testing.T) {
	down := func(context.Context) error { return errors.New("connection refused") }

	deps := NewService(nil, down, nil, nil).Check(context.Background())

	rd := byName(deps, Redis)
	if !rd.Configured {
		t.Error("want redis reported as configured: it has a probe")
	}
	if rd.Reachable {
		t.Error("want redis reported as unreachable")
	}
	if rd.Detail != "connection refused" {
		t.Errorf("detail = %q, want the probe's reason", rd.Detail)
	}
}

// A wedged dependency must not hold the page. The probe here would block for
// longer than anyone would wait; what it gets is the deadline.
func TestCheckBoundsAProbeThatHangs(t *testing.T) {
	hangs := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}

	started := time.Now()
	deps := NewService(nil, nil, nil, hangs).Check(context.Background())
	elapsed := time.Since(started)

	if elapsed > probeTimeout*2 {
		t.Errorf("Check took %s, want it bounded near %s", elapsed, probeTimeout)
	}
	k8s := byName(deps, Kubernetes)
	if k8s.Reachable {
		t.Error("want a hung probe reported as unreachable")
	}
}

// A caller that gave up must not be waited on either — the page was closed, and
// four probes at their own ceiling would outlive the request that asked for them.
func TestCheckRespectsACancelledCaller(t *testing.T) {
	hangs := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	NewService(hangs, hangs, hangs, hangs).Check(ctx)

	if elapsed := time.Since(started); elapsed > probeTimeout {
		t.Errorf("Check took %s after the caller cancelled, want it to return at once", elapsed)
	}
}
