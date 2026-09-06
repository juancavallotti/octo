// Package action delivers what a watch decided.
//
// Three kinds, and every attempt is recorded with its outcome, because "the
// alert fired but I got no email" has to be answerable from the history rather
// than from somebody's inbox.
//
// A delivery failure never rolls back a state transition. The watch did fire,
// and losing that fact because a mailer was down is strictly the worse failure —
// the incident is open, the history says so, and the next renotify tries again.
package action

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// Deliverer sends one notification through one configured action.
//
// An interface because there are three implementations with three failure modes
// and three sets of parameters, and the alternative is a Dispatch function with a
// switch at the top and every kind's plumbing interleaved in one body.
type Deliverer interface {
	Deliver(ctx context.Context, n alerting.Notification) error
}

// Dispatcher builds the deliverers a watch's actions name and runs them.
type Dispatcher struct {
	topics *Topics
	mail   *Mailer
}

// NewDispatcher wires the two deliverers that need somewhere to send to. Either
// may be nil — a service with no NATS connection cannot publish and a service
// with no orchestrator cannot mail — and a watch naming an action this process
// cannot perform records that, rather than failing silently or refusing to run.
func NewDispatcher(topics *Topics, mail *Mailer) *Dispatcher {
	return &Dispatcher{topics: topics, mail: mail}
}

// Notify runs every action on the watch and reports what each did.
//
// It never returns an error of its own: one action failing must not stop the
// next, because a watch that emails and publishes has two audiences and the
// second has done nothing wrong. Every failure lands in a result instead.
//
// The name is the runner's, not this package's: the runner declares the one-method
// interface it consumes and this happens to satisfy it.
func (d *Dispatcher) Notify(
	ctx context.Context, w alerting.Watch, n alerting.Notification,
) []alerting.DeliveryResult {
	out := make([]alerting.DeliveryResult, 0, len(w.Actions))
	for _, spec := range w.Actions {
		result := alerting.DeliveryResult{ActionID: spec.ID, Type: spec.Type}
		if err := d.deliver(ctx, spec, n); err != nil {
			result.Err = err.Error()
			slog.Error("an alert action failed",
				"watch", w.Name, "action", spec.ID, "type", spec.Type, "error", err)
		}
		out = append(out, result)
	}
	return out
}

func (d *Dispatcher) deliver(ctx context.Context, spec alerting.ActionSpec, n alerting.Notification) error {
	deliverer, err := d.build(spec)
	if err != nil {
		return err
	}
	return deliverer.Deliver(ctx, n)
}

// build resolves one action spec to the thing that performs it. It is also the
// validator: Validate below runs exactly this, so a watch naming an action that
// cannot be built is refused at save time.
func (d *Dispatcher) build(spec alerting.ActionSpec) (Deliverer, error) {
	switch spec.Type {
	case alerting.ActionTypeTopic:
		if d.topics == nil {
			return nil, fmt.Errorf("action: cannot publish: this process has no broker connection")
		}
		return newTopicAction(spec, d.topics)
	case alerting.ActionTypeEmail:
		if d.mail == nil {
			return nil, fmt.Errorf("action: cannot send mail: this process has no orchestrator address")
		}
		return newEmailAction(spec, d.mail)
	case alerting.ActionTypeLog:
		return newLogAction(spec)
	default:
		return nil, fmt.Errorf("action: %w: %q on action %q", alerting.ErrUnknownAction, spec.Type, spec.ID)
	}
}

// Validate checks a watch's actions without sending anything.
//
// Run at save time, and deliberately run through the same build the dispatcher
// uses: a validator that reimplemented the checks would drift from the thing it
// was validating, and the drift would show up as an action that saved cleanly and
// never delivered.
func Validate(w alerting.Watch) error {
	if len(w.Actions) > alerting.MaxActions {
		return fmt.Errorf("alerting: %w: %d actions exceeds the limit of %d",
			alerting.ErrInvalidWatch, len(w.Actions), alerting.MaxActions)
	}
	// Built against a dispatcher that has both dependencies, so validation
	// answers "is this action well-formed" rather than "can this particular
	// process perform it right now" — the second is a deployment fact and would
	// make a watch unsaveable on a machine that happens to have no broker.
	d := &Dispatcher{topics: &Topics{}, mail: &Mailer{}}
	seen := make(map[string]struct{}, len(w.Actions))
	for _, spec := range w.Actions {
		if spec.ID == "" {
			return fmt.Errorf("alerting: %w: an action needs an id", alerting.ErrInvalidParams)
		}
		if _, dup := seen[spec.ID]; dup {
			return fmt.Errorf("alerting: %w: two actions share the id %q", alerting.ErrInvalidWatch, spec.ID)
		}
		seen[spec.ID] = struct{}{}
		if _, err := d.build(spec); err != nil {
			return err
		}
	}
	return nil
}

// decodeParams unmarshals an action's parameters strictly, on the same terms a
// condition's are: a misspelled recipient list that decoded permissively is an
// alert that reaches nobody.
func decodeParams(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("action: %w: %w", alerting.ErrInvalidParams, err)
	}
	return nil
}
