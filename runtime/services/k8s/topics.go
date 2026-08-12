package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
	"github.com/nats-io/nats.go"
)

// natsTopics is the NATS-backed Topics implementation for the k8s module. Subjects
// are prefixed with the deployment id (under a "t" segment, distinct from the queues'
// "q") so deployments sharing one broker never collide — unless they opt out with
// the system: prefix, see below. Unlike natsQueues it uses a plain Subscribe, not a
// queue group, so every subscriber — across every replica — receives every message
// (broadcast fan-out). There is no reply.
type natsTopics struct {
	conn         *nats.Conn
	deploymentID string
}

func newNATSTopics(conn *nats.Conn, deploymentID string) *natsTopics {
	return &natsTopics{conn: conn, deploymentID: deploymentID}
}

// systemPrefix opts a subject out of deployment scoping.
//
// Everything else becomes octo.<id>.t.<subject> so deployments sharing a broker
// cannot collide, which is right for a flow talking to itself and wrong for one
// talking to the platform: a subject nobody else can name is a subject nobody else
// receives. A subject written "system:x.y" is published to "x.y" verbatim,
// alongside the platform's other unscoped subjects (see LogSubject and
// TraceSubject).
//
// The runtime deliberately holds no list of what those subjects are, and validates
// nothing about them. The caller names the subject and owns the payload shape; an
// unroutable name publishes into the void and a malformed payload is the
// subscriber's problem, exactly as they would be on any other subject. A table here
// would be this module keeping an inventory of the platform's internals, which is
// the coupling the convention exists to avoid.
const systemPrefix = "system:"

// subject resolves a user subject to the one that goes on the wire: scoped to this
// deployment, or verbatim when it opts out.
func (t *natsTopics) subject(subject string) string {
	if unscoped, ok := strings.CutPrefix(subject, systemPrefix); ok {
		return unscoped
	}
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
// A system: subject is refused. The prefix exists so a flow can *raise* a platform
// event; letting it subscribe as well would be a different capability entirely,
// because the unscoped plane carries every deployment's log and trace records. A
// flow could name system:internal.logs, or a wildcard, and read traffic belonging
// to workloads it has nothing to do with. Publishing has no equivalent reach — a
// publish subject cannot contain a wildcard, and the worst a wrong name does is go
// nowhere.
//
//nolint:ireturn // satisfies core.Topics
func (t *natsTopics) Subscribe(
	ctx context.Context, subject string, handler core.TopicHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	if strings.HasPrefix(subject, systemPrefix) {
		return nil, fmt.Errorf(
			"topics: subscribe %q: a %s subject may be published to, not subscribed to", subject, systemPrefix)
	}
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
