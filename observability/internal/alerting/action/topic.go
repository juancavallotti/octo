package action

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// scopedSubjectFormat scopes a deployment's own topics: octo.<deploymentID>.t.
// <subject>. The publisher of these subjects is in a separate Go module, so this
// is a copy; a contract test in this package fails if the two disagree.
//
// An alert goes to a deployment's own scoped subject rather than an unscoped
// `internal.` one, because that plane carries every deployment's logs and traces
// and a flow able to subscribe to it could read other workloads' traffic. The
// scoped subject is one a target deployment already listens to.
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
	// different.
	DeploymentID string `json:"deploymentId"`
	Subject      string `json:"subject"`

	// ReportTo is who the receiving app should send its findings to, carried on
	// the alert rather than configured inside that app.
	//
	// It rides on the alert so the addresses stay visible to whoever edits the
	// watch, and so the receiving app is not the one deciding who hears about an
	// incident.
	//
	// Optional and empty by default: an action feeding a flow that only records or
	// reacts needs nobody's address.
	ReportTo []string `json:"reportTo,omitempty"`
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
	// Trimmed into the params, not just for the check. Validating a trimmed copy
	// and publishing the original meant " alerts " passed — nothing in the
	// trimmed form contains whitespace — and then went to `octo.<id>.t. alerts `,
	// a subject no events source is subscribed to. The action reported success
	// and reached nobody.
	p.DeploymentID = strings.TrimSpace(p.DeploymentID)
	p.Subject = strings.TrimSpace(p.Subject)
	for i, address := range p.ReportTo {
		p.ReportTo[i] = strings.TrimSpace(address)
	}
	if p.DeploymentID == "" {
		return nil, fmt.Errorf(
			"action: %w: a topic action needs the deployment whose subject it publishes to",
			alerting.ErrInvalidParams)
	}
	if err := validSubject(p.Subject); err != nil {
		return nil, err
	}
	// Validated here even though this action sends no mail, on the same terms the
	// email action validates its own: an address that is not an address should be
	// refused while somebody is looking at the form, not discovered by whatever
	// picks the alert up an hour later — at which point the mistake is a silent
	// non-delivery in an app nobody is watching.
	if len(p.ReportTo) > maxRecipients {
		return nil, fmt.Errorf("action: %w: %d recipients exceeds the limit of %d",
			alerting.ErrInvalidParams, len(p.ReportTo), maxRecipients)
	}
	for _, address := range p.ReportTo {
		if _, err := mail.ParseAddress(address); err != nil {
			return nil, fmt.Errorf("action: %w: %q is not an address", alerting.ErrInvalidParams, address)
		}
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

// topicPayload is what goes on the wire: the notification every action shares,
// plus the parts that belong to this delivery alone.
//
// Embedded rather than added to Notification, because a recipient list is not a
// fact about the watch firing — it is a fact about where this one action sends
// it, and an email action carrying a `reportTo` would be nonsense.
type topicPayload struct {
	alerting.Notification
	ReportTo []string `json:"reportTo,omitempty"`
}

// Deliver publishes the notification as JSON.
//
// Fire and forget, followed by a flush. A publish with no flush returns before
// the bytes have left the process, so a failure would surface as nothing at all —
// and this is the one moment the caller can still record that the alert did not
// get out.
func (a *topicAction) Deliver(ctx context.Context, n alerting.Notification) error {
	payload, err := json.Marshal(topicPayload{Notification: n, ReportTo: a.params.ReportTo})
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
