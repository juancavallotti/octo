package core

import (
	"context"
	"errors"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// DefaultListeners is the number of concurrent handler goroutines a subscription
// runs when WithListeners is not set. It mirrors the per-flow worker default
// (the pool package's defaultWorkers), so a queue consumer gets the same fair
// amount of parallelism a flow does.
const DefaultListeners = 8

// DefaultRequestTimeout bounds a Request whose context carries no deadline, so a
// request to a subject with no responder fails instead of blocking forever.
const DefaultRequestTimeout = 30 * time.Second

// errNoQueues is returned by the no-op queues' send operations: with no backend
// configured there is nowhere to deliver, so failing loudly beats silently
// dropping the message.
var errNoQueues = errors.New("queues: no queue backend configured")

// Queues is a deployment-scoped message queue available to connectors and blocks.
// It offers two send shapes over one competing-consumer model: Publish is
// point-to-point (exactly one consumer handles each message, no reply), and
// Request is request-reply (the caller waits for one reply). The consumer side is
// the same Subscribe for both — whether a reply is sent is decided by whether the
// inbound message carries a reply destination.
//
// In the standalone module queues are in-process (buffered channels); in the k8s
// module they are backed by NATS (queue-group subscriptions and native request-
// reply). In those two, delivery is at-most-once: a message published with no live
// consumer is dropped. The api module is at-least-once instead, because the
// platform behind it acknowledges each delivery and redelivers what was not
// acknowledged — so a handler that runs there must be idempotent, or deduplicate
// on EventID, before it applies a side effect. A handler written for at-most-once
// is not automatically safe under it.
type Queues interface {
	// Publish sends msg to subject for exactly one competing consumer. It does not
	// wait for, or expect, a reply.
	Publish(ctx context.Context, subject string, msg types.Message) error
	// Request sends msg to subject and waits for one reply, bounded by ctx and the
	// configured timeout (WithTimeout, else DefaultRequestTimeout).
	Request(ctx context.Context, subject string, msg types.Message, opts ...RequestOption) (types.Message, error)
	// Subscribe joins the competing-consumer group on subject. The handler runs on
	// listeners concurrent goroutines (WithListeners, else DefaultListeners). When
	// the inbound message carries a reply destination, the reply the handler
	// returns is delivered to the requester; otherwise it is dropped. The returned
	// Subscription stops consuming when closed.
	//nolint:ireturn // returns the Subscription interface the caller stores
	Subscribe(ctx context.Context, subject string, handler QueueHandler, opts ...SubscribeOption) (Subscription, error)
}

// QueueHandler processes one inbound message and may return a reply. The reply is
// sent only when the sender requested one (a Request, not a Publish); for a
// fire-and-forget message it is ignored. A non-nil error is logged by the module
// and, for a Request, surfaces to the requester as a failed request.
type QueueHandler func(ctx context.Context, msg types.Message) (reply types.Message, err error)

// Subscription is a handle to an active subscription. Close stops the handler
// goroutines and unsubscribes from the backend (best-effort).
type Subscription interface {
	Close() error
}

// SubscribeOption configures a Subscribe call.
type SubscribeOption func(*SubscribeConfig)

// SubscribeConfig is the resolved configuration for a subscription. Modules build
// it from the caller's options with NewSubscribeConfig.
type SubscribeConfig struct {
	// Listeners is the number of concurrent handler goroutines.
	Listeners int
}

// WithListeners sets the number of concurrent handler goroutines for a
// subscription. A value <= 0 is ignored (the default applies).
func WithListeners(n int) SubscribeOption {
	return func(c *SubscribeConfig) { c.Listeners = n }
}

// NewSubscribeConfig resolves opts into a SubscribeConfig, applying DefaultListeners
// when no positive value was set. Modules call it to read the effective settings.
func NewSubscribeConfig(opts ...SubscribeOption) SubscribeConfig {
	cfg := SubscribeConfig{Listeners: DefaultListeners}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Listeners <= 0 {
		cfg.Listeners = DefaultListeners
	}
	return cfg
}

// RequestOption configures a Request call.
type RequestOption func(*RequestConfig)

// RequestConfig is the resolved configuration for a request. Modules build it from
// the caller's options with NewRequestConfig.
type RequestConfig struct {
	// Timeout bounds the wait for a reply when the request context has no deadline.
	Timeout time.Duration
}

// WithTimeout sets how long Request waits for a reply when its context carries no
// deadline. A value <= 0 is ignored (the default applies).
func WithTimeout(d time.Duration) RequestOption {
	return func(c *RequestConfig) { c.Timeout = d }
}

// NewRequestConfig resolves opts into a RequestConfig, applying DefaultRequestTimeout
// when no positive value was set. Modules call it to read the effective settings.
func NewRequestConfig(opts ...RequestOption) RequestConfig {
	cfg := RequestConfig{Timeout: DefaultRequestTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultRequestTimeout
	}
	return cfg
}

// NoopQueues returns a queues backend with no transport: Publish and Request fail
// loudly (errNoQueues) and Subscribe is an inert no-op. It is the fallback the
// no-op services expose for contexts that were not wired with real services.
//
//nolint:ireturn // returns the Queues interface intentionally
func NoopQueues() Queues { return noopQueues{} }

// noopQueues has no transport: sends fail and subscriptions deliver nothing.
type noopQueues struct{}

func (noopQueues) Publish(context.Context, string, types.Message) error { return errNoQueues }

func (noopQueues) Request(
	context.Context, string, types.Message, ...RequestOption,
) (types.Message, error) {
	return types.Message{}, errNoQueues
}

//nolint:ireturn // satisfies the Queues interface
func (noopQueues) Subscribe(
	context.Context, string, QueueHandler, ...SubscribeOption,
) (Subscription, error) {
	return noopSubscription{}, nil
}

// noopSubscription is an inert subscription handle.
type noopSubscription struct{}

func (noopSubscription) Close() error { return nil }
