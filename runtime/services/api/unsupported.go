package api

import (
	"context"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The PolicyError implementations: every call refuses, naming the feature and the
// fix. They exist alongside core's no-ops rather than instead of them — a feature
// resolves to one or the other, decided once in New — so no call site ever asks
// whether its capability is really there.

// erroringKV refuses every operation, reads included. That is the difference from
// core.NoopKV, whose reads miss silently: here a read that would have found
// something is a fact the caller should hear about.
type erroringKV struct{}

func (erroringKV) Get(context.Context, string, string) (core.Entry, bool, error) {
	return core.Entry{}, false, unsupportedError(FeatureKV)
}

func (erroringKV) Set(context.Context, string, string, []byte, int64) (int64, error) {
	return 0, unsupportedError(FeatureKV)
}

func (erroringKV) Delete(context.Context, string, string, int64) error {
	return unsupportedError(FeatureKV)
}

// erroringSecrets refuses secret access while ordinary KV keeps working. "I store
// values but not secrets" is a statement a platform should be able to make and
// have honoured, rather than having its secrets silently written to the same
// unencrypted place as everything else.
type erroringSecrets struct{}

func (erroringSecrets) Get(context.Context, string, string) (core.Entry, bool, error) {
	return core.Entry{}, false, unsupportedError(FeatureSecrets)
}

func (erroringSecrets) Set(context.Context, string, string, []byte, int64) (int64, error) {
	return 0, unsupportedError(FeatureSecrets)
}

func (erroringSecrets) Delete(context.Context, string, string, int64) error {
	return unsupportedError(FeatureSecrets)
}

// erroringResources refuses to load.
type erroringResources struct{}

func (erroringResources) Load(context.Context, core.ResourceKind, string) ([]byte, error) {
	return nil, unsupportedError(FeatureResources)
}

// erroringLeases refuses to decide a claim, which is not the same as deciding it
// is taken: core.Leases distinguishes those, and a caller that reads "somebody
// else has it" would go do something else, whereas one that reads an error knows
// the question could not be answered.
type erroringLeases struct{}

//nolint:ireturn // satisfies core.Leases
func (erroringLeases) Acquire(
	context.Context, string, ...core.LeaseOption,
) (core.Lease, bool, error) {
	return nil, false, unsupportedError(FeatureLeases)
}

// erroringLeaderElection refuses to campaign.
//
// The alternative — core.NoopLeaderElection — grants leadership to everyone, which
// on a platform this module runs against usually means every replica running the
// work exactly once each. See defaultPolicy.
type erroringLeaderElection struct{}

//nolint:ireturn // satisfies core.LeaderElection
func (erroringLeaderElection) Acquire(context.Context, string) (core.Leadership, error) {
	return nil, unsupportedError(FeatureLeaderElection)
}

// erroringQueues refuses both sends and subscriptions.
type erroringQueues struct{}

func (erroringQueues) Publish(context.Context, string, types.Message) error {
	return unsupportedError(FeatureQueues)
}

func (erroringQueues) Request(
	context.Context, string, types.Message, ...core.RequestOption,
) (types.Message, error) {
	return types.Message{}, unsupportedError(FeatureQueues)
}

//nolint:ireturn // satisfies core.Queues
func (erroringQueues) Subscribe(
	context.Context, string, core.QueueHandler, ...core.SubscribeOption,
) (core.Subscription, error) {
	return nil, unsupportedError(FeatureQueues)
}

// erroringTopics refuses both sends and subscriptions.
type erroringTopics struct{}

func (erroringTopics) Publish(context.Context, string, types.Message) error {
	return unsupportedError(FeatureTopics)
}

//nolint:ireturn // satisfies core.Topics
func (erroringTopics) Subscribe(
	context.Context, string, core.TopicHandler, ...core.SubscribeOption,
) (core.Subscription, error) {
	return nil, unsupportedError(FeatureTopics)
}
