package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// logMaxLen caps prompt/response strings in debug logs so a long body or system
// prompt stays readable in the log stream.
const logMaxLen = 2000

// truncForLog shortens s for logging, marking where it was cut.
func truncForLog(s string) string {
	if len(s) > logMaxLen {
		return s[:logMaxLen] + "…(truncated)"
	}
	return s
}

// toolCallNames lists the tool names in a set of calls for concise logging.
func toolCallNames(calls []core.LLMToolCall) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

// logModelCall traces an outgoing model request at debug: the component (kind +
// block name + connector), the message/tool counts, the tool choice, and the
// system prompt, so the prompt the model sees is visible with LOG_LEVEL=debug.
func logModelCall(kind, name, connector string, req core.LLMRequest) {
	slog.Debug(kind+" calling model",
		"block", name, "connector", connector,
		"messages", len(req.Messages), "tools", len(req.Tools),
		"toolChoice", req.ToolChoice.Mode, "system", truncForLog(req.System),
		"lastUser", truncForLog(lastUserText(req.Messages)))
}

// lastUserText returns the text of the most recent user message, for logging the
// concrete prompt (with error/body/variables context) the model is given.
func lastUserText(msgs []core.LLMMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == core.LLMRoleUser {
			return msgs[i].Text
		}
	}
	return ""
}

// logModelResp traces a model reply at debug: the stop reason, any free text, and
// the names of the tools it chose to call.
func logModelResp(kind, name string, resp *core.LLMResponse) {
	slog.Debug(kind+" model replied",
		"block", name, "stop", resp.StopReason,
		"text", truncForLog(resp.Text), "toolCalls", toolCallNames(resp.ToolCalls))
}

// routeGuardrailSentinel is the route name the model selects to fall back to the
// guardrail (Default) path when it is not confident in any named route.
const routeGuardrailSentinel = "__guardrail__"

// The shapes an ai-agent can be told to answer in. JSON is the default because an
// agent's answer becomes the next block's body; text is for an agent whose answer
// a person reads, and leaves the format to the block's own prompt.
const (
	answerJSON = "json"
	answerText = "text"
)

// defaultRouterRounds caps how many inspection turns the router runs before it
// gives up and takes the guardrail. Each turn is one model call.
const defaultRouterRounds = 5

// aiRouter is a composite that asks an LLM to pick one of its named routes. The
// model is given read-only tools to inspect the message body and variables, plus
// a select_route tool that emits the decision. The guardrail (Default) flow is
// taken when the model is not confident or never decides.
type aiRouter struct {
	caller    *llmCaller
	system    string
	tools     []core.LLMTool
	routes    map[string]*Flow
	guardrail *Flow
	maxRounds int
	name      string
	connector string
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) aiRouter(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Routes) == 0 {
		return nil, errors.New("ai-router block requires at least one route")
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, errors.New("ai-router block requires a prompt")
	}
	if err := allowSlots(cfg, blockKindAIRouter, "routes", "default", "connector", "prompt", "guardrail"); err != nil {
		return nil, err
	}

	caller, err := resolveLLM(blockKindAIRouter, cfg.Connector, b.deps)
	if err != nil {
		return nil, err
	}

	routes := make(map[string]*Flow, len(cfg.Routes))
	names := make([]string, 0, len(cfg.Routes))
	for i := range cfg.Routes {
		route := cfg.Routes[i]
		if route.Name == "" {
			return nil, fmt.Errorf("ai-router route %d requires a name", i)
		}
		if route.Description == "" {
			return nil, fmt.Errorf("ai-router route %q requires a description", route.Name)
		}
		if _, dup := routes[route.Name]; dup {
			return nil, fmt.Errorf("ai-router route %q is defined more than once", route.Name)
		}
		// A route is addressed by its own name, which is required above.
		flow, flowErr := b.branch(core.MemberBranch(route.Name, i)).
			subFlow(types.FlowConfig{Process: route.Process})
		if flowErr != nil {
			return nil, fmt.Errorf("ai-router route %q: %w", route.Name, flowErr)
		}
		routes[route.Name] = flow
		names = append(names, route.Name)
	}

	block := &aiRouter{
		caller:    caller,
		system:    buildRouterSystem(cfg.Prompt, cfg.Routes, cfg.Guardrail),
		tools:     routerTools(names),
		routes:    routes,
		maxRounds: defaultRouterRounds,
		name:      cfg.Name,
		connector: cfg.Connector,
	}
	if cfg.Default != nil {
		guardrail, defErr := b.branch(core.BranchDefault).subFlow(*cfg.Default)
		if defErr != nil {
			return nil, fmt.Errorf("ai-router default: %w", defErr)
		}
		block.guardrail = guardrail
	}
	return block, nil
}

// Process runs the inspection/decision loop, then dispatches to the chosen route
// or the guardrail.
func (r *aiRouter) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	messages := []core.LLMMessage{{
		Role: core.LLMRoleUser,
		Text: "Decide which route to take for the current message. " +
			"Inspect the body and variables as needed, then call select_route.",
	}}

	for round := 0; round < r.maxRounds; round++ {
		req := core.LLMRequest{
			System:     r.system,
			Messages:   messages,
			Tools:      r.tools,
			ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceAny},
		}
		logModelCall(blockKindAIRouter, r.name, r.connector, req)
		resp, err := r.caller.complete(ctx, msg, req, turnLabel{iteration: round + 1})
		if err != nil {
			return nil, fmt.Errorf("ai-router: %w", err)
		}
		logModelResp(blockKindAIRouter, r.name, resp)
		messages = append(messages, resp.Raw)
		if len(resp.ToolCalls) == 0 {
			break // model produced no decision; fall back to the guardrail
		}

		results := make([]core.LLMToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			if call.Name == "select_route" {
				return r.dispatch(ctx, routeFromCall(call), msg)
			}
			results = append(results, r.inspect(call, msg))
		}
		messages = append(messages, core.LLMMessage{Role: core.LLMRoleTool, ToolResults: results})
	}

	return r.dispatch(ctx, routeGuardrailSentinel, msg)
}

// dispatch runs the named route's flow, or the guardrail flow when the route is
// the guardrail sentinel or is unknown, or passes the message through when there
// is no guardrail (mirroring switch's nil-default behavior).
func (r *aiRouter) dispatch(ctx context.Context, route string, msg *types.Message) (*types.Message, error) {
	if flow, ok := r.routes[route]; ok {
		slog.Info("ai-router selected route", "block", r.name, "connector", r.connector, "route", route)
		return flow.Process(ctx, msg)
	}
	slog.Info("ai-router taking guardrail", "block", r.name, "connector", r.connector, "route", route)
	if r.guardrail != nil {
		return r.guardrail.Process(ctx, msg)
	}
	return msg, nil
}

// inspect serves a read-only inspection tool call against the message.
func (r *aiRouter) inspect(call core.LLMToolCall, msg *types.Message) core.LLMToolResult {
	slog.Debug("ai-router inspection tool", "block", r.name, "tool", call.Name, "input", truncForLog(string(call.Input)))
	switch call.Name {
	case "get_body":
		body, err := msg.BodyJSON()
		if err != nil {
			return errorResult(call, fmt.Sprintf("encode body: %v", err))
		}
		return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: string(body)}
	case "list_variables":
		return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: jsonStringArray(variableNames(msg))}
	case "get_variable":
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return errorResult(call, "invalid arguments")
		}
		value, ok := msg.Variables[args.Name]
		if !ok {
			return errorResult(call, fmt.Sprintf("variable %q is not set", args.Name))
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return errorResult(call, fmt.Sprintf("encode variable: %v", err))
		}
		return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: string(encoded)}
	default:
		return errorResult(call, fmt.Sprintf("unknown tool %q", call.Name))
	}
}

// routeFromCall extracts the chosen route name from a select_route tool call,
// defaulting to the guardrail sentinel when the arguments cannot be read.
func routeFromCall(call core.LLMToolCall) string {
	var args struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal(call.Input, &args); err != nil || args.Route == "" {
		return routeGuardrailSentinel
	}
	return args.Route
}

// defaultRetryAttempts caps how many times ai-retry re-runs its process chain
// after an LLM-driven revision before falling through to the error path.
const defaultRetryAttempts = 3

// reviseToolName is the tool the retry loop forces the model to call.
const reviseToolName = "revise_message"

// aiRetry is a composite that protects a process chain with an LLM-driven retry
// loop. When the chain fails, the model inspects the error (vars.error) and the
// message, revises the message, and the chain is re-run, up to maxAttempts. After
// the attempts are exhausted it falls through to the error chain (if any),
// otherwise the last error propagates.
type aiRetry struct {
	caller      *llmCaller
	system      string
	main        *Flow
	alternative *Flow
	maxAttempts int
	name        string
	connector   string
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) aiRetry(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Process) == 0 {
		return nil, errors.New("ai-retry block requires a process chain")
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, errors.New("ai-retry block requires a prompt")
	}
	if err := allowSlots(cfg, blockKindAIRetry,
		"process", "error", "connector", "prompt", "maxAttempts"); err != nil {
		return nil, err
	}

	caller, err := resolveLLM(blockKindAIRetry, cfg.Connector, b.deps)
	if err != nil {
		return nil, err
	}

	// Both slots are the block's own chains, addressed as `<block>[process]` and
	// `<block>[error]` — the same shape handle-errors has.
	main, err := b.branch(core.BranchProcess).flow(types.FlowConfig{Process: cfg.Process})
	if err != nil {
		return nil, fmt.Errorf("ai-retry process: %w", err)
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultRetryAttempts
	}

	block := &aiRetry{
		caller:      caller,
		system:      buildRetrySystem(cfg.Prompt),
		main:        main,
		maxAttempts: maxAttempts,
		name:        cfg.Name,
		connector:   cfg.Connector,
	}
	if len(cfg.Error) > 0 {
		alternative, altErr := b.branch(core.BranchError).flow(types.FlowConfig{Process: cfg.Error})
		if altErr != nil {
			return nil, fmt.Errorf("ai-retry error: %w", altErr)
		}
		block.alternative = alternative
	}
	return block, nil
}

// Process runs the protected chain; on failure it lets the model revise the
// message and re-runs the chain up to maxAttempts, then falls through to the
// error chain (or returns the last error when none is configured).
func (r *aiRetry) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	out, err := r.main.Process(ctx, msg)
	if err == nil {
		return out, nil
	}
	slog.Info("ai-retry chain failed, starting retry loop",
		"block", r.name, "connector", r.connector, "maxAttempts", r.maxAttempts, "error", err)

	SetErrorVariable(msg, r.name, err)
	convo := []core.LLMMessage{{Role: core.LLMRoleUser, Text: "The step failed.\n" + r.stateText(msg) +
		"\nCall revise_message to retry. Fix the body and/or set any missing variables the failing " +
		"step needs (e.g. a variable referenced as vars.<name> that is absent above) via the " +
		`"variables" field.`}}

	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		call, reviseErr := r.revise(ctx, msg, &convo, attempt+1)
		if reviseErr != nil {
			slog.Warn("ai-retry could not revise", "block", r.name, "attempt", attempt+1, "error", reviseErr)
			break
		}
		if applyErr := applyRevision(msg, call.Input); applyErr != nil {
			slog.Warn("ai-retry could not apply revision", "block", r.name, "attempt", attempt+1, "error", applyErr)
			break
		}
		out, err = r.main.Process(ctx, msg)
		if err == nil {
			slog.Info("ai-retry recovered", "block", r.name, "attempt", attempt+1)
			return out, nil
		}
		slog.Info("ai-retry attempt failed", "block", r.name, "attempt", attempt+1, "error", err)
		SetErrorVariable(msg, r.name, err)
		convo = append(convo, core.LLMMessage{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{{
			ToolCallID: call.ID, Tool: call.Name, IsError: true,
			Content: "That revision did not fix it; the step failed again.\n" + r.stateText(msg) +
				"\nCall revise_message again with a different fix.",
		}}})
	}

	slog.Warn("ai-retry exhausted attempts", "block", r.name, "maxAttempts", r.maxAttempts, "error", err)
	if r.alternative != nil {
		SetErrorVariable(msg, r.name, err)
		recovered, altErr := r.alternative.Process(ctx, msg)
		if altErr != nil {
			return nil, fmt.Errorf("ai-retry error path: %w", altErr)
		}
		return recovered, nil
	}
	return nil, err
}

// stateText renders the current error, body, and variables for the repair model,
// so it can see both what failed and what state it has to work with. vars.error
// must already be set on the message.
func (r *aiRetry) stateText(msg *types.Message) string {
	body, _ := msg.BodyJSON()
	errInfo, _ := json.Marshal(msg.Variables[errorVarName])
	vars, _ := json.Marshal(msg.Variables)
	return fmt.Sprintf("Error: %s\nCurrent message body:\n%s\n"+
		"Current message variables (referenced as vars.<name> in the flow):\n%s", errInfo, body, vars)
}

// revise runs one turn of the repair conversation: it sends the accumulated
// dialog (forcing a revise_message call), appends the model's reply, and returns
// the revise call so the caller can apply it and feed back the outcome. attempt is
// one-based and numbers the model call in the trace record.
func (r *aiRetry) revise(
	ctx context.Context, msg *types.Message, convo *[]core.LLMMessage, attempt int,
) (core.LLMToolCall, error) {
	req := core.LLMRequest{
		System:   r.system,
		Messages: *convo,
		Tools:    reviseTools(),
		// Auto (not forced): forcing the tool makes some providers (Gemini 3.x) skip
		// their reasoning step and emit an empty call. The system prompt already
		// instructs the model to call revise_message, so auto keeps the reasoning.
		ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceAuto},
	}
	logModelCall(blockKindAIRetry, r.name, r.connector, req)
	resp, err := r.caller.complete(ctx, msg, req, turnLabel{iteration: attempt})
	if err != nil {
		return core.LLMToolCall{}, fmt.Errorf("ai-retry: %w", err)
	}
	logModelResp(blockKindAIRetry, r.name, resp)
	*convo = append(*convo, resp.Raw)
	for _, call := range resp.ToolCalls {
		if call.Name == reviseToolName {
			slog.Debug("ai-retry applying revision",
				"block", r.name, "revision", truncForLog(string(call.Input)))
			return call, nil
		}
	}
	return core.LLMToolCall{}, errors.New("ai-retry: model did not produce a revision")
}

// applyRevision sets the message body and merges any variables from a
// revise_message tool call.
func applyRevision(msg *types.Message, raw json.RawMessage) error {
	var rev struct {
		Body      json.RawMessage `json:"body"`
		Variables map[string]any  `json:"variables"`
	}
	if err := json.Unmarshal(raw, &rev); err != nil {
		return fmt.Errorf("ai-retry: invalid revision: %w", err)
	}
	if len(rev.Body) > 0 {
		if err := msg.SetBodyJSON(rev.Body); err != nil {
			return fmt.Errorf("ai-retry: revised body: %w", err)
		}
	}
	for k, v := range rev.Variables {
		msg.Variables.Set(k, v)
	}
	return nil
}

// reviseTools is the single revise_message tool the retry loop forces.
func reviseTools() []core.LLMTool {
	return []core.LLMTool{{
		Name: reviseToolName,
		Description: "Provide a corrected message to retry the failed step. Use this to " +
			"fix the body and/or supply variables the step needs.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"body":{"description":"The corrected message body."},` +
			`"variables":{"type":"object","description":"Message variables to set or override ` +
			`(referenced as vars.<name> in the flow). Set any variable the failing step needs ` +
			`but that is missing."}},` +
			`"required":["body"]}`),
	}}
}

// buildRetrySystem assembles the repair system prompt.
func buildRetrySystem(prompt string) string {
	var b strings.Builder
	b.WriteString("A step in a processing pipeline failed. Inspect the error, the current ")
	b.WriteString("message body, and the current variables, then call revise_message to retry. ")
	b.WriteString("The failing step may reference variables as vars.<name>; if such a variable ")
	b.WriteString("is missing or wrong, set it via the revise_message \"variables\" field. ")
	b.WriteString("Fixing only the body will not help when the step reads from variables.\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

// defaultAgentIterations caps how many tool-calling turns an agent runs before
// falling back to the guardrail. Each turn is one model call.
const defaultAgentIterations = 8

// skillLoadToolName is the implicit tool an ai-agent with skills exposes to load
// a skill's content on demand. It is reserved: no user tool or skill may use it.
const skillLoadToolName = "load_skill"

// agentSkill is one loadable skill: its advertised name/description and the
// template resource whose rendered text load_skill returns.
type agentSkill struct {
	name        string
	description string
	resource    string
}

// aiAgent is a composite that lets an LLM accomplish a task by calling its
// branches as tools, one or more times, in a loop. Each branch is wired to the
// model as a function: the model's arguments become the branch's message body and
// the branch's output body is returned to the model as the tool result. Tool
// branches share the message, so variables they set accumulate across the loop.
// The guardrail (Default) flow is taken when the model refuses or never finishes.
type aiAgent struct {
	caller        *llmCaller
	system        string
	tools         []core.LLMTool
	branches      map[string]*Flow
	guardrail     *Flow
	maxIterations int
	name          string
	connector     string
	// skills are named instruction resources the agent can load on demand via
	// the implicit load_skill tool. skillRegistry renders a skill's template
	// resource against the current message when it is loaded.
	skills        []agentSkill
	skillRegistry *expr.TemplateRegistry
	// input states the opening user turn, or is nil to hand the model the whole
	// input body as a JSON document. See initConversation.
	input *expr.Program
	// Conversation memory (optional). When memoryThreadID is nil, memory is
	// disabled and the agent is stateless across invocations. When set, the agent
	// loads the thread's transcript before its run and saves the accumulated
	// transcript after.
	memoryThreadID   *expr.Program
	memoryCompaction string
	// runScope namespaces this block's claims in the process-wide registry, so two
	// agents whose signalId expressions agree do not hand each other messages.
	runScope string
	// stopWhen is the condition that ends the run this invocation would otherwise
	// have joined — a header, a field, whatever the flow puts it in. Nil when the
	// block offers no way to stop a run.
	stopWhen *expr.Program
	// contextMaxTokens budgets the whole prompt — system instructions, tool
	// schemas and conversation — against what the provider reports it read. It
	// applies whether or not memory is on: a stateless agent can still talk itself
	// past the model's window inside one run.
	contextMaxTokens int
	env              map[string]any
	// events is the observer path, nil when the block declares none. streaming says
	// the block asked to report its output as it arrives, which the builder only
	// accepts when the connector has a streaming half to do it with.
	events    *emitter
	streaming bool
}

// validateAgentConfig rejects an ai-agent block that cannot be built, before any
// of it is. It is separate from the builder so the checks can grow without the
// builder growing with them.
func validateAgentConfig(cfg types.BlockConfig) error {
	if len(cfg.Tools) == 0 {
		return errors.New("ai-agent block requires at least one tool")
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return errors.New("ai-agent block requires a prompt")
	}
	if cfg.Answer != "" && cfg.Answer != answerJSON && cfg.Answer != answerText {
		return fmt.Errorf("ai-agent answer must be %q or %q, got %q",
			answerJSON, answerText, cfg.Answer)
	}
	// Named before allowSlots gets to it, because "must not declare it" is not the
	// useful half of the sentence: the budget now covers the system prompt and the
	// tool schemas as well as the transcript, which is why it was renamed.
	if cfg.MemoryMaxTokens != 0 {
		return errors.New(
			"ai-agent memoryMaxTokens is now contextMaxTokens, and budgets the whole " +
				"prompt (system, tools and conversation) rather than the stored transcript alone")
	}
	return allowSlots(cfg, blockKindAIAgent,
		"tools", "skills", "default", "connector", "prompt", "guardrail", "input", "answer",
		"maxIterations", "memoryThreadId", "contextMaxTokens", "memoryCompaction",
		"stopWhen", "events", "emit", "stream")
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) aiAgent(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if err := validateAgentConfig(cfg); err != nil {
		return nil, err
	}

	caller, err := resolveLLM(blockKindAIAgent, cfg.Connector, b.deps)
	if err != nil {
		return nil, err
	}

	branches, tools, err := b.agentTools(blockKindAIAgent, cfg.Tools)
	if err != nil {
		return nil, err
	}

	skills, err := b.agentSkills(cfg.Skills, tools)
	if err != nil {
		return nil, err
	}
	if len(skills) > 0 {
		tools = append(tools, loadSkillTool(skills))
	}

	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultAgentIterations
	}

	block := &aiAgent{
		caller:        caller,
		system:        buildAgentSystem(cfg.Prompt, cfg.Guardrail, cfg.Answer, skills),
		tools:         tools,
		branches:      branches,
		skills:        skills,
		skillRegistry: expr.NewTemplateRegistry(b.deps.Resources),
		maxIterations: maxIterations,
		name:          cfg.Name,
		connector:     cfg.Connector,
		env:           expr.EnvActivation(b.deps.Env),
	}
	// The optional halves, each of which is a no-op for a block that declares none.
	// They run from a list rather than as four consecutive checks so that adding the
	// next one is a line here rather than another branch in an already long builder.
	for _, configure := range []func(*aiAgent, types.BlockConfig) error{
		b.configureAgentInput,
		b.configureAgentMemory,
		b.configureAgentSignals,
		b.configureAgentEvents,
		b.configureAgentGuardrail,
	} {
		if err := configure(block, cfg); err != nil {
			return nil, err
		}
	}
	return block, nil
}

// configureAgentGuardrail wires the default path an agent falls back to when it
// refuses or runs out of iterations. A block without one errors instead, so the
// failure reaches a recovery path rather than being swallowed.
func (b *builder) configureAgentGuardrail(block *aiAgent, cfg types.BlockConfig) error {
	if cfg.Default == nil {
		return nil
	}
	guardrail, err := b.branch(core.BranchDefault).subFlow(*cfg.Default)
	if err != nil {
		return fmt.Errorf("ai-agent default: %w", err)
	}
	block.guardrail = guardrail
	return nil
}

// configureAgentInput compiles the expression stating the agent's opening user
// turn. A block without one is left to the default framing in initConversation.
func (b *builder) configureAgentInput(block *aiAgent, cfg types.BlockConfig) error {
	if strings.TrimSpace(cfg.Input) == "" {
		return nil
	}
	input, err := expr.CompileMessage(b.deps.Resources, cfg.Input)
	if err != nil {
		return fmt.Errorf("ai-agent input: %w", err)
	}
	block.input = input
	return nil
}

// configureAgentMemory wires the agent's context budget and its optional
// per-thread memory, compiling the thread-id expression and applying defaults. A
// block without a memoryThreadId is left memory-disabled — but still budgeted,
// since a single run can outgrow the model's window on its own.
func (b *builder) configureAgentMemory(block *aiAgent, cfg types.BlockConfig) error {
	// The budget is set first and unconditionally: it bounds the prompt of every
	// run, and an agent with no memory can still talk itself past the model's
	// window over enough tool calls in one loop.
	block.contextMaxTokens = cfg.ContextMaxTokens
	if block.contextMaxTokens <= 0 {
		block.contextMaxTokens = defaultContextMaxTokens
	}
	compaction := cfg.MemoryCompaction
	if compaction == "" {
		compaction = memoryCompactPrune
	}
	if compaction != memoryCompactPrune && compaction != memoryCompactSummarize {
		return fmt.Errorf("ai-agent memoryCompaction must be %q or %q, got %q",
			memoryCompactPrune, memoryCompactSummarize, compaction)
	}
	block.memoryCompaction = compaction

	if cfg.MemoryThreadID == "" {
		return nil
	}
	threadID, err := expr.CompileMessage(b.deps.Resources, cfg.MemoryThreadID)
	if err != nil {
		return err
	}
	block.memoryThreadID = threadID
	return nil
}

// configureAgentEvents wires the observer path and, when the block asks to
// stream, the connector's streaming half.
//
// Every check here is build-time because every failure is a configuration mistake
// with an obvious fix, and none of them should wait for the first request to
// surface — least of all the streaming one, which would otherwise look like a
// provider that has simply gone quiet.
func (b *builder) configureAgentEvents(block *aiAgent, cfg types.BlockConfig) error {
	if cfg.Events == nil {
		switch {
		case cfg.Stream:
			return errors.New("ai-agent stream requires an events path to report to")
		case len(cfg.Emit) > 0:
			return errors.New("ai-agent emit requires an events path to filter")
		}
		return nil
	}

	flow, err := b.branch(core.BranchEvents).subFlow(*cfg.Events)
	if err != nil {
		return fmt.Errorf("ai-agent events: %w", err)
	}
	kinds, err := emitKinds(blockKindAIAgent, agentEventKinds, cfg.Emit)
	if err != nil {
		return err
	}
	block.events = &emitter{flow: flow, kinds: kinds, label: blockKindAIAgent}

	if cfg.Stream {
		if !block.caller.streams() {
			return fmt.Errorf("ai-agent stream: connector %q does not stream", cfg.Connector)
		}
		block.streaming = true
	}
	return nil
}

// agentTools builds the tool branches and their model-facing definitions,
// validating names, descriptions, uniqueness, and schemas. kind labels errors so
// the ai-agent and the mcp-router each report against their own block type.
func (b *builder) agentTools(kind string, configs []types.ToolConfig) (map[string]*Flow, []core.LLMTool, error) {
	branches := make(map[string]*Flow, len(configs))
	tools := make([]core.LLMTool, 0, len(configs))
	for i := range configs {
		tool := configs[i]
		if tool.Name == "" {
			return nil, nil, fmt.Errorf("%s tool %d requires a name", kind, i)
		}
		if tool.Description == "" {
			return nil, nil, fmt.Errorf("%s tool %q requires a description", kind, tool.Name)
		}
		if _, dup := branches[tool.Name]; dup {
			return nil, nil, fmt.Errorf("%s tool %q is defined more than once", kind, tool.Name)
		}
		if err := checkMCPToolFields(kind, tool); err != nil {
			return nil, nil, err
		}
		schema, err := toolInputSchema(tool)
		if err != nil {
			return nil, nil, err
		}
		// A tool branch is addressed by its own name, which is required above. This
		// serves the ai-agent and the mcp-router alike: b is already the builder for
		// whichever of the two blocks is being built.
		flow, err := b.branch(core.MemberBranch(tool.Name, i)).subFlow(types.FlowConfig{Process: tool.Process})
		if err != nil {
			return nil, nil, fmt.Errorf("ai-agent tool %q: %w", tool.Name, err)
		}
		branches[tool.Name] = flow
		tools = append(tools, core.LLMTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	return branches, tools, nil
}

// checkMCPToolFields rejects the MCP-only tool metadata on any block but the
// mcp-router.
//
// It is a hard error rather than a shrug because there is nowhere for the value
// to go: an LLM tool call has no protocol carrying a title, an annotation or an
// output schema, so accepting one on an ai-agent would be configuration that
// looks like it does something and does not.
func checkMCPToolFields(kind string, tool types.ToolConfig) error {
	if kind == blockKindMCPRouter {
		return nil
	}
	switch {
	case tool.Title != "":
		return fmt.Errorf("%s tool %q: title is mcp-router metadata and has no effect here", kind, tool.Name)
	case tool.Annotations != nil:
		return fmt.Errorf(
			"%s tool %q: annotations are mcp-router metadata and have no effect here", kind, tool.Name)
	case tool.OutputSchema != "":
		return fmt.Errorf(
			"%s tool %q: outputSchema is mcp-router metadata and has no effect here", kind, tool.Name)
	}
	return nil
}

// agentSkills builds the agent's skill set, validating names, descriptions, and
// resources, and rejecting duplicates, the reserved load_skill name, and
// collisions with a tool of the same name. It returns nil when no skills are
// configured (the agent then exposes no load_skill tool).
func (b *builder) agentSkills(configs []types.SkillConfig, tools []core.LLMTool) ([]agentSkill, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	toolNames := make(map[string]bool, len(tools))
	for _, t := range tools {
		toolNames[t.Name] = true
	}
	if toolNames[skillLoadToolName] {
		return nil, fmt.Errorf("ai-agent tool %q uses the reserved name for the skill loader", skillLoadToolName)
	}
	skills := make([]agentSkill, 0, len(configs))
	seen := make(map[string]bool, len(configs))
	for i := range configs {
		s := configs[i]
		switch {
		case s.Name == "":
			return nil, fmt.Errorf("ai-agent skill %d requires a name", i)
		case s.Name == skillLoadToolName:
			return nil, fmt.Errorf("ai-agent skill %q uses the reserved name %q", s.Name, skillLoadToolName)
		case s.Description == "":
			return nil, fmt.Errorf("ai-agent skill %q requires a description", s.Name)
		case s.Resource == "":
			return nil, fmt.Errorf("ai-agent skill %q requires a resource", s.Name)
		case seen[s.Name]:
			return nil, fmt.Errorf("ai-agent skill %q is defined more than once", s.Name)
		case toolNames[s.Name]:
			return nil, fmt.Errorf("ai-agent skill %q collides with a tool of the same name", s.Name)
		}
		seen[s.Name] = true
		skills = append(skills, agentSkill{name: s.Name, description: s.Description, resource: s.Resource})
	}
	return skills, nil
}

// loadSkillTool builds the implicit load_skill tool, restricting its name
// argument to the configured skill names.
func loadSkillTool(skills []agentSkill) core.LLMTool {
	names := make([]string, len(skills))
	for i, s := range skills {
		names[i] = s.name
	}
	enumJSON, _ := json.Marshal(names)
	return core.LLMTool{
		Name: skillLoadToolName,
		Description: "Load a skill's full instructions by name before acting on its area. " +
			"The available skills are listed in the system prompt.",
		InputSchema: json.RawMessage(fmt.Sprintf(
			`{"type":"object","properties":{"name":{"type":"string","enum":%s,`+
				`"description":"The skill to load."}},"required":["name"]}`, enumJSON)),
	}
}

// Process runs the agentic loop: the model calls tools (branches) until it
// finishes with a final result or the iteration cap is hit. Tool branches run on
// the shared message so variables accumulate; the final assistant text is folded
// into the body as the result.
func (a *aiAgent) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	// The run gets its own cancellable context so a stop can abandon a model call
	// already in flight rather than pay for the rest of it. It is derived here
	// rather than asked of the flow, which detaches its work from the context that
	// scheduled it on purpose.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Which conversation is this, and is this invocation meant to reach a run
	// rather than be one? Both of the latter answers end the flow here, and
	// neither calls a model.
	claim, taken, err := a.joinOrClaim(msg, cancel)
	if err != nil {
		return nil, err
	}
	if taken != nil {
		return taken, nil
	}
	defer liveRuns.release(claim.key, claim.run)
	defer claim.run.close()
	run, threadID := claim.run, claim.threadID

	messages, meter, err := a.initConversation(ctx, msg, threadID)
	if err != nil {
		return nil, err
	}
	// Everything that has to outlive a stop runs on this one: by the time the run
	// is saving its memory, runCtx is already cancelled.
	//
	// Bounded, though. WithoutCancel alone would leave a hung store able to hold a
	// flow worker for the life of the process, and the deadline has to be generous
	// rather than snappy because saving may include a summarize — a real model call
	// on the way out.
	saveCtx, endSave := context.WithTimeout(context.WithoutCancel(ctx), memorySaveTimeout)
	defer endSave()

	current := msg
	for iter := 0; iter < a.maxIterations; iter++ {
		messages = a.injectPending(runCtx, current, messages, iter, run)
		messages = a.fitContext(runCtx, current, messages, iter, meter)
		// What was sent, paired below with what the provider says it read. The two
		// together are what let the meter separate the run's fixed overhead from the
		// conversation that varies.
		sent := estimateTokens(messages)
		resp, callErr := a.callModel(runCtx, iter, current, messages)
		if callErr != nil {
			return a.callFailed(saveCtx, threadID, stoppedTranscript(messages, resp),
				current, iter, meter, run, callErr)
		}
		meter.observeResponse(sent, resp)
		messages = append(messages, resp.Raw)

		if resp.StopReason == core.LLMStopRefusal {
			a.persistMemory(saveCtx, current, threadID, messages, meter, a.memoryCompaction)
			return a.fallback(runCtx, current, iter, "model refused")
		}
		if len(resp.ToolCalls) == 0 {
			out := a.tryFinish(runCtx, saveCtx, threadID, current, messages, meter, run, resp, iter)
			if out == nil {
				continue // something arrived mid-answer; the run owes it a turn
			}
			return out, nil
		}

		results, stopped := a.runTools(runCtx, iter, resp.ToolCalls, &current)
		messages = append(messages, core.LLMMessage{Role: core.LLMRoleTool, ToolResults: results})

		// Halt rather than start another iteration, which would call the model again
		// and overwrite the body with the next tool call's arguments. A tool branch
		// runs on the shared message so its flag is already there; the events path
		// runs on its own, so its stop has to be carried over.
		switch {
		case run.stopRequested():
			return a.haltOnSignal(saveCtx, threadID, messages, current, iter, meter)
		case stopped || current.StopRequested():
			return a.halt(saveCtx, threadID, messages, current, iter, "a tool branch stopped the run", meter)
		}
	}

	a.persistMemory(saveCtx, current, threadID, messages, meter, a.memoryCompaction)
	return a.fallback(ctx, current, a.maxIterations-1, "exceeded max iterations")
}

// tryFinish ends the run with its answer, or returns nil to say it may not end
// yet.
//
// A message that arrived while this answer was being produced was accepted, and
// the invocation that handed it over has already stopped its own flow on the
// strength of that — so returning here would drop it. The run takes another turn
// instead, which is also what makes a follow-up typed mid-answer behave the way
// it does in any chat.
func (a *aiAgent) tryFinish(
	runCtx, saveCtx context.Context, threadID string, current *types.Message,
	messages []core.LLMMessage, meter *contextMeter, run *agentRun,
	resp *core.LLMResponse, iter int,
) *types.Message {
	if !run.finish() {
		return nil
	}
	slog.Info("ai-agent finished", "block", a.name, "iterations", iter+1)
	out := foldResult(current, resp.Text)
	a.report(runCtx, out, iter, eventDone, map[string]any{fieldText: resp.Text})
	a.persistMemory(saveCtx, current, threadID, messages, meter, a.memoryCompaction)
	return out
}

// callFailed decides what a failed model call means: a run someone stopped, a
// run the events path gave up on, or an actual failure.
//
// A stop is read from the run rather than from the error. Cancelling the run's
// context is what abandons the call, and each provider client makes its own error
// out of that — which error it is is not this loop's business to recognize.
func (a *aiAgent) callFailed(
	ctx context.Context, threadID string, messages []core.LLMMessage,
	current *types.Message, iter int, meter *contextMeter, run *agentRun, callErr error,
) (*types.Message, error) {
	switch {
	case run.stopRequested():
		return a.haltOnSignal(ctx, threadID, messages, current, iter, meter)
	case errors.Is(callErr, errEventStop):
		return a.halt(ctx, threadID, messages, current, iter, "the events path stopped the run", meter)
	}
	return nil, fmt.Errorf("ai-agent: %w", callErr)
}

// haltOnSignal ends the run because someone asked it to, reporting the stop
// before it goes.
func (a *aiAgent) haltOnSignal(
	ctx context.Context, threadID string, messages []core.LLMMessage,
	current *types.Message, iter int, meter *contextMeter,
) (*types.Message, error) {
	a.report(ctx, current, iter, eventSignal, map[string]any{fieldSignal: signalStop})
	return a.halt(ctx, threadID, messages, current, iter, "a stop signal ended the run", meter)
}

// callModel runs one model turn, reporting its boundaries and — when the block
// streams — its output as it arrives.
func (a *aiAgent) callModel(
	ctx context.Context, iter int, current *types.Message, messages []core.LLMMessage,
) (*core.LLMResponse, error) {
	req := core.LLMRequest{
		System:     a.system,
		Messages:   messages,
		Tools:      a.tools,
		ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceAuto},
	}
	logModelCall(blockKindAIAgent, a.name, a.connector, req)
	if a.report(ctx, current, iter, eventTurnStart, nil) {
		return nil, errEventStop
	}

	resp, err := a.completeTurn(ctx, req, current, iter)
	if err != nil {
		if !errors.Is(err, errEventStop) {
			a.report(ctx, current, iter, eventError, map[string]any{"error": err.Error()})
		}
		return nil, err
	}
	logModelResp(blockKindAIAgent, a.name, resp)
	if a.report(ctx, current, iter, eventTurnEnd, turnEndFields(resp, a.contextMaxTokens)) {
		// The response goes back with the stop: the turn finished and was billed, so
		// the caller decides whether it belongs in memory. The earlier stops have no
		// turn to hand over.
		return resp, errEventStop
	}
	return resp, nil
}

// stoppedTranscript is what to persist when the events path stopped the run. A
// stop at turn_end arrives with a finished turn — the model produced it and it was
// paid for — so recording it keeps memory matching what actually happened.
//
// Unless that turn asked for tools. Its results were never produced and now never
// will be, and a tool call without its result leaves the thread malformed: the
// providers require the results to follow the turn that asked for them, so the
// next run would replay a conversation they reject. A replayable transcript is
// worth more than a record of work that was abandoned.
func stoppedTranscript(messages []core.LLMMessage, resp *core.LLMResponse) []core.LLMMessage {
	if resp == nil || len(resp.ToolCalls) > 0 {
		return messages
	}
	return append(messages, resp.Raw)
}

// completeTurn calls the model, streaming when the block asks for it. Both paths
// return the same response, so streaming changes when the events path hears about
// the turn and never what the agent does with it.
func (a *aiAgent) completeTurn(
	ctx context.Context, req core.LLMRequest, current *types.Message, iter int,
) (*core.LLMResponse, error) {
	// One-based, matching the iteration the events path reports, so the two
	// observers of the same turn agree on what to call it.
	label := turnLabel{iteration: iter + 1}
	if !a.streaming {
		return a.caller.complete(ctx, current, req, label)
	}
	return a.caller.stream(ctx, current, req, func(ev core.LLMStreamEvent) error {
		kind, known := deltaKinds[ev.Kind]
		// Check before building anything: on a long answer this runs once per token,
		// and an excluded kind should cost a map lookup and nothing else.
		if !known || !a.events.wants(kind) {
			return nil
		}
		if a.report(ctx, current, iter, kind, deltaFields(ev)) {
			return errEventStop
		}
		return nil
	}, label)
}

// runTools dispatches one turn's tool calls, reporting each call and its result,
// and says whether the events path asked to stop.
func (a *aiAgent) runTools(
	ctx context.Context, iter int, calls []core.LLMToolCall, current **types.Message,
) ([]core.LLMToolResult, bool) {
	results := make([]core.LLMToolResult, 0, len(calls))
	stopped := false
	for _, call := range calls {
		stopped = a.report(ctx, *current, iter, eventToolCall, callFields(call)) || stopped
		var res core.LLMToolResult
		res, *current = a.runTool(ctx, call, *current)
		results = append(results, res)
		stopped = a.report(ctx, *current, iter, eventToolResult, resultFields(call, res)) || stopped
	}
	return results, stopped
}

// report sends one event, stamping the iteration every event carries, and says
// whether the events path asked to stop.
func (a *aiAgent) report(
	ctx context.Context, msg *types.Message, iter int, kind string, fields map[string]any,
) bool {
	if !a.events.wants(kind) {
		return false
	}
	stamped := make(map[string]any, len(fields)+1)
	for name, v := range fields {
		stamped[name] = v
	}
	stamped["iteration"] = iter + 1
	return a.events.send(ctx, msg, kind, stamped)
}

// halt ends the run early with the message as it stands, making sure the stop
// flag is on it — the events path sets it on its own message, not this one.
func (a *aiAgent) halt(
	ctx context.Context, threadID string, messages []core.LLMMessage,
	current *types.Message, iter int, reason string, meter *contextMeter,
) (*types.Message, error) {
	slog.Info("ai-agent stopped", "block", a.name, "iterations", iter+1, "reason", reason)
	current.RequestStop()
	// Pruned rather than summarized, whatever the block configured. Every path
	// through here is a run ending early because nobody is waiting for it any
	// more — a closed connection, a tool branch bailing out, a person pressing
	// stop — and summarizing costs a real model call. Buying one to tidy up work
	// that was just abandoned is the wrong instinct.
	a.persistMemory(ctx, current, threadID, messages, meter, memoryCompactPrune)
	return current, nil
}

// initConversation seeds the LLM message list with the opening user turn,
// prepending the thread's prior transcript when memory is enabled. It returns the
// resolved thread id (empty when memory is disabled) and a context meter carrying
// whatever the last run measured for that transcript.
func (a *aiAgent) initConversation(
	ctx context.Context, msg *types.Message, threadID string,
) (messages []core.LLMMessage, meter *contextMeter, err error) {
	opening, err := a.openingTurn(msg)
	if err != nil {
		return nil, nil, err
	}
	stored, err := a.loadHistory(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	messages = make([]core.LLMMessage, 0, len(stored.Messages)+1)
	messages = append(messages, stored.Messages...)
	messages = append(messages, core.LLMMessage{Role: core.LLMRoleUser, Text: opening})

	// The stored size was measured by the run that saved it, so seeding from it
	// gives the first turn back a rate learned from real tokens. It is a rate, not
	// an answer: the first measured turn of this run replaces it.
	meter = newContextMeter()
	meter.seed(estimateTokens(stored.Messages), stored.Tokens)
	return messages, meter, nil
}

// openingTurn is the text of the agent's first user message.
//
// The default hands over the whole body as a JSON document, which is what an agent
// transforming a payload needs — and, for the same reason, the wrong thing to give
// a conversational one. A model that answers without reasoning first tends to
// reply in the shape it was handed, so an agent given a body answers with a body:
// `{"message":"hello chat"}` came back from one provider for an input whose
// message was "hello chat". Nothing downstream can undo that, because the reply is
// streamed a token at a time to whoever is reading it.
//
// So an agent may state its own opening turn instead, and a conversational one
// should: ask the question and the model answers the question. The expression
// decides what of the body is the question and what is context, which is a choice
// only the flow can make.
func (a *aiAgent) openingTurn(msg *types.Message) (string, error) {
	if a.input == nil {
		body, err := msg.BodyJSON()
		if err != nil {
			return "", fmt.Errorf("ai-agent: encode input body: %w", err)
		}
		return "Accomplish the task for this input message body:\n" + string(body), nil
	}
	text, err := a.input.EvalString(expr.MessageActivation(msg, a.env))
	if err != nil {
		return "", fmt.Errorf("ai-agent input: %w", err)
	}
	return text, nil
}

// loadHistory resolves the memory thread id and loads its stored state when
// memory is enabled. It returns the resolved thread id (empty when disabled) and
// the stored envelope (zero when disabled or the thread is new).
func (a *aiAgent) loadHistory(ctx context.Context, threadID string) (memoryEnvelope, error) {
	if threadID == "" {
		return memoryEnvelope{}, nil
	}
	stored, err := loadMemory(ctx, threadID)
	if err != nil {
		return memoryEnvelope{}, fmt.Errorf("ai-agent load memory: %w", err)
	}
	return stored, nil
}

// resolveThread evaluates the conversation this message belongs to, or returns
// empty for a stateless agent.
//
// It is the one identity an agent has, and it does two jobs: it is the key its
// transcript is stored under, and it is what a run is claimed on so a second
// message joins it rather than starting a rival. Those are the same fact — two
// runs on one thread would overwrite each other's memory — so they are the same
// expression.
func (a *aiAgent) resolveThread(msg *types.Message) (string, error) {
	if a.memoryThreadID == nil {
		return "", nil
	}
	threadID, err := a.memoryThreadID.EvalString(expr.MessageActivation(msg, a.env))
	if err != nil {
		return "", fmt.Errorf("ai-agent memory threadId: %w", err)
	}
	if threadID == "" {
		slog.Warn("ai-agent memoryThreadId resolved to nothing; the run is stateless and unreachable",
			"block", a.name)
	}
	return threadID, nil
}

// persistMemory saves the accumulated transcript for the thread (best-effort,
// compacted to the budget). It is a no-op when memory is disabled; a save failure
// is logged rather than failing the flow.
func (a *aiAgent) persistMemory(
	ctx context.Context, msg *types.Message, threadID string,
	transcript []core.LLMMessage, meter *contextMeter, strategy string,
) {
	if a.memoryThreadID == nil {
		return
	}
	compacted := compactMemory(ctx, a.caller, msg, transcript, a.contextMaxTokens, strategy, meter)
	// The size stored is the conversation's own, without this run's overhead: the
	// next run's system prompt and tool set are not necessarily this one's.
	env := memoryEnvelope{Messages: compacted, Tokens: meter.sizeOfMessages(compacted)}
	if err := saveMemory(ctx, threadID, env); err != nil {
		slog.Warn("ai-agent failed to save memory", "block", a.name, "thread", threadID, "error", err)
	}
}

// runTool dispatches one tool call to its branch: the call arguments become the
// branch's body and the branch's output body is the tool result. A branch error
// or a dropped message becomes an error result fed back to the model rather than
// aborting the agent. It returns the (possibly updated) current message so shared
// state carries forward.
func (a *aiAgent) runTool(
	ctx context.Context, call core.LLMToolCall, current *types.Message,
) (core.LLMToolResult, *types.Message) {
	slog.Info("ai-agent tool call", "block", a.name, "tool", call.Name)
	slog.Debug("ai-agent tool input", "block", a.name, "tool", call.Name, "input", truncForLog(string(call.Input)))
	if call.Name == skillLoadToolName {
		return a.loadSkill(ctx, call, current), current
	}
	flow, ok := a.branches[call.Name]
	if !ok {
		return errorResult(call, fmt.Sprintf("unknown tool %q", call.Name)), current
	}
	content, out, errMsg := dispatchToolBranch(ctx, flow, call.Input, current)
	if errMsg != "" {
		return errorResult(call, errMsg), out
	}
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: content}, out
}

// dispatchToolBranch runs a tool's flow branch on the shared message: the JSON
// args become the message body and the branch's output body is returned as a JSON
// string. It returns the (possibly updated) message so shared state carries
// forward, and a non-empty errMsg describing any failure to surface. It is the
// single seam both the ai-agent (runTool) and the mcp-router use to route a tool
// call to a flow.
func dispatchToolBranch(
	ctx context.Context, flow *Flow, args json.RawMessage, current *types.Message,
) (content string, out *types.Message, errMsg string) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := current.SetBodyJSON(args); err != nil {
		return "", current, fmt.Sprintf("invalid arguments: %v", err)
	}
	res, err := flow.Process(ctx, current)
	if err != nil {
		return "", current, err.Error()
	}
	if res == nil {
		return "", current, "tool produced no result"
	}
	encoded, err := res.BodyJSON()
	if err != nil {
		return "", res, fmt.Sprintf("encode result: %v", err)
	}
	return string(encoded), res, ""
}

// loadSkill serves a load_skill tool call: it renders the named skill's template
// resource against the current message and returns the text as the tool result.
// An unknown skill or a render failure becomes an error result fed back to the
// model rather than aborting the agent.
func (a *aiAgent) loadSkill(ctx context.Context, call core.LLMToolCall, msg *types.Message) core.LLMToolResult {
	var args struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(call.Input, &args); err != nil || args.Name == "" {
		return errorResult(call, "invalid arguments: name is required")
	}
	skill, ok := a.findSkill(args.Name)
	if !ok {
		return errorResult(call, fmt.Sprintf("unknown skill %q", args.Name))
	}
	tpl, err := a.skillRegistry.Get(ctx, skill.resource)
	if err != nil {
		return errorResult(call, fmt.Sprintf("load skill %q: %v", skill.name, err))
	}
	rendered, err := tpl.Render(expr.MessageActivation(msg, a.env))
	if err != nil {
		return errorResult(call, fmt.Sprintf("render skill %q: %v", skill.name, err))
	}
	slog.Info("ai-agent loaded skill", "block", a.name, "skill", skill.name)
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: rendered}
}

// findSkill returns the configured skill with the given name.
func (a *aiAgent) findSkill(name string) (agentSkill, bool) {
	for _, s := range a.skills {
		if s.name == name {
			return s, true
		}
	}
	return agentSkill{}, false
}

// fallback runs the guardrail flow, or errors when none is configured so the
// failure propagates to a recovery path.
func (a *aiAgent) fallback(
	ctx context.Context, msg *types.Message, iter int, reason string,
) (*types.Message, error) {
	slog.Warn("ai-agent taking guardrail", "block", a.name, "connector", a.connector, "reason", reason)
	a.report(ctx, msg, iter, eventGuardrail, map[string]any{"reason": reason})
	if a.guardrail != nil {
		return a.guardrail.Process(ctx, msg)
	}
	return nil, fmt.Errorf("ai-agent: %s and no guardrail configured", reason)
}

// foldResult sets the message body to the model's final answer, parsing it as
// JSON when possible and otherwise storing it as text. An empty answer leaves the
// body untouched (the last tool's effect stands).
func foldResult(msg *types.Message, text string) *types.Message {
	trimmed := stripJSONFence(text)
	if trimmed == "" {
		return msg
	}
	var decoded any
	if json.Unmarshal([]byte(trimmed), &decoded) == nil {
		msg.SetBody(decoded)
	} else {
		msg.SetBody(text)
	}
	return msg
}

// toolInputSchema returns the tool's JSON Schema as raw JSON, defaulting to an
// empty object schema and validating any supplied schema is well-formed JSON.
func toolInputSchema(tool types.ToolConfig) (json.RawMessage, error) {
	schema := strings.TrimSpace(tool.InputSchema)
	if schema == "" {
		return json.RawMessage(`{"type":"object"}`), nil
	}
	if !json.Valid([]byte(schema)) {
		return nil, fmt.Errorf("ai-agent tool %q: inputSchema is not valid JSON", tool.Name)
	}
	return json.RawMessage(schema), nil
}

// buildAgentSystem assembles the agent's task system prompt, listing any skills
// so the model knows what it can load via load_skill.
func buildAgentSystem(prompt, guardrail, answer string, skills []agentSkill) string {
	var b strings.Builder
	b.WriteString("You are an agent that accomplishes a task by calling the available tools. ")
	b.WriteString("Call tools as needed; when the task is complete, respond with the final result")
	// The one sentence the answer setting decides.
	//
	// Demanding JSON is the default because an agent's answer becomes the next
	// block's body, and a flow reading body.tier needs the model to have been asked
	// for an object rather than left to choose. What it cannot be is unconditional,
	// which it was: it is written ahead of the block's own prompt, so an agent asked
	// by its author for Markdown prose was asked here, first, for JSON and nothing
	// else. No flow author can win that argument — Anthropic followed the later,
	// more specific instruction and answered the sentence, while OpenAI and Gemini
	// followed this one and answered `{"message":"…"}`. Streaming makes it
	// permanent, since the JSON reaches the reader a token at a time.
	//
	// Saying nothing is therefore a real choice rather than an absence, and both
	// shapes work downstream either way: foldResult parses an answer that is JSON
	// and keeps anything else as text.
	if answer != answerText {
		b.WriteString(" as JSON only (no prose, no markdown code fences), ")
		// The concession is not the fix — `answer: text` is — but it is what an agent
		// gets whose author never found the setting, and it costs one clause. It names
		// the instructions rather than "the user" because that is a place the model can
		// actually look: they are the next thing in this prompt.
		b.WriteString("unless the instructions below call for a different format")
	}
	b.WriteString(".\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	if len(skills) > 0 {
		b.WriteString("\n\nYou have these skills available. Call load_skill(name) to read a skill's ")
		b.WriteString("full instructions before acting on its area:\n")
		for _, s := range skills {
			fmt.Fprintf(&b, "- %s: %s\n", s.name, s.description)
		}
	}
	if strings.TrimSpace(guardrail) != "" {
		b.WriteString("\n\nGuardrail: ")
		b.WriteString(strings.TrimSpace(guardrail))
	}
	return b.String()
}

// stripJSONFence removes a surrounding ```json ... ``` (or bare ``` ... ```)
// markdown fence if the model wrapped its answer in one.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimPrefix(s, "JSON")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// buildRouterSystem assembles the routing system prompt: the user's instruction,
// the route catalog, and the guardrail guidance.
func buildRouterSystem(prompt string, routes []types.RouteConfig, guardrail string) string {
	var b strings.Builder
	b.WriteString("You are a router. Choose exactly one route for the incoming message by ")
	b.WriteString("calling the select_route tool. Use the inspection tools (get_body, ")
	b.WriteString("get_variable, list_variables) to gather what you need before deciding.\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	b.WriteString("\n\nAvailable routes:\n")
	for _, route := range routes {
		fmt.Fprintf(&b, "- %s: %s\n", route.Name, route.Description)
	}
	b.WriteString("\nIf you are not confident in any route, select ")
	b.WriteString(routeGuardrailSentinel)
	b.WriteString(" (the guardrail).")
	if strings.TrimSpace(guardrail) != "" {
		b.WriteString("\nGuardrail guidance: ")
		b.WriteString(strings.TrimSpace(guardrail))
	}
	return b.String()
}

// routerTools builds the inspection tools plus the select_route decision tool.
func routerTools(routeNames []string) []core.LLMTool {
	enum := make([]string, 0, len(routeNames)+1)
	enum = append(enum, routeNames...)
	enum = append(enum, routeGuardrailSentinel)

	return []core.LLMTool{
		{
			Name:        "get_body",
			Description: "Return the current message body as JSON.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "list_variables",
			Description: "Return the names of the variables set on the message.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "get_variable",
			Description: "Return the value of a named message variable as JSON.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		},
		{
			Name:        "select_route",
			Description: "Choose the route to run for this message.",
			InputSchema: selectRouteSchema(enum),
		},
	}
}

// selectRouteSchema builds the JSON Schema for the select_route tool, restricting
// the route to the known names plus the guardrail sentinel.
func selectRouteSchema(enum []string) json.RawMessage {
	enumJSON, _ := json.Marshal(enum)
	return json.RawMessage(fmt.Sprintf(
		`{"type":"object","properties":{`+
			`"route":{"type":"string","enum":%s,"description":"The route to run."},`+
			`"reason":{"type":"string","description":"A brief justification for the choice."}},`+
			`"required":["route"]}`,
		enumJSON))
}

// errorResult builds a tool result marked as an error so the model can react. It
// takes the call rather than its id so the result carries the tool's name too,
// which a provider addressing its function responses by name needs.
func errorResult(call core.LLMToolCall, message string) core.LLMToolResult {
	return core.LLMToolResult{ToolCallID: call.ID, Tool: call.Name, Content: message, IsError: true}
}

// variableNames returns the message's variable names, sorted for determinism.
func variableNames(msg *types.Message) []string {
	names := make([]string, 0, len(msg.Variables))
	for name := range msg.Variables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// jsonStringArray marshals a string slice to a JSON array string.
func jsonStringArray(values []string) string {
	raw, _ := json.Marshal(values)
	return string(raw)
}

// resolveLLM binds an AI block to its LLM provider connector by name, asserting
// the shared core.LLMClient interface so any provider satisfies it. The kind
// labels the error.
//
// It returns the caller rather than the client so a block cannot reach a provider
// without the model-call record coming with it — see aicaller.go.
func resolveLLM(kind, name string, deps core.BlockDeps) (*llmCaller, error) {
	if name == "" {
		return nil, fmt.Errorf("%s block requires a connector", kind)
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("%s block: connector %q requested but no connectors are available", kind, name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("%s block: connector %q is not configured", kind, name)
	}
	client, ok := connector.(core.LLMClient)
	if !ok {
		return nil, fmt.Errorf("%s block: connector %q is not an LLM provider", kind, name)
	}
	// The streaming half is optional, so it is asserted rather than required, and
	// asserted here so "does this provider stream" is answered in the same place
	// "is this a provider at all" is.
	streamer, _ := connector.(core.LLMStreamClient)
	return &llmCaller{
		client:   client,
		streamer: streamer,
		who:      newIdentity(kind, name, providerOf(connector), deps),
	}, nil
}
