package api

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// defaultConsumerGroup is the group a runtime joins when it has no deployment id
// to name itself by.
const defaultConsumerGroup = "octo"

// nackDelay is how long the platform is asked to hold a rejected message before
// handing it back. Without one, a message whose handler fails every time is
// redelivered as fast as the network allows — a poison message becomes a hot loop
// against the platform, and against whatever the handler was failing to reach.
const nackDelay = 5 * time.Second

// queues is the point-to-point plane, delegated to the platform API.
//
// Delivery here is at-least-once rather than the at-most-once the other two
// modules give: a message is acknowledged after its handler returns, and one
// whose handler failed is negatively acknowledged and redelivered. That is a
// strengthening — a handler written against at-most-once still works — but it is
// a real difference, and a handler that is not idempotent may now see the same
// message twice.
//
// There is no push. Every subscription is a long poll, which is a good fit for
// internal work distribution across replicas and a poor one for being triggered
// from outside: an external event source should call an HTTP flow instead.
type queues struct {
	c *client
	// module ends when Services.Close is called, so a subscription started from a
	// long-lived caller context still stops with the module.
	module        context.Context //nolint:containedctx // the module's lifetime, not a request's
	latch         *latch
	consumerGroup string
	poll          pollConfig
	requestReply  bool
}

func newQueues(module context.Context, c *client, consumerGroup string, f queueFeature) *queues {
	if consumerGroup == "" {
		consumerGroup = defaultConsumerGroup
	}
	return &queues{
		c:             c,
		module:        module,
		latch:         &latch{feature: FeatureQueues},
		consumerGroup: consumerGroup,
		poll:          resolvePoll("queues", f.PollTimeoutSeconds, f.MaxBatch, c.longTimeout),
		requestReply:  f.RequestReply,
	}
}

// Wire types.
type (
	publishRequest struct {
		Message messageWire `json:"message"`
	}

	requestRequest struct {
		Message        messageWire `json:"message"`
		TimeoutSeconds int64       `json:"timeoutSeconds"`
	}

	requestResponse struct {
		Message messageWire `json:"message"`
	}

	// receiveRequest is the long poll. waitSeconds is how long the server should
	// hold the request open before answering 204.
	receiveRequest struct {
		ConsumerGroup  string `json:"consumerGroup,omitempty"`
		SubscriptionID string `json:"subscriptionId,omitempty"`
		MaxMessages    int    `json:"maxMessages"`
		WaitSeconds    int64  `json:"waitSeconds"`
	}

	receiveResponse struct {
		Messages []delivery `json:"messages"`
	}

	// delivery is one message and the handle needed to settle it.
	delivery struct {
		DeliveryID string      `json:"deliveryId"`
		ReplyTo    string      `json:"replyTo,omitempty"`
		Message    messageWire `json:"message"`
	}

	settleRequest struct {
		SubscriptionID string   `json:"subscriptionId,omitempty"`
		DeliveryIDs    []string `json:"deliveryIds"`
		DelaySeconds   int64    `json:"delaySeconds,omitempty"`
	}

	replyRequest struct {
		ReplyTo string      `json:"replyTo"`
		Message messageWire `json:"message"`
	}
)

// Publish sends msg to one competing consumer with no reply.
func (q *queues) Publish(ctx context.Context, subject string, msg types.Message) error {
	if !q.latch.live() {
		return unsupportedError(FeatureQueues)
	}
	wire, err := encodeMessage(msg)
	if err != nil {
		return err
	}
	err = q.c.json(ctx, routeQueuePublish, q.c.url(routeQueuePublish, subject),
		publishRequest{Message: wire}, nil, q.c.timeout)
	return q.settleFeatureError(err)
}

// Request sends msg and waits for one reply.
func (q *queues) Request(
	ctx context.Context, subject string, msg types.Message, opts ...core.RequestOption,
) (types.Message, error) {
	if !q.latch.live() {
		return types.Message{}, unsupportedError(FeatureQueues)
	}
	if !q.requestReply {
		return types.Message{}, fmt.Errorf("api: the platform API implements queues but not "+
			"request-reply; declare \"requestReply\": true once %s answers a reply",
			q.c.url(routeQueueRequest, subject))
	}
	wire, err := encodeMessage(msg)
	if err != nil {
		return types.Message{}, err
	}
	timeout := core.NewRequestConfig(opts...).Timeout

	var out requestResponse
	// The client waits a little longer than the server is asked to, so a timeout
	// is reported by the platform (which knows whether anybody was listening)
	// rather than by a deadline on this side.
	err = q.c.json(ctx, routeQueueRequest, q.c.url(routeQueueRequest, subject),
		requestRequest{Message: wire, TimeoutSeconds: seconds(timeout)},
		&out, timeout+q.c.timeout)
	if err != nil {
		return types.Message{}, q.settleFeatureError(err)
	}
	return decodeMessage(out.Message)
}

// Subscribe joins the competing-consumer group on subject.
//
//nolint:ireturn // satisfies core.Queues
func (q *queues) Subscribe(
	ctx context.Context, subject string, handler core.QueueHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	if !q.latch.live() {
		return nil, unsupportedError(FeatureQueues)
	}
	cfg := core.NewSubscribeConfig(opts...)
	ctx, unbind := bindTo(ctx, q.module)
	loop := run(ctx, cfg.Listeners, q.poll.timeout,
		func(ctx context.Context) ([]delivery, error) { return q.receive(ctx, subject) },
		func(ctx context.Context, d delivery) { q.dispatch(ctx, subject, d, handler) },
	)
	slog.Debug("api: subscribed to a queue",
		"subject", subject, "group", q.consumerGroup, "listeners", cfg.Listeners)
	return &pollSubscription{loop: loop, unbind: unbind}, nil
}

// receive makes one long poll. A 204 is not an empty answer to be retried
// urgently — it is the poll expiring, which is the normal case on an idle
// subject, so it returns no messages and no error and the loop asks again.
func (q *queues) receive(ctx context.Context, subject string) ([]delivery, error) {
	in := receiveRequest{
		ConsumerGroup: q.consumerGroup,
		MaxMessages:   q.poll.maxBatch,
		WaitSeconds:   seconds(q.poll.timeout),
	}
	var out receiveResponse
	err := q.c.json(ctx, routeQueueReceive, q.c.url(routeQueueReceive, subject),
		in, &out, q.poll.timeout+pollTimeoutHeadroom)
	if err != nil {
		if isNotImplemented(err) {
			q.latch.mark()
			return nil, nil
		}
		return nil, err
	}
	return out.Messages, nil
}

// dispatch runs one delivery's handler and settles it.
//
// The reply goes out before the ack, so a request whose reply could not be
// delivered is redelivered rather than acknowledged — the requester is waiting,
// and losing the reply silently is worse than handling the message twice.
func (q *queues) dispatch(ctx context.Context, subject string, d delivery, handler core.QueueHandler) {
	in, err := decodeMessage(d.Message)
	if err != nil {
		// A message that cannot be decoded will not decode on redelivery either,
		// so acknowledge it rather than looping on it forever.
		slog.Error("api queues: decode a delivered message", "subject", subject, "error", err)
		q.settle(ctx, routeQueueAck, subject, d.DeliveryID, 0)
		return
	}
	reply, err := handler(ctx, in)
	if err != nil {
		slog.Error("api queues: handler", "subject", subject, "error", err)
		q.settle(ctx, routeQueueNack, subject, d.DeliveryID, nackDelay)
		return
	}
	if d.ReplyTo != "" && !q.reply(ctx, subject, d.ReplyTo, reply) {
		q.settle(ctx, routeQueueNack, subject, d.DeliveryID, nackDelay)
		return
	}
	q.settle(ctx, routeQueueAck, subject, d.DeliveryID, 0)
}

// reply delivers a handler's answer to the requester, reporting whether it landed.
func (q *queues) reply(ctx context.Context, subject, replyTo string, msg types.Message) bool {
	wire, err := encodeMessage(msg)
	if err != nil {
		slog.Error("api queues: encode a reply", "subject", subject, "error", err)
		return false
	}
	err = q.c.json(ctx, routeQueueReply, q.c.url(routeQueueReply),
		replyRequest{ReplyTo: replyTo, Message: wire}, nil, q.c.timeout)
	if err != nil {
		slog.Error("api queues: deliver a reply", "subject", subject, "error", err)
		return false
	}
	return true
}

// settle acknowledges or rejects a delivery. A failure here is logged rather than
// returned: the handler has already run, and the platform redelivers what it was
// not told about.
func (q *queues) settle(ctx context.Context, r route, subject, deliveryID string, delay time.Duration) {
	// Settling runs on a context detached from the loop's, so a subscription
	// closing mid-handler still tells the platform what happened.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), q.c.timeout)
	defer cancel()
	err := q.c.json(ctx, r, q.c.url(r, subject),
		settleRequest{DeliveryIDs: []string{deliveryID}, DelaySeconds: seconds(delay)},
		nil, q.c.timeout)
	if err != nil {
		slog.Warn("api queues: could not settle a delivery; the platform will redeliver it",
			"subject", subject, "delivery", deliveryID, "route", r.path, "error", err)
	}
}

// settleFeatureError latches the feature off on a 501 and hands back the error
// the caller should see.
func (q *queues) settleFeatureError(err error) error {
	if isNotImplemented(err) {
		q.latch.mark()
		return unsupportedError(FeatureQueues)
	}
	return err
}

// pollSubscription is a handle to a running poll loop. Close stops delivery and
// drains the workers, so afterwards no handler is still running.
type pollSubscription struct {
	loop   *pollLoop
	unbind context.CancelFunc
}

func (s *pollSubscription) Close() error {
	s.loop.close()
	s.unbind()
	return nil
}
