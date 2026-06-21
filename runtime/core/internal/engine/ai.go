package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// routeGuardrailSentinel is the route name the model selects to fall back to the
// guardrail (Default) path when it is not confident in any named route.
const routeGuardrailSentinel = "__guardrail__"

// defaultRouterRounds caps how many inspection turns the router runs before it
// gives up and takes the guardrail. Each turn is one model call.
const defaultRouterRounds = 5

// aiRouter is a composite that asks an LLM to pick one of its named routes. The
// model is given read-only tools to inspect the message body and variables, plus
// a select_route tool that emits the decision. The guardrail (Default) flow is
// taken when the model is not confident or never decides.
type aiRouter struct {
	client    core.LLMClient
	system    string
	tools     []core.LLMTool
	routes    map[string]*Flow
	guardrail *Flow
	maxRounds int
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

	client, err := resolveLLM(blockKindAIRouter, cfg.Connector, b.deps)
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
		flow, flowErr := b.subFlow(route.Flow)
		if flowErr != nil {
			return nil, fmt.Errorf("ai-router route %q: %w", route.Name, flowErr)
		}
		routes[route.Name] = flow
		names = append(names, route.Name)
	}

	block := &aiRouter{
		client:    client,
		system:    buildRouterSystem(cfg.Prompt, cfg.Routes, cfg.Guardrail),
		tools:     routerTools(names),
		routes:    routes,
		maxRounds: defaultRouterRounds,
	}
	if cfg.Default != nil {
		guardrail, defErr := b.subFlow(*cfg.Default)
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
		resp, err := r.client.Complete(ctx, core.LLMRequest{
			System:     r.system,
			Messages:   messages,
			Tools:      r.tools,
			ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceAny},
		})
		if err != nil {
			return nil, fmt.Errorf("ai-router: %w", err)
		}
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
		return flow.Process(ctx, msg)
	}
	if r.guardrail != nil {
		return r.guardrail.Process(ctx, msg)
	}
	return msg, nil
}

// inspect serves a read-only inspection tool call against the message.
func (r *aiRouter) inspect(call core.LLMToolCall, msg *types.Message) core.LLMToolResult {
	switch call.Name {
	case "get_body":
		body, err := msg.BodyJSON()
		if err != nil {
			return errorResult(call.ID, fmt.Sprintf("encode body: %v", err))
		}
		return core.LLMToolResult{ToolCallID: call.ID, Content: string(body)}
	case "list_variables":
		return core.LLMToolResult{ToolCallID: call.ID, Content: jsonStringArray(variableNames(msg))}
	case "get_variable":
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(call.Input, &args); err != nil {
			return errorResult(call.ID, "invalid arguments")
		}
		value, ok := msg.Variables[args.Name]
		if !ok {
			return errorResult(call.ID, fmt.Sprintf("variable %q is not set", args.Name))
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return errorResult(call.ID, fmt.Sprintf("encode variable: %v", err))
		}
		return core.LLMToolResult{ToolCallID: call.ID, Content: string(encoded)}
	default:
		return errorResult(call.ID, fmt.Sprintf("unknown tool %q", call.Name))
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
	client      core.LLMClient
	system      string
	main        *Flow
	alternative *Flow
	maxAttempts int
	name        string
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

	client, err := resolveLLM(blockKindAIRetry, cfg.Connector, b.deps)
	if err != nil {
		return nil, err
	}

	main, err := b.flow(types.FlowConfig{Process: cfg.Process})
	if err != nil {
		return nil, fmt.Errorf("ai-retry process: %w", err)
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultRetryAttempts
	}

	block := &aiRetry{
		client:      client,
		system:      buildRetrySystem(cfg.Prompt),
		main:        main,
		maxAttempts: maxAttempts,
		name:        cfg.Name,
	}
	if len(cfg.Error) > 0 {
		alternative, altErr := b.flow(types.FlowConfig{Process: cfg.Error})
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

	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		SetErrorVariable(msg, r.name, err)
		if reviseErr := r.revise(ctx, msg); reviseErr != nil {
			break // cannot get a usable revision; stop retrying
		}
		out, err = r.main.Process(ctx, msg)
		if err == nil {
			return out, nil
		}
	}

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

// revise asks the model for a corrected message and applies it. vars.error must
// already be set on the message.
func (r *aiRetry) revise(ctx context.Context, msg *types.Message) error {
	body, err := msg.BodyJSON()
	if err != nil {
		return fmt.Errorf("ai-retry: encode body: %w", err)
	}
	errInfo, _ := json.Marshal(msg.Variables[errorVarName])

	resp, err := r.client.Complete(ctx, core.LLMRequest{
		System: r.system,
		Messages: []core.LLMMessage{{
			Role: core.LLMRoleUser,
			Text: fmt.Sprintf("The step failed.\nError: %s\nCurrent message body:\n%s\n"+
				"Call revise_message with a corrected message to retry.", errInfo, body),
		}},
		Tools:      reviseTools(),
		ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: reviseToolName},
	})
	if err != nil {
		return fmt.Errorf("ai-retry: %w", err)
	}
	for _, call := range resp.ToolCalls {
		if call.Name == reviseToolName {
			return applyRevision(msg, call.Input)
		}
	}
	return errors.New("ai-retry: model did not produce a revision")
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
		Name:        reviseToolName,
		Description: "Provide a corrected message to retry the failed step.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{` +
			`"body":{"description":"The corrected message body."},` +
			`"variables":{"type":"object","description":"Variables to set or override."}},` +
			`"required":["body"]}`),
	}}
}

// buildRetrySystem assembles the repair system prompt.
func buildRetrySystem(prompt string) string {
	var b strings.Builder
	b.WriteString("A step in a processing pipeline failed. Inspect the error and the current ")
	b.WriteString("message, then call revise_message with a corrected message to retry.\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	return b.String()
}

// defaultAgentIterations caps how many tool-calling turns an agent runs before
// falling back to the guardrail. Each turn is one model call.
const defaultAgentIterations = 8

// aiAgent is a composite that lets an LLM accomplish a task by calling its
// branches as tools, one or more times, in a loop. Each branch is wired to the
// model as a function: the model's arguments become the branch's message body and
// the branch's output body is returned to the model as the tool result. Tool
// branches share the message, so variables they set accumulate across the loop.
// The guardrail (Default) flow is taken when the model refuses or never finishes.
type aiAgent struct {
	client        core.LLMClient
	system        string
	tools         []core.LLMTool
	branches      map[string]*Flow
	guardrail     *Flow
	maxIterations int
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) aiAgent(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Tools) == 0 {
		return nil, errors.New("ai-agent block requires at least one tool")
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, errors.New("ai-agent block requires a prompt")
	}
	if err := allowSlots(cfg, blockKindAIAgent,
		"tools", "default", "connector", "prompt", "guardrail", "maxIterations"); err != nil {
		return nil, err
	}

	client, err := resolveLLM(blockKindAIAgent, cfg.Connector, b.deps)
	if err != nil {
		return nil, err
	}

	branches, tools, err := b.agentTools(cfg.Tools)
	if err != nil {
		return nil, err
	}

	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultAgentIterations
	}

	block := &aiAgent{
		client:        client,
		system:        buildAgentSystem(cfg.Prompt, cfg.Guardrail),
		tools:         tools,
		branches:      branches,
		maxIterations: maxIterations,
	}
	if cfg.Default != nil {
		guardrail, defErr := b.subFlow(*cfg.Default)
		if defErr != nil {
			return nil, fmt.Errorf("ai-agent default: %w", defErr)
		}
		block.guardrail = guardrail
	}
	return block, nil
}

// agentTools builds the tool branches and their model-facing definitions,
// validating names, descriptions, uniqueness, and schemas.
func (b *builder) agentTools(configs []types.ToolConfig) (map[string]*Flow, []core.LLMTool, error) {
	branches := make(map[string]*Flow, len(configs))
	tools := make([]core.LLMTool, 0, len(configs))
	for i := range configs {
		tool := configs[i]
		if tool.Name == "" {
			return nil, nil, fmt.Errorf("ai-agent tool %d requires a name", i)
		}
		if tool.Description == "" {
			return nil, nil, fmt.Errorf("ai-agent tool %q requires a description", tool.Name)
		}
		if _, dup := branches[tool.Name]; dup {
			return nil, nil, fmt.Errorf("ai-agent tool %q is defined more than once", tool.Name)
		}
		schema, err := toolInputSchema(tool)
		if err != nil {
			return nil, nil, err
		}
		flow, err := b.subFlow(tool.Flow)
		if err != nil {
			return nil, nil, fmt.Errorf("ai-agent tool %q: %w", tool.Name, err)
		}
		branches[tool.Name] = flow
		tools = append(tools, core.LLMTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	return branches, tools, nil
}

// Process runs the agentic loop: the model calls tools (branches) until it
// finishes with a final result or the iteration cap is hit. Tool branches run on
// the shared message so variables accumulate; the final assistant text is folded
// into the body as the result.
func (a *aiAgent) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	body, err := msg.BodyJSON()
	if err != nil {
		return nil, fmt.Errorf("ai-agent: encode input body: %w", err)
	}
	messages := []core.LLMMessage{{
		Role: core.LLMRoleUser,
		Text: "Accomplish the task for this input message body:\n" + string(body),
	}}

	current := msg
	for iter := 0; iter < a.maxIterations; iter++ {
		resp, completeErr := a.client.Complete(ctx, core.LLMRequest{
			System:     a.system,
			Messages:   messages,
			Tools:      a.tools,
			ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceAuto},
		})
		if completeErr != nil {
			return nil, fmt.Errorf("ai-agent: %w", completeErr)
		}
		messages = append(messages, resp.Raw)

		if resp.StopReason == core.LLMStopRefusal {
			return a.fallback(ctx, current, "model refused")
		}
		if len(resp.ToolCalls) == 0 {
			return foldResult(current, resp.Text), nil
		}

		results := make([]core.LLMToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			var res core.LLMToolResult
			res, current = a.runTool(ctx, call, current)
			results = append(results, res)
		}
		messages = append(messages, core.LLMMessage{Role: core.LLMRoleTool, ToolResults: results})
	}

	return a.fallback(ctx, current, "exceeded max iterations")
}

// runTool dispatches one tool call to its branch: the call arguments become the
// branch's body and the branch's output body is the tool result. A branch error
// or a dropped message becomes an error result fed back to the model rather than
// aborting the agent. It returns the (possibly updated) current message so shared
// state carries forward.
func (a *aiAgent) runTool(
	ctx context.Context, call core.LLMToolCall, current *types.Message,
) (core.LLMToolResult, *types.Message) {
	flow, ok := a.branches[call.Name]
	if !ok {
		return errorResult(call.ID, fmt.Sprintf("unknown tool %q", call.Name)), current
	}
	args := call.Input
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := current.SetBodyJSON(args); err != nil {
		return errorResult(call.ID, fmt.Sprintf("invalid arguments: %v", err)), current
	}
	out, err := flow.Process(ctx, current)
	if err != nil {
		return errorResult(call.ID, err.Error()), current
	}
	if out == nil {
		return errorResult(call.ID, "tool produced no result"), current
	}
	content, err := out.BodyJSON()
	if err != nil {
		return errorResult(call.ID, fmt.Sprintf("encode result: %v", err)), out
	}
	return core.LLMToolResult{ToolCallID: call.ID, Content: string(content)}, out
}

// fallback runs the guardrail flow, or errors when none is configured so the
// failure propagates to a recovery path.
func (a *aiAgent) fallback(ctx context.Context, msg *types.Message, reason string) (*types.Message, error) {
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
		msg.Body = decoded
	} else {
		msg.Body = text
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

// buildAgentSystem assembles the agent's task system prompt.
func buildAgentSystem(prompt, guardrail string) string {
	var b strings.Builder
	b.WriteString("You are an agent that accomplishes a task by calling the available tools. ")
	b.WriteString("Call tools as needed; when the task is complete, respond with the final result ")
	b.WriteString("as JSON only (no prose, no markdown code fences).\n\n")
	b.WriteString(strings.TrimSpace(prompt))
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

// errorResult builds a tool result marked as an error so the model can react.
func errorResult(toolCallID, message string) core.LLMToolResult {
	return core.LLMToolResult{ToolCallID: toolCallID, Content: message, IsError: true}
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

// resolveLLM binds an AI composite to its LLM provider connector by name,
// asserting the shared core.LLMClient interface so any provider satisfies it. The
// kind labels the error.
//
//nolint:ireturn // the shared interface is the binding mechanism, by design
func resolveLLM(kind, name string, deps core.BlockDeps) (core.LLMClient, error) {
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
	return client, nil
}
