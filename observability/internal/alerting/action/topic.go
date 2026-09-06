package action

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// scopedSubjectFormat is how the runtime scopes a deployment's own topics:
// octo.<deploymentID>.t.<subject>.
//
// This is a SECOND COPY of a format the runtime owns, in
// runtime/services/k8s/topics.go, where it is unexported. A contract test in this
// package reads that source and fails if the two ever disagree — the same device
// podstats/wire_contract_test.go uses to keep two copies of a wire type honest.
//
// The copy is here rather than the two sides sharing a constant because they are
// separate Go modules, and because the direction of the coupling matters: the
// runtime does not get to know that the platform publishes alerts at it.
//
// Publishing into a deployment's own subject, rather than to an unscoped
// `internal.` one, is deliberate and is the only delivery that works today. The
// runtime refuses Subscribe on a system: subject on purpose — that plane carries
// every deployment's logs and traces, so a flow that could subscribe to it could
// read other workloads' traffic. An alert therefore arrives on the subject the
// target deployment already listens to with an ordinary `events` source, and no
// runtime change is needed.
const scopedSubjectFormat = "octo.%s.t.%s"

// Topics publishes alerts onto the broker.
type Topics struct {
	conn *nats.Conn
}

// NewTopics returns a publisher over conn. A nil connection is a Topics that
// cannot publish, which is what a process with no NATS_URL has.
func NewTopics(conn *nats.Conn) *Topics {
	if conn == nil {
		return nil
	}
	return &Topics{conn: conn}
}

// TopicParams names where an alert goes.
type TopicParams struct {
	// DeploymentID is the deployment whose subject this publishes to — the app
	// that acts on the alert, not the app the alert is about. They are often
	// different: a platform agent watching an integration it does not run.
	DeploymentID string `json:"deploymentId"`
	Subject      string `json:"subject"`
}

type topicAction struct {
	params TopicParams
	topics *Topics
}

func newTopicAction(spec alerting.ActionSpec, topics *Topics) (Deliverer, error) {
	var p TopicParams
	if err := decodeParams(spec.Params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.DeploymentID) == "" {
		return nil, fmt.Errorf(
			"action: %w: a topic action needs the deployment whose subject it publishes to",
			alerting.ErrInvalidParams)
	}
	if err := validSubject(p.Subject); err != nil {
		return nil, err
	}
	return &topicAction{params: p, topics: topics}, nil
}

// validSubject refuses the subjects a publish cannot mean.
//
// Wildcards are the ones worth naming: a publish subject may not contain one,
// and a watch configured with `alerts.*` would go nowhere while looking exactly
// like a watch that was working.
func validSubject(subject string) error {
	subject = strings.TrimSpace(subject)
	switch {
	case subject == "":
		return fmt.Errorf("action: %w: a topic action needs a subject", alerting.ErrInvalidParams)
	case strings.ContainsAny(subject, " \t\r\n"):
		return fmt.Errorf("action: %w: a subject may not contain whitespace", alerting.ErrInvalidParams)
	case strings.Contains(subject, "*"), strings.Contains(subject, ">"):
		return fmt.Errorf(
			"action: %w: a subject published to may not contain a wildcard", alerting.ErrInvalidParams)
	case strings.HasPrefix(subject, "system:"):
		// The prefix exists so a flow can raise a platform event. Letting the
		// platform publish onto that plane from here would put alerts alongside
		// every deployment's logs and traces, which is not somewhere a
		// deployment can subscribe anyway.
		return fmt.Errorf(
			"action: %w: a topic action publishes to a deployment's own subject, not a system one",
			alerting.ErrInvalidParams)
	}
	return nil
}

// Deliver publishes the notification as JSON.
//
// Fire and forget, followed by a flush. A publish with no flush returns before
// the bytes have left the process, so a failure would surface as nothing at all —
// and this is the one moment the caller can still record that the alert did not
// get out.
func (a *topicAction) Deliver(ctx context.Context, n alerting.Notification) error {
	payload, err := json.Marshal(n)
	if err != nil {
		return fmt.Errorf("action: encode an alert notification: %w", err)
	}
	subject := fmt.Sprintf(scopedSubjectFormat, a.params.DeploymentID, a.params.Subject)
	if err := a.topics.conn.Publish(subject, payload); err != nil {
		return fmt.Errorf("action: publish to %s: %w", subject, err)
	}
	if err := a.topics.conn.FlushWithContext(ctx); err != nil {
		return fmt.Errorf("action: flush a publish to %s: %w", subject, err)
	}
	return nil
}
