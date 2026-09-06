// Reaching a run on another replica.
//
// The registry next door is a map, which settles a conversation's ownership
// inside one process and says nothing at all about the others. Across replicas
// that shows up as a stop that stops nothing *and reports success*, which is the
// one failure here that looks exactly like the thing working.
//
// Two jobs, and only one of them is hard. Claiming needs exclusivity decided
// atomically with a way to recover from a holder that died — core.Leases.
// Delivering needs a message to reach one known subscriber — core.Queues, whose
// reply is what turns "somebody took this" from a hopeful timeout into a fact.
// Neither is new machinery and neither needs JetStream.
//
// See docs/distributed-agent-runs.md for what this fixes and what it does not.
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// claimSubjectPrefix names the conversation's delivery subject. The rest is a
	// hash, because the key is built from an expression an author wrote and a
	// thread a caller chose — text that may contain anything, and that a broker
	// reads as structure.
	claimSubjectPrefix = "agent-run-"
	// subjectHashLen is how much of the hash rides in the subject. Sixteen bytes
	// is far past the point where a collision between two conversations on one
	// deployment is worth reasoning about, and keeps the subject readable in a log.
	subjectHashLen = 32
)

// deliveryTimeout bounds the wait for the holder to answer.
//
// Short, because a live holder replies from a mutex and nothing else: the wait is
// a round trip, not a queue. The whole point of the reply is to tell a caller
// whether to stop its own flow, and that caller has a person waiting on it.
const deliveryTimeout = 5 * time.Second

// The delivery envelope's fields. It is deliberately not the caller's message:
// what travels is the instruction, not the request that produced it.
const (
	fieldAccepted = "accepted"
)

// delivery is one instruction addressed to a conversation: which kind it is, and
// whatever that kind carries.
//
// A struct rather than a widening argument list, because the three kinds carry
// different things — a steer carries text, an authorization carries an id and a
// decision, a stop carries nothing — and a fourth positional string is how the
// wrong one ends up in the wrong field at a call site.
type delivery struct {
	signal string
	text   string
	// authorizationID and allowed are the authorize signal's, and are read only
	// for it.
	authorizationID string
	allowed         bool
}

// body is what travels on the wire. Only the fields the signal uses are written,
// so a stop is still an envelope with a signal in it and nothing else.
func (d delivery) body() map[string]any {
	body := map[string]any{fieldSignal: d.signal, fieldText: d.text}
	if d.signal == signalAuthorize {
		body[fieldAuthorizationID] = d.authorizationID
		body[fieldAllowed] = d.allowed
	}
	return body
}

// errNobodyHome is the delivery that found no run behind a claim. It never
// escapes this file — it decides whether to try the claim again — but it is a
// named value so the two places that produce it cannot drift apart.
var errNobodyHome = errors.New("the conversation's claim holder did not answer")

// heldClaim is what a run holds while it owns a conversation across the cluster:
// the lease that makes it the owner, and the subscription other replicas reach it
// through. Both are nil for a run that claimed nothing.
type heldClaim struct {
	lease core.Lease
	sub   core.Subscription
}

// close gives up the claim. The subscription goes first: a replica that can still
// find this run is better than one told the name is free while it is still
// answering.
func (h *heldClaim) close() {
	if h == nil {
		return
	}
	if h.sub != nil {
		if err := h.sub.Close(); err != nil {
			slog.Debug("ai-agent could not close a claim's subscription", "error", err)
		}
	}
	if h.lease != nil {
		if err := h.lease.Close(); err != nil {
			slog.Debug("ai-agent could not release a claim", "error", err)
		}
	}
}

// claimAcross takes the cluster-wide claim on key for run, or reports that
// somebody else has it.
//
// The lease is taken before the local registry entry, not after, and that order
// is the whole correctness argument: a live local run always holds the lease, so
// holding it is proof that no other run — here or anywhere — is registered under
// this key. Registering first and discovering the refusal afterwards would leave
// a window in which a second local invocation handed its message to a run about
// to find out it owns nothing.
func claimAcross(ctx context.Context, key string, run *agentRun) (*heldClaim, bool, error) {
	leases := core.RuntimeServicesFromContext(ctx).Leases()
	lease, ok, err := leases.Acquire(ctx, key)
	if err != nil {
		return nil, false, fmt.Errorf("claim the conversation: %w", err)
	}
	if !ok {
		return nil, false, nil
	}

	sub, err := subscribeForRun(ctx, key, run)
	if err != nil {
		// Owning a conversation nobody can reach is worse than not owning it: every
		// follow-up would be refused delivery and every stop would silently fail.
		// Give the name back and let the caller decide.
		if closeErr := lease.Close(); closeErr != nil {
			slog.Debug("ai-agent could not release a claim it could not subscribe for", "error", closeErr)
		}
		return nil, false, err
	}
	return &heldClaim{lease: lease, sub: sub}, true, nil
}

// subscribeForRun routes messages addressed to this conversation into the run.
//
// One listener, not the default eight: everything a handler does happens under
// the run's own mutex, so extra goroutines would buy contention rather than
// throughput, and they would reorder follow-ups that a person sent in order.
//
//nolint:ireturn // hands back the core.Subscription the claim holds and closes
func subscribeForRun(ctx context.Context, key string, run *agentRun) (core.Subscription, error) {
	queues := core.RuntimeServicesFromContext(ctx).Queues()
	subject := claimSubject(key)

	// Subscribed on a context detached from the caller's: the subscription belongs
	// to the run, and the run outlives the request that started it.
	sub, err := queues.Subscribe(context.WithoutCancel(ctx), subject,
		func(_ context.Context, msg types.Message) (types.Message, error) {
			return handleDelivery(msg, run), nil
		}, core.WithListeners(1))
	if err != nil {
		return nil, fmt.Errorf("subscribe for the conversation on %q: %w", subject, err)
	}
	return sub, nil
}

// handleDelivery applies one instruction to the run and answers whether the run
// took it.
//
// The answer travels in the reply body and never as an error. Both queue modules
// drop the reply when a handler errors, which the sender experiences as a
// timeout — the slowest possible way to be told a fast, definite "no".
//
// It emits no agent event. It runs on a listener goroutine, and the events path
// is contracted to the flow's own; the signal is reported when the run consumes
// it, on the goroutine that owns the conversation.
func handleDelivery(msg types.Message, run *agentRun) types.Message {
	d := readDelivery(msg)

	accepted := false
	switch d.signal {
	case signalStop:
		accepted = run.requestStop()
	case signalAuthorize:
		accepted = run.authorize(d.authorizationID, d.allowed)
	default:
		accepted = run.offer(d.text)
	}

	var reply types.Message
	reply.SetBody(map[string]any{fieldAccepted: accepted})
	return reply
}

// deliver hands an instruction to whoever holds key, and reports whether they
// took it.
func deliver(ctx context.Context, key string, d delivery) (bool, error) {
	queues := core.RuntimeServicesFromContext(ctx).Queues()
	subject := claimSubject(key)

	envelope, err := types.NewMessage("")
	if err != nil {
		return false, fmt.Errorf("build a delivery for %q: %w", subject, err)
	}
	envelope.SetBody(d.body())

	reply, err := queues.Request(ctx, subject, *envelope, core.WithTimeout(deliveryTimeout))
	if err != nil {
		// Nobody answered. That is not a transport failure to report upwards: the
		// holder released between the refused claim and this request, or its replica
		// went away. Either way the caller's next move is to try the claim again.
		slog.Debug("ai-agent found nobody behind a claim", "subject", subject, "error", err)
		return false, errNobodyHome
	}
	return acceptedBy(reply), nil
}

// acceptedBy reads the holder's answer. Anything unreadable counts as not taken,
// which is the safe direction: the caller then goes and answers the message
// itself rather than dropping it on the strength of a reply it could not parse.
func acceptedBy(reply types.Message) bool {
	body, ok := reply.Body.(map[string]any)
	if !ok {
		return false
	}
	accepted, _ := body[fieldAccepted].(bool)
	return accepted
}

// readDelivery pulls the instruction out of an envelope, tolerating one that is
// not shaped as expected — an unrecognised body is a context signal with nothing
// in it, which offer takes and does not queue.
func readDelivery(msg types.Message) delivery {
	body, ok := msg.Body.(map[string]any)
	if !ok {
		return delivery{signal: signalContext}
	}
	d := delivery{}
	d.signal, _ = body[fieldSignal].(string)
	d.text, _ = body[fieldText].(string)
	d.authorizationID, _ = body[fieldAuthorizationID].(string)
	d.allowed, _ = body[fieldAllowed].(bool)
	if d.signal == "" {
		d.signal = signalContext
	}
	return d
}

// claimSubject is the delivery address for a conversation, derived so that every
// replica computes the same one from the same key.
func claimSubject(key string) string {
	sum := sha256.Sum256([]byte(key))
	return claimSubjectPrefix + hex.EncodeToString(sum[:])[:subjectHashLen]
}
