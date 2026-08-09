package k8s

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// TraceSubject is the shared, deployment-agnostic NATS subject every runtime
// publishes its trace records to, one JSON object per message.
//
// Like LogSubject and unlike the queue subjects (octo.<id>.q.*), it is NOT scoped
// per deployment: a single traces engine consumes it as a competing consumer
// across all deployments, and the deployment identity travels inside each record
// so it can attribute them. Scoping the subject instead would make the consumer's
// subscription list grow with the number of deployments, which is the thing an
// aggregator exists to avoid.
const TraceSubject = "internal.traces"

// traceRecord is what goes on the wire: the record itself plus the deployment it
// came from. The embedded event flattens into the same JSON object, so a consumer
// sees one flat record rather than an envelope to unwrap — the same shape the log
// records already arrive in, where the deployment fields sit beside the message.
type traceRecord struct {
	types.TraceEvent
	DeploymentID string `json:"deploymentId"`
	AppName      string `json:"appName,omitempty"`
	AppVersion   string `json:"appVersion,omitempty"`
}

// natsTraceSink publishes trace records to TraceSubject.
type natsTraceSink struct {
	conn         *nats.Conn
	deploymentID string
	appName      string
	appVersion   string
}

// newTraceSink builds the sink. name and version are empty for an unnamed or
// untagged deployment; the id always exists, and is what the consumer keys on.
func newTraceSink(conn *nats.Conn, deploymentID, name, version string) *natsTraceSink {
	return &natsTraceSink{
		conn:         conn,
		deploymentID: deploymentID,
		appName:      name,
		appVersion:   version,
	}
}

// Write publishes one record.
//
// Unlike the log sink, which drops its publish errors because a broker hiccup
// must never fail a caller's log call, this returns them: it is called from the
// tracer's own drain goroutine, which has nobody above it to disturb and which
// counts failures and reports the total. A trace stream quietly going nowhere is
// exactly the kind of thing that should be countable.
func (s *natsTraceSink) Write(event types.TraceEvent) error {
	raw, err := json.Marshal(traceRecord{
		TraceEvent:   event,
		DeploymentID: s.deploymentID,
		AppName:      s.appName,
		AppVersion:   s.appVersion,
	})
	if err != nil {
		return fmt.Errorf("k8s: encode trace record: %w", err)
	}
	if err := s.conn.Publish(TraceSubject, raw); err != nil {
		return fmt.Errorf("k8s: publish trace record: %w", err)
	}
	return nil
}

// Flush is a no-op. The client buffers and writes on its own schedule, and
// conn.Flush is a round trip to the server — paying for one every time the record
// queue happens to empty would add real latency to a fire-and-forget telemetry
// stream. Close does the one flush that matters.
func (s *natsTraceSink) Flush() error { return nil }

// Close waits for what has been published to reach the server.
//
// This is the flush worth having: a pod being rolled has seconds to hand over
// what it saw, and records still sitting in the client's write buffer when the
// connection goes are records nobody will ever see again. The connection itself
// belongs to the provider, which closes it after this.
func (s *natsTraceSink) Close() error {
	if err := s.conn.Flush(); err != nil {
		return fmt.Errorf("k8s: flush trace records: %w", err)
	}
	return nil
}

// newTracePublisher builds the module's trace publisher: the no-op when tracing
// is off, so a deployment that did not ask for traces publishes nothing at all.
//
//nolint:ireturn // returns the TracePublisher interface Services.Traces exposes
func newTracePublisher(
	conn *nats.Conn, opts core.TraceOptions, deploymentID, name, version string,
) core.TracePublisher {
	if !opts.Enabled {
		return core.NoopTracer()
	}
	return core.NewBufferedTracer(newTraceSink(conn, deploymentID, name, version), opts)
}
