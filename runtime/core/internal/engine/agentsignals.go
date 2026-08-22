// Reaching an ai-agent run that is already in flight.
//
// An agent run is otherwise a closed loop: a request goes in, an answer comes
// out, and nothing in between can change its mind or end it early. That is right
// for a transformation and wrong for a conversation, where a person changes what
// they wanted — or stops wanting it — while the agent is still working.
//
// A run with a signalId puts itself in a registry for the length of its run. A
// second invocation that resolves the same id finds it there and hands its
// message over instead of starting a run of its own, which is what stops one
// person getting two answers on two streams. There is no transport and no
// message in flight: the lookup and the handover happen under one lock, so
// "somebody took this" is a fact rather than a hopeful timeout.
//
// The map is per process, and a deployment is not. So the map is the fast path
// rather than the answer: when it misses, the conversation is claimed
// cluster-wide and the message is delivered to whichever replica holds it. That
// part lives in agentclaim.go; this file is the mechanics of a run and the lock
// that makes a handover atomic.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// The signal event's own field, and the one kind of thing that reaches a run
// from outside it. They are spelled here beside the mechanism that produces
// them rather than with the provider's stream vocabulary next door.
const (
	fieldSignal   = "signal"
	signalContext = "context"
	signalStop    = "stop"
)

// liveRuns is the process's ai-agent runs currently in flight, by claim key.
//
// Package-level because a run and the invocation looking for it are different
// messages on different goroutines with nothing else in common — the same reason
// the http connector keeps its open streams on the connector rather than on the
// block that writes to them. An ai-agent belongs to no connector, so there is
// nowhere else for this to live.
var liveRuns = &runRegistry{runs: make(map[string]*agentRun)}

// claimAttempts bounds how many times an invocation will re-try the claim when
// the holder answers that it has finished.
//
// That answer means the name is about to be free but is not yet: the run has
// stopped accepting and has not released its lease. Retrying costs a moment;
// giving up would drop a message the person is waiting on. Three is enough for
// a release that is already under way and few enough to fail loudly if it is not.
const claimAttempts = 3

// claimRetryDelay is the pause between those attempts, sized to a lease release
// rather than to a network — the holder is finishing, not unreachable.
const claimRetryDelay = 100 * time.Millisecond

// runRegistry is the map, and the lock that makes a local handover atomic.
//
// A local lookup and offer resolve inside the lock, so there is no window
// between finding a run and handing it something: a caller that found a run,
// released the lock, and then offered could be handing work to a run that
// finished in between — losing the message and stopping the caller's flow on the
// strength of having placed it.
//
// A run that reaches this map always holds the cluster claim, which is what lets
// the map be checked first and the claim only on a miss.
type runRegistry struct {
	mu   sync.Mutex
	runs map[string]*agentRun
}

// offerOrClaim hands text to whoever is working on key — in this process or on
// another replica — or claims the conversation for mine and reports that the
// caller should run it.
//
// held is non-nil exactly when the caller claimed the conversation, and is what
// it gives back when the run ends.
func (r *runRegistry) offerOrClaim(
	ctx context.Context, key, text string, mine *agentRun,
) (held *heldClaim, handedOff bool, err error) {
	if r.offerLocal(key, text) {
		return nil, true, nil
	}
	for attempt := range claimAttempts {
		held, ok, claimErr := claimAcross(ctx, key, mine)
		if claimErr != nil {
			return nil, false, claimErr
		}
		if ok {
			r.take(key, mine)
			return held, false, nil
		}

		accepted, deliverErr := deliver(ctx, key, signalContext, text)
		switch {
		case deliverErr == nil && accepted:
			return nil, true, nil
		case deliverErr != nil && !errors.Is(deliverErr, errNobodyHome):
			return nil, false, deliverErr
		}
		// Either nobody was behind the claim or the holder has finished with the
		// conversation. Both mean the name is on its way to being free.
		if err := pause(ctx, attempt); err != nil {
			return nil, false, err
		}
	}
	return nil, false, fmt.Errorf(
		"the conversation is claimed by a run that has stopped accepting and has not released it")
}

// offerLocal hands text to a run this process is already working on.
func (r *runRegistry) offerLocal(key, text string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.runs[key]
	return ok && existing.offer(text)
}

// take records mine as this process's run for key. It is called only once the
// cluster claim is held, which is what makes overwriting whatever was there
// safe: anything still in the map under this key belongs to a run that has
// released its claim and not yet cleaned up.
func (r *runRegistry) take(key string, mine *agentRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[key] = mine
}

// stop asks whoever is working on key to end, and reports whether one was there.
//
// Not finding a run is an answer rather than a failure — a stop can be sent by a
// client that cannot know whether one is still going — so an unreachable holder
// reports false rather than erroring.
func (r *runRegistry) stop(ctx context.Context, key string) bool {
	if r.stopLocal(key) {
		return true
	}
	accepted, err := deliver(ctx, key, signalStop, "")
	if err != nil {
		return false
	}
	return accepted
}

// stopLocal asks a run in this process to end.
func (r *runRegistry) stopLocal(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.runs[key]
	return ok && existing.requestStop()
}

// release removes a run, leaving alone a key another run has already taken over
// — and doing nothing for an unreachable run, which never claimed one.
func (r *runRegistry) release(key string, mine *agentRun) {
	if key == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs[key] == mine {
		delete(r.runs, key)
	}
}

// pause waits between claim attempts, and gives up if the caller has.
func pause(ctx context.Context, attempt int) error {
	if attempt == claimAttempts-1 {
		return nil // nothing follows the last attempt
	}
	timer := time.NewTimer(claimRetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for the conversation's claim: %w", ctx.Err())
	}
}

// agentRun is one in-flight run, as everything outside it sees it.
//
// Its lock covers more than holding a slice needs, and that is the point.
// Accepting a message and deciding the run is over are the same decision: a run
// that took a message and then returned would have lost it *and* stopped the
// sender's flow on the strength of having taken it. So while anything is
// pending, the run is not allowed to finish — it takes another turn instead,
// which is also what makes a follow-up typed mid-answer behave the way it does
// in any chat.
//
// A nil run is inert and safe to call, which is what an agent with no signalId
// has.
type agentRun struct {
	mu      sync.Mutex
	pending []string
	stopped bool
	done    bool
	// cancel ends the run's context, which is what abandons a model call already
	// in flight rather than paying for the rest of it.
	cancel func()
}

// offer hands a message to the run and reports whether it took responsibility
// for it.
//
// The answer is about the run's liveness, not about the message: a live run
// takes responsibility for anything on its conversation, and false means only
// "this run is over, go and start your own". Reporting false for a message a
// live run had nothing to do with would evict it from the registry and let a
// rival start on the same thread — the exact thing the claim exists to stop.
//
// Blank text is such a message. It is not appended, because a pending entry with
// nothing in it would block the run from finishing and then inject nothing,
// buying a billed turn to say nothing. But the run still has the conversation,
// so the invocation that sent it still stops.
func (a *agentRun) offer(text string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return false // over; the caller should run it itself
	}
	if trimmed := strings.TrimSpace(text); trimmed != "" {
		a.pending = append(a.pending, trimmed)
	}
	return true
}

// requestStop ends the run, cancelling whatever it is waiting on.
func (a *agentRun) requestStop() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return false
	}
	a.stopped = true
	if a.cancel != nil {
		a.cancel()
	}
	return true
}

// take removes and returns the messages waiting to be answered.
func (a *agentRun) take() []string {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := a.pending
	a.pending = nil
	return out
}

// stopRequested reports whether someone asked this run to end.
func (a *agentRun) stopRequested() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stopped
}

// finish closes the run and reports whether it may end.
//
// It answers false when a message arrived while the run was producing its final
// answer: that message was accepted, and the invocation that offered it has
// already stopped its own flow expecting this run to handle it, so returning
// now would drop it.
func (a *agentRun) finish() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) > 0 {
		return false
	}
	a.done = true
	return true
}

// close ends the run's acceptance whatever it is doing.
//
// It is every exit that is not the clean answer — a stop, a halted tool branch,
// an exhausted iteration cap, a failed call — where there is no turn left to act
// on a message in. Unlike finish it cannot refuse, because there is nothing left
// to refuse on behalf of.
func (a *agentRun) close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.done = true
}

// injectPending folds anything handed to the run into the conversation as user
// turns, and reports each one.
//
// It runs at the top of an iteration, which is the only safe seam: a tool turn
// has to follow immediately after the assistant turn that asked for it, so
// nothing may be slipped between them.
func (a *aiAgent) injectPending(
	ctx context.Context, msg *types.Message, messages []core.LLMMessage, iter int, run *agentRun,
) []core.LLMMessage {
	for _, text := range run.take() {
		messages = append(messages, core.LLMMessage{Role: core.LLMRoleUser, Text: text})
		slog.Info("ai-agent took a message handed to it mid-run", "block", a.name)
		a.report(ctx, msg, iter, eventSignal, map[string]any{fieldSignal: signalContext, fieldText: text})
	}
	return messages
}

// configureAgentSignals compiles the condition that ends a run, and records the
// address that scopes this block's claims.
//
// There is no identifier to configure: a run is claimed on the conversation it
// belongs to, which memoryThreadId already names. Asking for a second expression
// saying the same thing would be asking the author to keep two answers to one
// question in step.
func (b *builder) configureAgentSignals(block *aiAgent, cfg types.BlockConfig) error {
	block.runScope = b.deps.Address.Path
	if strings.TrimSpace(cfg.StopWhen) == "" {
		return nil
	}
	if cfg.MemoryThreadID == "" {
		return errors.New(
			"ai-agent stopWhen requires memoryThreadId: a run is stopped by the conversation " +
				"it belongs to, and without one there is nothing to name")
	}
	stop, err := expr.CompileMessage(b.deps.Resources, cfg.StopWhen)
	if err != nil {
		return fmt.Errorf("ai-agent stopWhen: %w", err)
	}
	block.stopWhen = stop
	return nil
}

// stoppedBody is what a flow that reached a run returns. There is nothing to
// say: the answer is going to whoever is reading the run it reached.
const stoppedBody = "{}"

// joinOrClaim decides what this invocation is for before any model is called.
//
// Three outcomes. A stateless agent claims nothing and every run of one is its
// own. An invocation whose stop condition holds ends the run already working on
// its conversation, and stops. Otherwise it offers its message to that run — and
// if one took it, stops too, because the person who sent it is already reading
// that run's output and a second run would answer them twice. Only when nothing
// was there does it claim the conversation and go on to do the work.
//
// taken is non-nil exactly when the flow should stop here.
func (a *aiAgent) joinOrClaim(
	ctx context.Context, msg *types.Message, cancel func(),
) (c runClaim, taken *types.Message, err error) {
	c.run = &agentRun{cancel: cancel}
	c.bound = noBound
	if c.threadID, err = a.resolveThread(msg); err != nil {
		return c, nil, err
	}
	// No thread is no conversation: a stateless agent has nothing another message
	// could join, and every run of one is its own.
	if c.threadID == "" {
		return c, nil, nil
	}
	// The claim is shared by every agent on the deployment, so the block's own
	// address goes in the key beside the thread the flow computed.
	//
	// Without it, two agents whose thread expressions happen to agree — and
	// body.threadId is the easy way for that to happen — would hand each other
	// messages: a request meant for the support agent taken by the sales agent,
	// and the caller's flow stopped on the strength of it. The thread says which
	// conversation; the address says which agent is having it, and only the second
	// is something the runtime can know for itself.
	c.key = a.runScope + "\x00" + c.threadID

	if a.stopWhen != nil {
		stop, evalErr := evalCondition(a.stopWhen, msg, a.env)
		if evalErr != nil {
			return c, nil, fmt.Errorf("ai-agent stopWhen: %w", evalErr)
		}
		if stop {
			// Idempotent: stopping a run that is not there is not an error, so a stop
			// can be sent blind by a client that cannot know whether one is still going.
			slog.Info("ai-agent stop requested",
				"block", a.name, "stopped", liveRuns.stop(ctx, c.key))
			return c, stopFlow(msg), nil
		}
	}

	text, err := a.openingTurn(msg)
	if err != nil {
		return c, nil, err
	}
	held, handedOff, err := liveRuns.offerOrClaim(ctx, c.key, text, c.run)
	if err != nil {
		return c, nil, err
	}
	if handedOff {
		slog.Info("ai-agent handed a message to the run already in flight",
			"block", a.name, "thread", c.threadID)
		return c, stopFlow(msg), nil
	}
	c.held = held
	c.bound = boundRunAge(c.run, a.name, c.threadID, maxRunAge)
	watchClaim(held, c.run, a.name, c.threadID)
	return c, nil, nil
}

// maxRunAge is the longest a run may hold the conversation it claimed.
//
// Without it a run that wedges — a tool branch with no timeout of its own — owns
// its conversation until the process restarts, and every follow-up is handed to
// something that will never answer. A cluster-wide claim makes that worse rather
// than better: the conversation is then out of service for the whole deployment
// instead of for one replica.
//
// It bounds a conversation, not an agent. A run with no memoryThreadId claims
// nothing and is not timed, so a genuinely long batch run is unaffected; fifteen
// minutes is far past any interactive turn and short enough that a wedged
// conversation comes back on its own.
const maxRunAge = 15 * time.Minute

// boundRunAge stops the run once it has held its conversation too long, and
// returns the function that cancels the bound when it ends normally.
//
// It is a stop rather than a failure on purpose: the run takes the path a stop
// already takes, so the model call in flight is abandoned, the transcript is
// pruned rather than summarized, and the claim is given back — leaving the
// conversation in a state the next message can pick up.
func boundRunAge(run *agentRun, block, thread string, limit time.Duration) func() bool {
	timer := time.AfterFunc(limit, func() {
		if run.requestStop() {
			slog.Warn("ai-agent stopped a run that held its conversation too long",
				"block", block, "thread", thread, "limit", limit)
		}
	})
	return timer.Stop
}

// watchClaim ends the run if it loses the conversation.
//
// Losing a claim means another replica now owns the conversation and will save
// it, so carrying on would be two runs writing one transcript — the case where
// the later save wins entirely and the other run's turns are gone. It is
// therefore a stop, and takes the stop path: the model call in flight is
// abandoned rather than paid for, and the transcript is pruned rather than
// summarized on the way out.
//
// The goroutine exits when the lease is closed, which the run does on its way
// out — by which time it is already marked done, so the stop below is a no-op.
func watchClaim(held *heldClaim, run *agentRun, block, thread string) {
	if held == nil || held.lease == nil {
		return
	}
	go func() {
		<-held.lease.Done()
		if run.requestStop() {
			slog.Warn("ai-agent lost the claim on its conversation and stopped",
				"block", block, "thread", thread)
		}
	}()
}

// runClaim is what one invocation established before its first turn: which
// conversation it belongs to, the registry entry it holds, and the handle other
// invocations reach it through.
//
// A zero key means it claimed nothing — a stateless agent, or one whose thread
// expression came up empty — and release then does nothing.
type runClaim struct {
	threadID string
	key      string
	run      *agentRun
	// held is the cluster-wide claim, and is nil for an invocation that took
	// none — a stateless agent, or one that handed its message to somebody else.
	held *heldClaim
	// bound cancels the maximum-age timer. It is never nil: an invocation that
	// claimed nothing gets one that does nothing, so the caller can defer it
	// without asking whether there is anything to cancel.
	bound func() bool
}

// noBound is the age bound of an invocation that claimed no conversation.
func noBound() bool { return false }

// stopFlow ends the invocation with an empty body: whatever the answer is, it is
// going to whoever is reading the run this message reached.
func stopFlow(msg *types.Message) *types.Message {
	if err := msg.SetBodyJSON([]byte(stoppedBody)); err != nil {
		slog.Warn("ai-agent could not empty the body of a stopped flow", "error", err)
	}
	msg.RequestStop()
	return msg
}
