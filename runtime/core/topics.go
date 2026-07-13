package core

import (
	"context"
	"errors"

	"github.com/juancavallotti/octo/runtime/types"
)

// Topics is a deployment-scoped broadcast pub/sub available to connectors and
// blocks. Unlike Queues (a competing-consumer model where each message reaches
// exactly one consumer), a Topics message is fanned out to every subscriber on the
// subject — the platform-events / topic model. There is no reply.
//
// In the standalone module topics are in-process (fan-out to local subscribers);
// in the k8s module they are backed by NATS (a plain, non-queue subscription, so
// every replica's subscribers receive every message). Delivery is at-most-once: a
// message published with no live subscriber is dropped.
type Topics interface {
	// Publish broadcasts msg to every subscriber on subject. It does not wait for,
	// or expect, a reply.
	Publish(ctx context.Context, subject string, msg types.Message) error
	// Subscribe delivers every message on subject to handler, which runs on
	// listeners concurrent goroutines (WithListeners, else DefaultListeners). The
	// returned Subscription stops consuming when closed.
	//nolint:ireturn // returns the Subscription interface the caller stores
	Subscribe(ctx context.Context, subject string, handler TopicHandler, opts ...SubscribeOption) (Subscription, error)
}

// TopicHandler processes one broadcast message. A non-nil error is logged by the
// module and otherwise ignored — a topic has no requester to surface it to.
type TopicHandler func(ctx context.Context, msg types.Message) error

// NoopTopics returns a topics backend with no transport: Publish fails loudly
// (errNoTopics) and Subscribe is an inert no-op. It is the fallback the no-op
// services expose for contexts that were not wired with real services.
//
//nolint:ireturn // returns the Topics interface intentionally
func NoopTopics() Topics { return noopTopics{} }

// errNoTopics is returned by the no-op topics' Publish: with no backend configured
// there is nowhere to deliver, so failing loudly beats silently dropping.
var errNoTopics = errors.New("topics: no topic backend configured")

// noopTopics has no transport: Publish fails and subscriptions deliver nothing.
type noopTopics struct{}

func (noopTopics) Publish(context.Context, string, types.Message) error { return errNoTopics }

//nolint:ireturn // satisfies the Topics interface
func (noopTopics) Subscribe(
	context.Context, string, TopicHandler, ...SubscribeOption,
) (Subscription, error) {
	return noopSubscription{}, nil
}
