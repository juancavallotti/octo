package action

import (
	"context"
	"log/slog"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// logAction writes the alert to this service's own log, where the platform's log
// pipeline then stores it.
//
// It exists because it costs nothing and it makes the feature demonstrable before
// anybody has configured a mailer or written a flow to receive a topic. It is
// also the honest default for a watch somebody is still tuning: the evaluation
// history already records what happened, and this puts it where an operator
// tailing the service will see it.
type logAction struct{}

func newLogAction(_ alerting.ActionSpec) (Deliverer, error) { return &logAction{}, nil }

// Deliver never fails. There is nowhere for it to fail to.
func (a *logAction) Deliver(_ context.Context, n alerting.Notification) error {
	level := slog.LevelWarn
	if n.Resolved() {
		level = slog.LevelInfo
	}
	slog.Log(context.Background(), level, n.Headline(),
		"watch", n.WatchName, "watch_id", n.WatchID, "severity", n.Severity,
		"incident", n.IncidentID, "matched", n.Matched, "total", n.Total,
		"combinator", n.Combinator, "degraded", n.Degraded)
	return nil
}
