package k8s

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
	"github.com/nats-io/nats.go"
)

// natsTopics is the NATS-backed Topics implementation for the k8s module. Subjects
// are prefixed with the deployment id (under a "t" segment, distinct from the queues'
// "q") so deployments sharing one broker never collide. Unlike natsQueues it uses a
// plain Subscribe, not a queue group, so every subscriber — across every replica —
// receives every message (broadcast fan-out). There is no reply.
type natsTopics struct {
	conn         *nats.Conn
	deploymentID string
}

func newNATSTopics(conn *nats.Conn, deploymentID string) *natsTopics {
	return &natsTopics{conn: conn, deploymentID: deploymentID}
}

// subject scopes a user subject to this deployment, e.g. octo.<id>.t.<subject>.
func (t *natsTopics) subject(subject string) string {
	return fmt.Sprintf("octo.%s.t.%s", t.deploymentID, subject)
}

// Publish broadcasts msg to every subscriber on subject with no reply.
func (t *natsTopics) Publish(_ context.Context, subject string, msg types.Message) error {
	return publishMsg(t.conn, "topics", subject, t.subject(subject), msg)
}

// Subscribe delivers every message on subject to handler on cfg.Listeners
// concurrent workers. It uses a plain subscription (no queue group), so this
// subscriber receives every message independently of any other.
//
//nolint:ireturn // satisfies core.Topics
func (t *natsTopics) Subscribe(
	ctx context.Context, subject string, handler core.TopicHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	cfg := core.NewSubscribeConfig(opts...)
	scoped := t.subject(subject)
	// A plain subscription (no queue group) makes every subscriber, across every
	// replica, receive every message (broadcast fan-out).
	return natsSubscribe(ctx, "topics", subject, cfg.Listeners,
		func(cb nats.MsgHandler) (*nats.Subscription, error) {
			return t.conn.Subscribe(scoped, cb)
		},
		func(ctx context.Context, m *nats.Msg) { t.dispatch(ctx, m, handler) },
	)
}

// dispatch decodes a delivered message and runs the handler; a decode or handler
// error is logged and dropped, since a topic has no requester to surface it to.
func (t *natsTopics) dispatch(ctx context.Context, m *nats.Msg, handler core.TopicHandler) {
	in, err := decodeMsg(m)
	if err != nil {
		slog.Error("topics: decode delivered message", "subject", m.Subject, "err", err)
		return
	}
	if err := handler(ctx, in); err != nil {
		slog.Error("topics: handler", "subject", m.Subject, "err", err)
	}
}
