// A person in the loop of a tool call.
//
// Every other boundary an agent has is structural: a method allowlist, a tool a
// block does not hold, a specialist reachable only by a deliberate delegation.
// Those work because a model cannot talk its way past a thing that is not
// configured. What they cannot express is a rule about an *argument* — the
// difference between "this tool is a GET" and "this call is a PUT" — and once
// untrusted text can reach the conversation, that difference is where the danger
// is. A page excerpt can carry instructions; the prompt that says to ignore them
// is not a boundary.
//
// So this is the gate a model cannot answer: the run stops, says what it is about
// to do and with which arguments, and waits for somebody to allow it. The answer
// arrives the way a steer does — a second invocation on the same conversation,
// handed to the run in flight — because that path already exists and already
// answers "somebody took this" as a fact rather than a timeout.
//
// It cannot wait forever. A panel gets closed and a person walks away, and a run
// parked on a tool call is billed for nothing. Every way of not being allowed
// ends the same: a denial as the tool's own result, so the run carries on and the
// model is told plainly that the call was refused rather than left to interpret a
// hang.
package ai

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// eventToolAuthorization is the agent asking for a person before it runs a tool,
// with the arguments as the model asked for them and the id an answer quotes. It
// travels the ordinary events path, so a panel already reading tool_call frames
// needs no new transport to receive it.
const eventToolAuthorization = "tool_authorization"

// The authorization event's own fields, beside the tool and call id every tool
// event carries.
const (
	fieldAuthorizationID = "authorizationId"
	// fieldExpiresIn is how long the run will wait, in seconds, so a panel can show
	// the clock it is actually running against rather than one of its own choosing.
	fieldExpiresIn = "expiresInSeconds"
	// fieldAllowed is the decision, on the signal that carries it and on the event
	// reporting that the run consumed it.
	fieldAllowed = "allowed"
	// fieldReason says why a call was not authorized, in the tool result the model
	// reads.
	fieldReason = "reason"
)

// defaultAuthorizeTimeout is how long a run waits for an answer when the block
// names no limit.
//
// Five minutes is sized to a person who is at the panel and looking at something
// else — long enough that stepping away from the keyboard for a moment does not
// lose the call, short enough that a closed tab is not a conversation held open
// until the run age bound catches it a quarter of an hour later.
const defaultAuthorizeTimeout = 5 * time.Minute

// authorizationIDLen is how many random bytes an authorization id carries. A
// uuid's worth: the id is what an answer quotes, so guessing one has to be no
// easier than guessing a session.
const authorizationIDLen = 16

// authorization is one call waiting on a person.
type authorization struct {
	id      string
	call    core.LLMToolCall
	timeout time.Duration
}

// decision is what a gate resolved to, and why. denied carries a reason the model
// reads; allowed carries none.
type decision struct {
	allowed bool
	reason  string
}

// configureAgentAuthorization compiles the per-tool gates and the two expressions
// an answer arrives through.
//
// The checks here are all one shape: an authorization that cannot be answered is
// worse than no authorization at all, because it looks like a boundary and
// behaves like a broken tool. So a block that gates anything must also have a way
// to ask (an events path carrying the event) and a way to be told (an id and a
// conversation to deliver it on), and each missing half is refused at build time
// rather than discovered as a denial five minutes into a run.
func (b *builder) configureAgentAuthorization(block *aiAgent, cfg agentSettings) error {
	gates, err := b.agentGates(cfg.Tools)
	if err != nil {
		return err
	}
	block.gates = gates
	if err := b.configureAuthorizationAnswer(block, cfg); err != nil {
		return err
	}
	block.authorizeTimeout, err = authorizeTimeout(cfg.AuthorizeTimeout)
	if err != nil {
		return err
	}
	if len(gates) == 0 {
		return nil
	}
	switch {
	case block.authorizeID == nil:
		return errors.New(
			"ai-agent authorizeId is required when a tool declares authorize: without it " +
				"nothing can ever allow a gated call, and every one of them is denied on the timeout")
	case !block.events.Wants(eventToolAuthorization):
		return errors.New(
			"ai-agent tool authorization requires an events path emitting tool_authorization: " +
				"the event is how a person is asked, and a gate nobody is asked about only denies")
	}
	return nil
}

// agentGates compiles the tools' authorization conditions, keyed by tool name. A
// tool without one is absent from the map, which is what makes free the default.
func (b *builder) agentGates(tools []agentToolSettings) (map[string]*expr.Program, error) {
	var gates map[string]*expr.Program
	for _, tool := range tools {
		if strings.TrimSpace(tool.Authorize) == "" {
			continue
		}
		program, err := expr.CompileToolCall(b.deps.Resources, tool.Authorize)
		if err != nil {
			return nil, fmt.Errorf("ai-agent tool %q authorize: %w", tool.Name, err)
		}
		if gates == nil {
			gates = make(map[string]*expr.Program, len(tools))
		}
		gates[tool.Name] = program
	}
	return gates, nil
}

// configureAuthorizationAnswer compiles the pair of expressions an answer arrives
// through: which authorization it is for, and what it says.
func (b *builder) configureAuthorizationAnswer(block *aiAgent, cfg agentSettings) error {
	if strings.TrimSpace(cfg.AuthorizeID) == "" {
		if strings.TrimSpace(cfg.AuthorizeAllow) != "" {
			return errors.New(
				"ai-agent authorizeAllow requires authorizeId: an answer says yes or no to a " +
					"particular call, and without an id there is nothing for it to be about")
		}
		return nil
	}
	if cfg.MemoryThreadID == "" {
		return errors.New(
			"ai-agent authorizeId requires memoryThreadId: an answer is handed to the run " +
				"working on a conversation, and without one there is nothing to name")
	}
	if strings.TrimSpace(cfg.AuthorizeAllow) == "" {
		return errors.New(
			"ai-agent authorizeId requires authorizeAllow: an invocation carrying an id is an " +
				"answer, and without a condition there is nothing for it to answer")
	}
	id, err := expr.CompileMessage(b.deps.Resources, cfg.AuthorizeID)
	if err != nil {
		return fmt.Errorf("ai-agent authorizeId: %w", err)
	}
	allow, err := expr.CompileMessage(b.deps.Resources, cfg.AuthorizeAllow)
	if err != nil {
		return fmt.Errorf("ai-agent authorizeAllow: %w", err)
	}
	block.authorizeID, block.authorizeAllow = id, allow
	return nil
}

// authorizeTimeout reads the configured wait, applying the default and refusing a
// value that is not a duration.
func authorizeTimeout(configured string) (time.Duration, error) {
	if strings.TrimSpace(configured) == "" {
		return defaultAuthorizeTimeout, nil
	}
	timeout, err := time.ParseDuration(configured)
	if err != nil {
		return 0, fmt.Errorf("ai-agent authorizeTimeout: %w", err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("ai-agent authorizeTimeout must be positive, got %s", configured)
	}
	return timeout, nil
}

// gate decides whether a call may run, asking a person when the tool says to.
//
// It returns the denial to feed the model in place of the call's result, whether
// there is one, and whether the events path asked the run to stop on the way
// past. A call that needs no authorization, or one that is allowed, comes back
// with denied false and nothing else to do.
func (a *aiAgent) gate(
	ctx context.Context, iter int, call core.LLMToolCall, current *types.Message, run *agentRun,
) (res core.LLMToolResult, denied, stopped bool) {
	program, gated := a.gates[call.Name]
	if !gated {
		return core.LLMToolResult{}, false, false
	}
	needed, err := a.gateApplies(program, call, current)
	if err != nil {
		// Fail closed. The condition is what decides whether this call is dangerous,
		// so a condition that did not evaluate is not an answer of "no it isn't" — and
		// the one place a security gate must not be generous is where it is broken.
		slog.Warn("ai-agent could not evaluate a tool's authorize condition",
			"block", a.name, "tool", call.Name, "error", err)
		return denialResult(call, fmt.Sprintf(
			"the authorization condition for this tool could not be evaluated: %v", err)), true, false
	}
	if !needed {
		return core.LLMToolResult{}, false, false
	}

	auth := authorization{id: mintAuthorizationID(), call: call, timeout: a.authorizeTimeout}
	answer, stopped := a.askAuthorization(ctx, iter, auth, current, run)
	if answer.allowed {
		slog.Info("ai-agent tool call authorized",
			"block", a.name, "tool", call.Name, "authorization", auth.id)
		return core.LLMToolResult{}, false, stopped
	}
	slog.Warn("ai-agent tool call not authorized",
		"block", a.name, "tool", call.Name, "authorization", auth.id, "reason", answer.reason)
	return denialResult(call, answer.reason), true, stopped
}

// gateApplies evaluates one tool's authorization condition against the call the
// model asked for.
func (a *aiAgent) gateApplies(
	program *expr.Program, call core.LLMToolCall, current *types.Message,
) (bool, error) {
	activation := expr.ToolCallActivation(
		current, a.env, call.Name, call.ID, decodeJSON(call.Input))
	value, err := program.Eval(activation)
	if err != nil {
		return false, err
	}
	needed, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("authorize must evaluate to a bool, got %T", value)
	}
	return needed, nil
}

// askAuthorization puts the call in front of a person and waits for the answer.
//
// The wait ends four ways and three of them are denials. An answer allows or
// refuses it; the clock runs out, which is a refusal on behalf of somebody who is
// not there; or the run is stopped or its context ends, which is not a refusal of
// this call so much as the end of everything including it. Only the first is
// somebody's decision, and the rest are written as what they are so the model is
// never told a person said no when nobody did.
func (a *aiAgent) askAuthorization(
	ctx context.Context, iter int, auth authorization, current *types.Message, run *agentRun,
) (answer decision, stopped bool) {
	// Open before asking. An answer can come back before the event has finished
	// travelling — the events path is a sub-flow and a fast panel is faster than a
	// slow sink — and a gate that is not open yet has nowhere to put it.
	answers := run.openGate(auth.id)
	defer run.closeGate(auth.id)

	if a.report(ctx, current, iter, eventToolAuthorization, authorizationFields(auth)) {
		// The events path asked the run to stop, which for a closed connection —
		// `ifClosed: stop` — means the only person who could answer is gone. Denying
		// now is the honest reading of that, and waiting out the clock would hold a
		// conversation open for somebody who has already left.
		return decision{reason: "the connection that would have asked for authorization is closed"}, true
	}

	timer := time.NewTimer(auth.timeout)
	defer timer.Stop()
	select {
	case allowed := <-answers:
		a.report(ctx, current, iter, eventSignal, map[string]any{
			fieldSignal: signalAuthorize, fieldAuthorizationID: auth.id, fieldAllowed: allowed,
		})
		if allowed {
			return decision{allowed: true}, false
		}
		return decision{reason: "a person denied this call"}, false
	case <-timer.C:
		return decision{reason: fmt.Sprintf(
			"nobody authorized this call within %s", auth.timeout)}, false
	case <-ctx.Done():
		return decision{reason: "the run ended before this call was authorized"}, false
	}
}

// authorizationFields describes a call waiting on a person: what is about to run,
// with which arguments, under which id, and for how long it will wait.
func authorizationFields(auth authorization) map[string]any {
	return map[string]any{
		fieldTool:            auth.call.Name,
		fieldToolCallID:      auth.call.ID,
		fieldAuthorizationID: auth.id,
		fieldInput:           decodeJSON(auth.call.Input),
		fieldExpiresIn:       int(auth.timeout.Seconds()),
	}
}

// denialResult is what the model gets in place of the call it asked for.
//
// It is a tool result and not a failed run, which is the whole point: the model is
// told the call did not happen and why, and goes on to do something else — say so,
// ask, take another route — instead of interpreting a hang. It is marked as an
// error so a provider renders it as one rather than as data the tool returned, and
// it is stated in the same words every time so a model cannot mistake one denial
// for another kind of failure it might retry around.
func denialResult(call core.LLMToolCall, reason string) core.LLMToolResult {
	content, err := json.Marshal(map[string]any{"authorized": false, fieldReason: reason})
	if err != nil {
		// Only a reason that will not marshal reaches here, and there is no such
		// string; the fallback exists so a denial is never lost to an encoding error.
		content = []byte(`{"authorized":false,"reason":"this call was not authorized"}`)
	}
	return core.LLMToolResult{
		ToolCallID: call.ID,
		Tool:       call.Name,
		Content:    string(content),
		IsError:    true,
	}
}

// mintAuthorizationID names one waiting call, so an answer can say which one it is
// about.
func mintAuthorizationID() string {
	buf := make([]byte, authorizationIDLen)
	if _, err := rand.Read(buf); err != nil {
		// A broken entropy source. An empty id is unanswerable, so the gate denies on
		// the timeout rather than accepting the first answer that comes along — which
		// is what a predictable id would let a second, unrelated answer do.
		slog.Warn("ai-agent could not mint an authorization id", "error", err)
		return ""
	}
	return "auth_" + hex.EncodeToString(buf)
}

// answerAuthorization delivers an answer to the run holding this conversation, and
// reports whether one was waiting for it.
//
// It is the third thing an invocation can be, beside a message and a stop, and it
// takes the same path: the local registry first, the conversation's subject on a
// miss. Nothing about the delivery is new — only what the run does with it.
func (a *aiAgent) answerAuthorization(
	ctx context.Context, msg *types.Message, key string,
) (answered bool, isAnswer bool, err error) {
	if a.authorizeID == nil {
		return false, false, nil
	}
	activation := expr.MessageActivation(msg, a.env)
	id, err := a.authorizeID.EvalString(activation)
	if err != nil {
		return false, false, fmt.Errorf("ai-agent authorizeId: %w", err)
	}
	if strings.TrimSpace(id) == "" {
		return false, false, nil
	}
	// Anything but a plain yes is a no, an expression that failed to evaluate
	// included: an answer that could not be read is not permission.
	allowed, err := expr.EvalCondition(a.authorizeAllow, msg, a.env)
	if err != nil {
		slog.Warn("ai-agent could not read an authorization answer, denying",
			"block", a.name, "authorization", id, "error", err)
		allowed = false
	}
	return liveRuns.answer(ctx, key, id, allowed), true, nil
}
