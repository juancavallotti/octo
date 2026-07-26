package observability

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

// newMetricsService returns a started service with --metrics on, bound to an
// OS-assigned port.
func newMetricsService(t *testing.T) *Service {
	t.Helper()
	// Neutralize the ambient environment: these flags default to it, so a
	// developer or CI runner with OCTO_OBSERVABILITY set would otherwise get a
	// service that never binds and a pile of confusing failures downstream.
	t.Setenv(envEnabled, "false")
	t.Setenv(envMetrics, "false")

	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	if err := fs.Parse([]string{"--observability=true", "--observability-addr", "127.0.0.1:0", "--metrics"}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	health := services.NewHealth()
	if err := svc.Start(context.Background(), health); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := svc.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return svc
}

// flowEvent builds a terminal flow event.
func flowEvent(kind types.FlowEventKind, flow string, d time.Duration, block string) types.FlowEvent {
	return types.FlowEvent{Kind: kind, Flow: flow, Duration: d, Block: block}
}

func TestMetricsCountsOutcomesPerFlow(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.onFlowEvent(types.FlowEvent{Kind: types.FlowEventStarted, Flow: "orders"})
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 10*time.Millisecond, ""))
	m.onFlowEvent(types.FlowEvent{Kind: types.FlowEventStarted, Flow: "orders"})
	m.onFlowEvent(flowEvent(types.FlowEventDropped, "orders", time.Millisecond, ""))
	m.onFlowEvent(types.FlowEvent{Kind: types.FlowEventStarted, Flow: "audit"})
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "audit", time.Millisecond, ""))

	if got := testutil.ToFloat64(m.flowMessages.WithLabelValues("orders", "completed")); got != 1 {
		t.Errorf("orders completed = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.flowMessages.WithLabelValues("orders", "dropped")); got != 1 {
		t.Errorf("orders dropped = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.flowMessages.WithLabelValues("audit", "completed")); got != 1 {
		t.Errorf("audit completed = %v, want 1", got)
	}
}

// The in-flight gauge is the signal nothing else in the runtime provides: it goes
// up on started and back down on every terminal outcome, so a flow whose workers
// are all blocked shows a flat non-zero value.
func TestMetricsInFlightRisesAndFalls(t *testing.T) {
	m := newMetrics(services.NewHealth())

	for i := 0; i < 3; i++ {
		m.onFlowEvent(types.FlowEvent{Kind: types.FlowEventStarted, Flow: "orders"})
	}
	if got := testutil.ToFloat64(m.flowInFlight.WithLabelValues("orders")); got != 3 {
		t.Fatalf("in flight after 3 starts = %v, want 3", got)
	}

	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 0, ""))
	m.onFlowEvent(flowEvent(types.FlowEventDropped, "orders", 0, ""))
	m.onFlowEvent(flowEvent(types.FlowEventFailed, "orders", 0, "charge"))

	if got := testutil.ToFloat64(m.flowInFlight.WithLabelValues("orders")); got != 0 {
		t.Errorf("in flight after all three terminal kinds = %v, want 0", got)
	}
}

// The error counter is labelled by the block the failure came from, which is what
// turns "this integration is erroring" into "the charge block is erroring".
func TestMetricsErrorsAreLabelledByBlock(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.onFlowEvent(flowEvent(types.FlowEventFailed, "orders", 0, "charge"))
	m.onFlowEvent(flowEvent(types.FlowEventFailed, "orders", 0, "charge"))
	m.onFlowEvent(flowEvent(types.FlowEventFailed, "orders", 0, "notify"))

	if got := testutil.ToFloat64(m.flowErrors.WithLabelValues("orders", "charge")); got != 2 {
		t.Errorf("charge errors = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.flowErrors.WithLabelValues("orders", "notify")); got != 1 {
		t.Errorf("notify errors = %v, want 1", got)
	}
}

// Errors are unhandled only. A completed event carries no error even when the
// message failed on the way — the error path recovered it — so nothing increments.
func TestMetricsRecoveredFailureIsNotAnError(t *testing.T) {
	m := newMetrics(services.NewHealth())

	// What a recovered failure actually publishes: completed, no block.
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", time.Millisecond, ""))

	if got := testutil.CollectAndCount(m.flowErrors); got != 0 {
		t.Errorf("error series after a recovered failure = %d, want none", got)
	}
	if got := testutil.ToFloat64(m.flowMessages.WithLabelValues("orders", "completed")); got != 1 {
		t.Errorf("completed = %v, want 1", got)
	}
}

// Only failures carry a block label, so a dropped or completed message must not
// create an error series at all.
func TestMetricsNonFailuresCreateNoErrorSeries(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 0, ""))
	m.onFlowEvent(flowEvent(types.FlowEventDropped, "orders", 0, ""))

	if got := testutil.CollectAndCount(m.flowErrors); got != 0 {
		t.Errorf("error series = %d, want none", got)
	}
}

func TestMetricsObservesDuration(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 250*time.Millisecond, ""))
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 750*time.Millisecond, ""))

	const want = `
		# HELP octo_flow_duration_seconds How long a flow took to process a message, including its error path.
		# TYPE octo_flow_duration_seconds histogram
		octo_flow_duration_seconds_bucket{flow="orders",outcome="completed",le="0.25"} 1
		octo_flow_duration_seconds_bucket{flow="orders",outcome="completed",le="0.5"} 1
		octo_flow_duration_seconds_bucket{flow="orders",outcome="completed",le="1"} 2
		octo_flow_duration_seconds_sum{flow="orders",outcome="completed"} 1
		octo_flow_duration_seconds_count{flow="orders",outcome="completed"} 2
	`
	err := testutil.CollectAndCompare(m.flowDuration, strings.NewReader(want),
		"octo_flow_duration_seconds_bucket", "octo_flow_duration_seconds_sum", "octo_flow_duration_seconds_count")
	if err != nil {
		t.Error(err)
	}
}

// A flow with no traffic must be visible as 0 rather than absent: "nothing is
// arriving" and "I cannot tell" look identical when the series is missing.
func TestConfiguredPreCreatesFlowSeriesAtZero(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.observeConfig(types.Config{
		Flows:      []types.FlowConfig{{Name: "orders"}, {Name: "audit"}},
		Connectors: []types.ConnectorConfig{{Name: "web"}},
	})

	if got := testutil.ToFloat64(m.flows); got != 2 {
		t.Errorf("octo_flows = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.connectors); got != 1 {
		t.Errorf("octo_connectors = %v, want 1", got)
	}
	for _, flow := range []string{"orders", "audit"} {
		for _, outcome := range []string{"completed", "dropped", "failed"} {
			if got := testutil.ToFloat64(m.flowMessages.WithLabelValues(flow, outcome)); got != 0 {
				t.Errorf("%s/%s = %v, want a zero series", flow, outcome, got)
			}
		}
	}
	// 2 flows x 3 outcomes.
	if got := testutil.CollectAndCount(m.flowMessages); got != 6 {
		t.Errorf("pre-created series = %d, want 6", got)
	}
}

// A reload must not reset counters: rate() reads a reset as a counter restart and
// invents a spike. Reloads only ever add series.
func TestConfiguredDoesNotResetCountersOnReload(t *testing.T) {
	m := newMetrics(services.NewHealth())

	m.observeConfig(types.Config{Flows: []types.FlowConfig{{Name: "orders"}}})
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 0, ""))
	m.onFlowEvent(flowEvent(types.FlowEventCompleted, "orders", 0, ""))

	// A reload that adds a flow.
	m.observeConfig(types.Config{Flows: []types.FlowConfig{{Name: "orders"}, {Name: "audit"}}})

	if got := testutil.ToFloat64(m.flowMessages.WithLabelValues("orders", "completed")); got != 2 {
		t.Errorf("orders completed after a reload = %v, want 2 — a reload must not reset", got)
	}
	if got := testutil.ToFloat64(m.flows); got != 2 {
		t.Errorf("octo_flows after a reload = %v, want 2", got)
	}
}

// octo_ready is read at scrape time so it cannot drift from what /readyz answers.
func TestReadyGaugeTracksHealth(t *testing.T) {
	health := services.NewHealth()
	m := newMetrics(health)

	tests := []struct {
		state services.State
		want  float64
	}{
		{services.StateStarting, 0},
		{services.StateReady, 1},
		{services.StateReloading, 0},
		{services.StateDraining, 0},
	}
	for _, tt := range tests {
		health.SetState(tt.state)
		if got := gaugeValue(t, m, "octo_ready"); got != tt.want {
			t.Errorf("octo_ready in state %q = %v, want %v", tt.state, got, tt.want)
		}
	}
}

// gaugeValue scrapes the registry and returns the named single-sample gauge, which
// is how a GaugeFunc has to be read: it has no value until it is collected.
func gaugeValue(t *testing.T, m *metrics, name string) float64 {
	t.Helper()
	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatalf("%s not registered", name)
	return 0
}

// /metrics is served only with --metrics, and the index never advertises a 404.
func TestMetricsEndpointServedOnlyWhenEnabled(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		svc := newMetricsService(t)
		status, body := get(t, svc, pathMetrics)
		if status != http.StatusOK {
			t.Fatalf("GET /metrics = %d, want 200", status)
		}
		for _, want := range []string{"octo_build_info", "octo_ready", "go_goroutines", "process_"} {
			if !strings.Contains(body, want) {
				t.Errorf("/metrics does not export %s", want)
			}
		}
		if _, index := get(t, svc, pathIndex); !strings.Contains(index, pathMetrics) {
			t.Error("index does not list /metrics while it is served")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		svc, _ := newTestService(t)
		if status, _ := get(t, svc, pathMetrics); status != http.StatusNotFound {
			t.Errorf("GET /metrics without --metrics = %d, want 404", status)
		}
		if _, index := get(t, svc, pathIndex); strings.Contains(index, pathMetrics) {
			t.Error("index advertises /metrics when nothing serves it")
		}
	})
}

// The service must report a flow it actually saw, end to end through the real
// process-wide bus the runtime publishes on.
func TestMetricsEndToEndOverTheEventBus(t *testing.T) {
	svc := newMetricsService(t)
	svc.Configured(types.Config{Flows: []types.FlowConfig{{Name: "e2e"}}})

	bus := core.DefaultEventBus()
	bus.Publish(types.FlowEvent{Kind: types.FlowEventStarted, Flow: "e2e"})
	bus.Publish(types.FlowEvent{
		Kind: types.FlowEventFailed, Flow: "e2e", Duration: 5 * time.Millisecond,
		Block: "charge", Err: errors.New("card declined"),
	})

	_, body := get(t, svc, pathMetrics)
	for _, want := range []string{
		`octo_flow_messages_total{flow="e2e",outcome="failed"} 1`,
		`octo_flow_errors_total{block="charge",flow="e2e"} 1`,
		`octo_flow_in_flight{flow="e2e"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics is missing %q", want)
		}
	}
}
