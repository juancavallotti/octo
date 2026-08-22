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
// The registry is per process. So is the http connector's registry of open SSE
// streams (runtime/connectors/http/sse.go), for the same reason and with the
// same consequence: across replicas a second request that lands elsewhere finds
// nothing and starts its own run.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

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

// liveRuns is the process's ai-agent runs currently in flight, by signal id.
//
// Package-level because a run and the invocation looking for it are different
// messages on different goroutines with nothing else in common — the same reason
// the http connector keeps its open streams on the connector rather than on the
// block that writes to them. An ai-agent belongs to no connector, so there is
// nowhere else for this to live.
var liveRuns = &runRegistry{runs: make(map[string]*agentRun)}

// runRegistry is the map, and the lock that makes a handover atomic.
//
// Every operation resolves inside the lock, so there is no window between
// finding a run and handing it something. That is the whole reason this is a
// registry rather than a queue: a request/reply round trip can only report that
// nobody answered *in time*, which is not the same fact and cannot be acted on
// the same way.
type runRegistry struct {
	mu   sync.Mutex
	runs map[string]*agentRun
}

// offerOrClaim hands text to the run already working on id, or registers mine
// and reports that the caller should run it.
//
// One method rather than a lookup and a send, because the two must not be
// separable: a caller that found a run, released the lock, and then offered
// could be handing work to a run that finished in between — losing the message
// and stopping the caller's flow on the strength of having placed it.
func (r *runRegistry) offerOrClaim(id, text string, mine *agentRun) (handedOff bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.runs[id]; ok && existing.offer(text) {
		return true
	}
	// Either nothing was running, or what was there has finished and has not yet
	// cleaned up. Take the id.
	r.runs[id] = mine
	return false
}

// stop asks the run working on id to end, and reports whether one was there.
func (r *runRegistry) stop(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.runs[id]
	return ok && existing.requestStop()
}

// release removes a run, leaving alone an id another run has already taken over
// — and doing nothing for an unreachable run, which never claimed one.
func (r *runRegistry) release(id string, mine *agentRun) {
	if id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs[id] == mine {
		delete(r.runs, id)
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

// offer adds a message for the run to answer, and reports whether it took it.
func (a *agentRun) offer(text string) bool {
	if a == nil || strings.TrimSpace(text) == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.done {
		return false // over; the caller should run it itself
	}
	a.pending = append(a.pending, text)
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
	msg *types.Message, cancel func(),
) (c runClaim, taken *types.Message, err error) {
	c.run = &agentRun{cancel: cancel}
	if c.threadID, err = a.resolveThread(msg); err != nil {
		return c, nil, err
	}
	// No thread is no conversation: a stateless agent has nothing another message
	// could join, and every run of one is its own.
	if c.threadID == "" {
		return c, nil, nil
	}
	// The registry is one map for the whole process, so the block's own address
	// goes in the key beside the thread the flow computed.
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
			slog.Info("ai-agent stop requested", "block", a.name, "stopped", liveRuns.stop(c.key))
			return c, stopFlow(msg), nil
		}
	}

	text, err := a.openingTurn(msg)
	if err != nil {
		return c, nil, err
	}
	if liveRuns.offerOrClaim(c.key, text, c.run) {
		slog.Info("ai-agent handed a message to the run already in flight",
			"block", a.name, "thread", c.threadID)
		return c, stopFlow(msg), nil
	}
	return c, nil, nil
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
}

// stopFlow ends the invocation with an empty body: whatever the answer is, it is
// going to whoever is reading the run this message reached.
func stopFlow(msg *types.Message) *types.Message {
	if err := msg.SetBodyJSON([]byte(stoppedBody)); err != nil {
		slog.Warn("ai-agent could not empty the body of a stopped flow", "error", err)
	}
	msg.RequestStop()
	return msg
}
