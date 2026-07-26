package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

// namespace prefixes every metric this runtime exports.
const namespace = "octo"

// Label names. Every one of them is filled from the config — a flow's name, a
// block's label, a fixed outcome — and never from message data, which is what
// keeps cardinality bounded by what someone wrote rather than by traffic.
const (
	labelFlow    = "flow"
	labelOutcome = "outcome"
	labelBlock   = "block"
)

// buildVersion and buildDate identify the binary in octo_build_info. They are set
// by SetBuildInfo, because the version is a const in package main and a runtime
// service cannot import it back.
var buildVersion, buildDate = "unknown", ""

// SetBuildInfo records the binary's version and build date for octo_build_info.
// The CLI calls it from the same build-tagged file that imports this package, so
// the coupling to package main lives behind the same gate as the service itself.
func SetBuildInfo(version, date string) {
	buildVersion, buildDate = version, date
}

// metrics holds the collectors and the registry they are exported through. The
// registry is the service's own rather than prometheus.DefaultRegisterer, so a
// dependency that registers a global collector cannot appear on octo's /metrics,
// and two Services in one process cannot collide.
type metrics struct {
	registry *prometheus.Registry

	// Per-flow RED, built from the flow-event bus.
	flowMessages *prometheus.CounterVec
	flowErrors   *prometheus.CounterVec
	flowDuration *prometheus.HistogramVec
	flowInFlight *prometheus.GaugeVec

	// Per-generation, set from the config each generation starts with.
	flows      prometheus.Gauge
	connectors prometheus.Gauge
}

// newMetrics builds the registry and every collector on it. health backs the
// octo_ready gauge, which is read at scrape time rather than pushed, so it can
// never drift from what /readyz answers.
func newMetrics(health *services.Health) *metrics {
	m := newFlowCollectors()
	m.registry.MustRegister(
		m.flowMessages, m.flowErrors, m.flowDuration, m.flowInFlight, m.flows, m.connectors,
	)
	m.registerProcessCollectors(health)
	return m
}

// newFlowCollectors builds the collectors octo fills itself, unregistered.
func newFlowCollectors() *metrics {
	return &metrics{
		registry: prometheus.NewRegistry(),

		flowMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flow_messages_total",
			Help:      "Messages a flow finished with, by outcome.",
		}, []string{labelFlow, labelOutcome}),

		flowErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "flow_errors_total",
			Help: "Unhandled failures, by the block they came from. " +
				"A message a flow's error path recovered completed and is not counted here.",
		}, []string{labelFlow, labelBlock}),

		flowDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "flow_duration_seconds",
			Help:      "How long a flow took to process a message, including its error path.",
			Buckets:   prometheus.DefBuckets,
		}, []string{labelFlow, labelOutcome}),

		flowInFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "flow_in_flight",
			Help: "Messages currently being processed by a flow. " +
				"Sustained at the flow's worker count with flat throughput means every worker is blocked.",
		}, []string{labelFlow}),

		flows: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "flows",
			Help:      "Flows in the running configuration.",
		}),

		connectors: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "connectors",
			Help:      "Connectors in the running configuration.",
		}),
	}
}

// registerProcessCollectors adds what describes the process rather than the flows:
// readiness, build identity, and the Go runtime and OS collectors.
func (m *metrics) registerProcessCollectors(health *services.Health) {
	// Read at scrape time, so it cannot disagree with /readyz.
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "ready",
		Help:      "1 when the runtime is serving, 0 otherwise. The same signal /readyz answers.",
	}, func() float64 {
		if health != nil && health.Ready() {
			return 1
		}
		return 0
	}))

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "Always 1; the labels carry which binary is running.",
	}, []string{"version", "build_date", "services_module"})
	buildInfo.WithLabelValues(buildVersion, buildDate, services.Module()).Set(1)
	m.registry.MustRegister(buildInfo)

	// Free, and the answer to questions nothing else in the runtime can answer:
	// goroutine count, heap and GC behaviour, RSS, CPU and open file descriptors.
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// onFlowEvent turns one flow event into metric updates. It runs on the flow's own
// worker goroutine, because the flow-event bus fans out synchronously — so it does
// a counter increment, a gauge step and one observation, and nothing else.
func (m *metrics) onFlowEvent(event types.FlowEvent) {
	if event.Kind == types.FlowEventStarted {
		m.flowInFlight.WithLabelValues(event.Flow).Inc()
		return
	}

	// Every other kind is terminal: the message is no longer in flight.
	m.flowInFlight.WithLabelValues(event.Flow).Dec()
	outcome := string(event.Kind)
	m.flowMessages.WithLabelValues(event.Flow, outcome).Inc()
	m.flowDuration.WithLabelValues(event.Flow, outcome).Observe(event.Duration.Seconds())

	// Failed means unhandled: a message the error path recovered was published as
	// completed and never reaches here, which is what makes this an error rate
	// rather than an incident count.
	if event.Kind == types.FlowEventFailed {
		m.flowErrors.WithLabelValues(event.Flow, event.Block).Inc()
	}
}

// observeConfig records the shape of a generation and creates each flow's series
// at zero, so a flow that has taken no traffic yet is visible as 0 rather than
// absent — the difference between "nothing is arriving" and "I cannot tell".
//
// It only ever adds series. A reload does not reset counters: resetting would make
// rate() read a reload as a counter reset and invent a spike.
func (m *metrics) observeConfig(config types.Config) {
	m.flows.Set(float64(len(config.Flows)))
	m.connectors.Set(float64(len(config.Connectors)))

	outcomes := []string{
		string(types.FlowEventCompleted),
		string(types.FlowEventDropped),
		string(types.FlowEventFailed),
	}
	for _, flow := range config.Flows {
		m.flowInFlight.WithLabelValues(flow.Name)
		for _, outcome := range outcomes {
			m.flowMessages.WithLabelValues(flow.Name, outcome)
			m.flowDuration.WithLabelValues(flow.Name, outcome)
		}
	}
	// octo_flow_errors_total is deliberately not pre-created: its block label is
	// only known once something fails, and inventing a series per (flow, block)
	// pair would be a guess about which blocks can fail.
}
