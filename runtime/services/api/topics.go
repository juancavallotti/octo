package api

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// systemPrefix opts a subject out of deployment scoping, as it does in the k8s
// module.
//
// A subject written "system:x.y" is published as "x.y" with a flag saying so, so
// an implementer can route it to their platform plane without sniffing the
// string. The runtime deliberately holds no list of what those subjects are and
// validates nothing about them: the caller names the subject and owns the payload
// shape, and a table here would be this module keeping an inventory of somebody
// else's internals.
const systemPrefix = "system:"

// topics is the broadcast plane, delegated to the platform API.
//
// The difference from queues is the explicit subscription resource. Fan-out over
// a pull API needs a per-subscriber cursor — there is no way to say "each of you
// receives every message" without naming each of you — so a subscriber registers,
// polls against its own subscription, and unregisters on close. On GCP that maps
// straight onto a Pub/Sub subscription.
//
// There is no nack: core.TopicHandler returns an error that is logged and
// dropped, because a topic has no requester to surface one to, so redelivering
// would contradict the interface.
type topics struct {
	c *client
	// module ends when Services.Close is called; see queues.
	module     context.Context //nolint:containedctx // the module's lifetime, not a request's
	latch      *latch
	subscriber string
	poll       pollConfig
}

func newTopics(module context.Context, c *client, subscriber string, f topicFeature) *topics {
	return &topics{
		c:          c,
		module:     module,
		latch:      &latch{feature: FeatureTopics},
		subscriber: subscriber,
		poll:       resolvePoll("topics", f.PollTimeoutSeconds, f.MaxBatch, c.longTimeout),
	}
}

// Wire types.
type (
	topicPublishRequest struct {
		Message messageWire `json:"message"`
		// System marks a subject that opted out of deployment scoping, so an
		// implementer routes it deliberately rather than by parsing the name.
		System bool `json:"system,omitempty"`
	}

	subscribeRequest struct {
		Subscriber string `json:"subscriber"`
	}

	subscribeResponse struct {
		SubscriptionID string `json:"subscriptionId"`
	}
)

// Publish broadcasts msg to every subscriber on subject.
func (t *topics) Publish(ctx context.Context, subject string, msg types.Message) error {
	if !t.latch.live() {
		return unsupportedError(FeatureTopics)
	}
	wire, err := encodeMessage(msg)
	if err != nil {
		return err
	}
	name, system := strings.CutPrefix(subject, systemPrefix)
	err = t.c.json(ctx, routeTopicPublish, t.c.url(routeTopicPublish, name),
		topicPublishRequest{Message: wire, System: system}, nil, t.c.timeout)
	if isNotImplemented(err) {
		t.latch.mark()
		return unsupportedError(FeatureTopics)
	}
	return err
}

// Subscribe delivers every message on subject to handler.
//
// A system: subject is refused, for the reason the k8s module gives: the prefix
// exists so a flow can raise a platform event, and letting it subscribe would be
// a different capability entirely — the unscoped plane carries every workload's
// traffic, and a flow could name a subject belonging to something it has nothing
// to do with. Publishing has no equivalent reach.
//
//nolint:ireturn // satisfies core.Topics
func (t *topics) Subscribe(
	ctx context.Context, subject string, handler core.TopicHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	if strings.HasPrefix(subject, systemPrefix) {
		return nil, fmt.Errorf(
			"api topics: subscribe %q: a %s subject may be published to, not subscribed to",
			subject, systemPrefix)
	}
	if !t.latch.live() {
		return nil, unsupportedError(FeatureTopics)
	}

	var out subscribeResponse
	err := t.c.json(ctx, routeTopicSubscribe, t.c.url(routeTopicSubscribe, subject),
		subscribeRequest{Subscriber: t.subscriber}, &out, t.c.timeout)
	if err != nil {
		if isNotImplemented(err) {
			t.latch.mark()
			return nil, unsupportedError(FeatureTopics)
		}
		return nil, err
	}
	if out.SubscriptionID == "" {
		return nil, fmt.Errorf("api topics: subscribe %q: the platform API returned no "+
			"subscriptionId, so there is no cursor to poll against", subject)
	}

	cfg := core.NewSubscribeConfig(opts...)
	ctx, unbind := bindTo(ctx, t.module)
	loop := run(ctx, cfg.Listeners, t.poll.timeout,
		func(ctx context.Context) ([]delivery, error) {
			return t.receive(ctx, subject, out.SubscriptionID)
		},
		func(ctx context.Context, d delivery) {
			t.dispatch(ctx, subject, out.SubscriptionID, d, handler)
		},
	)
	slog.Debug("api: subscribed to a topic",
		"subject", subject, "subscription", out.SubscriptionID, "listeners", cfg.Listeners)
	return &topicSubscription{
		topics: t, subject: subject, id: out.SubscriptionID, loop: loop, unbind: unbind,
	}, nil
}

// receive makes one long poll against this subscriber's cursor.
func (t *topics) receive(ctx context.Context, subject, subscriptionID string) ([]delivery, error) {
	in := receiveRequest{
		SubscriptionID: subscriptionID,
		MaxMessages:    t.poll.maxBatch,
		WaitSeconds:    seconds(t.poll.timeout),
	}
	var out receiveResponse
	err := t.c.json(ctx, routeTopicReceive, t.c.url(routeTopicReceive, subject),
		in, &out, t.poll.timeout+pollTimeoutHeadroom)
	if err != nil {
		if isNotImplemented(err) {
			t.latch.mark()
			return nil, nil
		}
		return nil, err
	}
	return out.Messages, nil
}

// dispatch runs one delivery's handler and acknowledges it.
//
// It acknowledges whatever the handler did, including a failure, because there is
// no nack: core.TopicHandler's error is logged and dropped, and holding the
// message back would mean a redelivery the interface says will not happen.
func (t *topics) dispatch(
	ctx context.Context, subject, subscriptionID string, d delivery, handler core.TopicHandler,
) {
	in, err := decodeMessage(d.Message)
	if err != nil {
		slog.Error("api topics: decode a delivered message", "subject", subject, "error", err)
	} else if err := handler(ctx, in); err != nil {
		slog.Error("api topics: handler", "subject", subject, "error", err)
	}
	t.ack(ctx, subject, subscriptionID, d.DeliveryID)
}

// ack advances this subscriber's cursor past a delivery.
func (t *topics) ack(ctx context.Context, subject, subscriptionID, deliveryID string) {
	// Detached from the loop's context, so a subscription closing mid-handler
	// still advances the cursor rather than replaying the message on restart.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), t.c.timeout)
	defer cancel()
	err := t.c.json(ctx, routeTopicAck, t.c.url(routeTopicAck, subject),
		settleRequest{SubscriptionID: subscriptionID, DeliveryIDs: []string{deliveryID}},
		nil, t.c.timeout)
	if err != nil {
		slog.Warn("api topics: could not acknowledge a delivery; it may be delivered again",
			"subject", subject, "delivery", deliveryID, "error", err)
	}
}

// topicSubscription stops the loop and gives the subscription back.
type topicSubscription struct {
	topics  *topics
	subject string
	id      string
	loop    *pollLoop
	unbind  context.CancelFunc
}

// Close stops delivery, drains the workers, and unregisters the subscription.
//
// Unregistering matters more here than releasing a queue consumer does: a topic
// subscription is a durable cursor on the platform's side, and one left behind by
// every restart accumulates messages nobody will ever read.
func (s *topicSubscription) Close() error {
	s.loop.close()
	s.unbind()
	ctx, cancel := context.WithTimeout(context.Background(), s.topics.c.timeout)
	defer cancel()
	err := s.topics.c.json(ctx, routeTopicUnsubscribe,
		s.topics.c.url(routeTopicUnsubscribe, s.subject, s.id), nil, nil, s.topics.c.timeout)
	if err != nil {
		slog.Warn("api topics: could not remove a subscription; it may keep accumulating messages",
			"subject", s.subject, "subscription", s.id, "error", err)
	}
	return nil
}
