// Schema-only metadata for composite (control-flow) blocks. Composites are not
// leaf blocks: they are dispatched by the flow builder (flow.go) and read their
// typed sub-flow slots off types.BlockConfig, so they have no settings struct to
// reflect. Instead each declares a schema-only meta struct here — never decoded,
// only reflected for the editor schema — whose fields carry octo slot tags. A
// pure sub-flow slot uses a *struct{} placeholder since type=flow overrides
// inference and no value is read from it.
package engine

import (
	"reflect"

	"github.com/juancavallotti/octo/core"
)

// Palette groups the engine's blocks fall under in the editor sidebar.
const (
	groupFlowControl  = "Flow Control"
	groupAILLM        = "AI & LLM"
	groupStorageCache = "Storage & Cache"
	groupData         = "Data"
)

// registerFlowControlComposites registers the plain control-flow composites
// (branching, iteration, error handling) under the "Flow Control" group.
func registerFlowControlComposites() {
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "handle-errors",
		Label:       "Handle Errors",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "ShieldAlert",
		Description: "Run the process chain; on error, expose vars.error and run the error chain (recovery).",
		Config:      reflect.TypeFor[handleErrorsMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "fork",
		Label:       "Fork",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "GitFork",
		Description: "Scatter the message across parallel branches, then join and pass through.",
		Config:      reflect.TypeFor[forkMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "if",
		Label:       "If",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "Split",
		Description: "Conditional branching on a CEL boolean expression.",
		Config:      reflect.TypeFor[ifMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "switch",
		Label:       "Switch",
		Category:    core.CategoryControlFlow,
		Group:       groupFlowControl,
		Icon:        "Split",
		Description: "Multi-case routing; runs the first matching case or the default.",
		Config:      reflect.TypeFor[switchMeta](),
	})
}

// registerIterationComposites registers the iteration and filter composites,
// also under the "Flow Control" group.
func registerIterationComposites() {
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "foreach",
		Label:    "For Each",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "Repeat",
		Description: "Sequentially iterate over an array, running the body per element. In map mode, " +
			"each element's resulting body is collected into an array that replaces the message body.",
		Config: reflect.TypeFor[foreachMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "enrich",
		Label:    "Enrich",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "Sparkles",
		Description: "Run a body flow on an isolated copy of the message, then enrich the message " +
			"from the result: a CEL expression for the body and a CEL expression per variable.",
		Config: reflect.TypeFor[enrichMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "validate",
		Label:    "Validate",
		Category: core.CategoryControlFlow,
		Group:    groupFlowControl,
		Icon:     "ShieldCheck",
		Description: "Filter block: assert a list of CEL rules against the message. If all hold, the " +
			"message passes through; if any fail, the block rejects — running the onReject sub-flow " +
			"(or a built-in response) and stopping the flow so the rest of the chain never runs. " +
			"Failing rule messages are exposed as vars.validationErrors.",
		Config: reflect.TypeFor[validateMeta](),
	})
}

// registerAIComposites registers the LLM-driven composites under the "AI & LLM"
// group.
func registerAIComposites() {
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "ai-retry",
		Label:    "AI Retry",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "RefreshCw",
		Description: "Run the process chain; on error, let the LLM inspect vars.error, revise the " +
			"message, and retry up to maxAttempts, then run the error chain.",
		Config: reflect.TypeFor[aiRetryMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "ai-router",
		Label:    "AI Router",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Route",
		Description: "Route the message to a named branch the LLM chooses after inspecting it; the " +
			"default path is the guardrail taken when it is not confident.",
		Config: reflect.TypeFor[aiRouterMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "ai-agent",
		Label:    "AI Agent",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Bot",
		Description: "Let the LLM call branches as tools in a loop to accomplish a task; tool runs " +
			"share accumulating variables. The default path is the guardrail.",
		Config: reflect.TypeFor[aiAgentMeta](),
	})
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "mcp-router",
		Label:    "MCP Router",
		Category: core.CategoryControlFlow,
		Group:    groupAILLM,
		Icon:     "Mcp",
		Description: "Turn a flow into a stateless MCP server behind an HTTP source: advertise tool " +
			"flows as MCP tools, template resources as MCP resources and prompts, and route " +
			"tools/call to the matching flow. Calls no LLM.",
		Config: reflect.TypeFor[mcpRouterMeta](),
	})
}

func init() {
	registerFlowControlComposites()
	registerIterationComposites()
	registerAIComposites()
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "cache-scope",
		Label:       "Cache",
		Category:    core.CategoryControlFlow,
		Group:       groupStorageCache,
		Icon:        "Clock",
		Description: "Memoize the body flow's result in the runtime store, keyed by a CEL expression.",
		Config:      reflect.TypeFor[cacheScopeMeta](),
	})
}

// ifMeta is the schema-only description of the `if` composite's editor slots.
type ifMeta struct {
	// CEL boolean expression.
	Condition string `json:"condition" octo:"label=Condition,type=cel,required"`
	// Flow run when the condition is true.
	Then *struct{} `json:"then" octo:"label=Then,type=flow,required"`
	// Flow run when the condition is false.
	Else *struct{} `json:"else" octo:"label=Else,type=flow"`
}

// handleErrorsMeta describes the handle-errors composite's editor slots.
type handleErrorsMeta struct {
	// The happy-path chain.
	Process *struct{} `json:"process" octo:"label=Process,type=block-list,required"`
	// Runs when the process chain errors; reads vars.error.
	Error *struct{} `json:"error" octo:"label=Error,type=block-list,required"`
}

// forkMeta describes the fork composite's editor slots.
type forkMeta struct {
	// Parallel flows; each receives a clone of the message.
	Branches *struct{} `json:"branches" octo:"label=Branches,type=flow-list,required"`
}

// switchMeta describes the switch composite's editor slots.
type switchMeta struct {
	// Ordered, condition-guarded flows (each has a CEL `when`).
	Cases *struct{} `json:"cases" octo:"label=Cases,type=case-list,required"`
	// Flow run when no case matches.
	Default *struct{} `json:"default" octo:"label=Default,type=flow"`
}

// foreachMeta describes the foreach composite's editor slots.
type foreachMeta struct {
	// CEL expression evaluating to the array to iterate.
	Items string `json:"items" octo:"label=Items,type=cel,required"`
	// Variable name bound to each element.
	As string `json:"as" octo:"label=As,default=item"`
	// How the body's results are treated: "iterate" threads the message through the
	// body once per element (side effects); "map" collects each element's resulting
	// body into an array that replaces the message body (transformation).
	Mode string `json:"mode" octo:"label=Mode,type=enum,enum=iterate|map,default=iterate"`
	// Flow run once per element.
	Body *struct{} `json:"body" octo:"label=Body,type=flow,required"`
}

// enrichMeta describes the enrich composite's editor slots.
type enrichMeta struct {
	// CEL expression for the new body, evaluated against the scope's result (the
	// enriched clone). Empty leaves the body unchanged.
	SetBody string `json:"setBody" octo:"label=Set body,type=cel"`
	// Map of variable name to CEL expression, each evaluated against the scope's
	// result and set on the message.
	SetVars map[string]string `json:"setVars" octo:"label=Set variables,type=string-map"`
	// Flow run once on an isolated clone of the message.
	Body *struct{} `json:"body" octo:"label=Body,type=flow,required"`
}

// validateMeta describes the validate composite's editor slots.
type validateMeta struct {
	// Assertions that must all evaluate true. Each entry is {expr: <CEL bool>,
	// message: <text>}; the messages of any that fail are collected into
	// vars.validationErrors. Expressions see body, vars, eventID, correlationID,
	// env, now.
	Rules *struct{} `json:"rules" octo:"label=Rules,type=rule-list,required"`
	// HTTP status the built-in rejection response uses when a rule fails and no
	// onReject flow is set. Defaults to 422.
	RejectStatus int `json:"rejectStatus" octo:"label=Reject status"`
	// Optional sub-flow run when a rule fails, before the flow stops. It shapes the
	// response itself (e.g. set-payload + set httpStatus) and can read
	// vars.validationErrors. Empty uses the built-in default response.
	OnReject *struct{} `json:"onReject" octo:"label=On reject,type=flow"`
}

// aiRetryMeta describes the ai-retry composite's editor slots.
type aiRetryMeta struct {
	// Name of the LLM connector that revises the message between attempts.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Instruction for repairing the message from vars.error before a retry.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// How many times to revise and re-run the process chain.
	MaxAttempts int `json:"maxAttempts" octo:"label=Max attempts,default=3"`
	// The protected chain, re-run after each revision.
	Process *struct{} `json:"process" octo:"label=Process,type=block-list,required"`
	// Runs when attempts are exhausted; reads vars.error.
	Error *struct{} `json:"error" octo:"label=Error,type=block-list"`
}

// aiRouterMeta describes the ai-router composite's editor slots.
type aiRouterMeta struct {
	// Name of the LLM connector that picks the route.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Routing instruction; the model reads the body/variables before choosing.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// Describes when to fall back to the default path (low confidence / ambiguity).
	Guardrail string `json:"guardrail" octo:"label=Guardrail"`
	// Named, described branches; the model picks one by name.
	Routes *struct{} `json:"routes" octo:"label=Routes,type=route-list,required"`
	// The guardrail path, run when the model is not confident.
	Default *struct{} `json:"default" octo:"label=Default,type=flow"`
}

// aiAgentMeta describes the ai-agent composite's editor slots.
type aiAgentMeta struct {
	// Name of the LLM connector that drives the agent loop.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Task instruction; the model calls tools as needed, then stops.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// Describes when to fall back to the default path (cannot complete with confidence).
	Guardrail string `json:"guardrail" octo:"label=Guardrail"`
	// Cap on tool-calling turns before falling back to the guardrail.
	MaxIterations int `json:"maxIterations" octo:"label=Max iterations"`
	// CEL expression for the conversation thread id. When set, the agent loads the
	// thread's prior transcript before its run and saves it after; empty disables
	// memory.
	MemoryThreadID string `json:"memoryThreadId" octo:"label=Memory thread ID,type=cel"`
	// Estimated-token budget for the stored transcript; it is compacted when it
	// exceeds this.
	MemoryMaxTokens int `json:"memoryMaxTokens" octo:"label=Memory max tokens,default=8000"`
	// How to shrink memory over budget: prune drops the oldest turns; summarize
	// folds them into a running summary.
	//nolint:lll // the enum tag (options + default) is inherently longer than 120 cols
	MemoryCompaction string `json:"memoryCompaction" octo:"label=Memory compaction,type=enum,enum=prune|summarize,default=prune"`
	// Named, described branches wired to the model as callable functions.
	Tools *struct{} `json:"tools" octo:"label=Tools,type=tool-list,required"`
	// Named instruction resources the agent can load on demand. Each skill's name
	// and description are shown to the model up front; it calls the implicit
	// load_skill tool to pull the resource's full content into the conversation.
	Skills *struct{} `json:"skills" octo:"label=Skills,type=skill-list"`
	// The guardrail path, run when the agent cannot complete the task.
	Default *struct{} `json:"default" octo:"label=Default,type=flow"`
}

// mcpRouterMeta describes the mcp-router composite's editor slots.
type mcpRouterMeta struct {
	// Name reported in the MCP initialize response; defaults to the block name.
	ServerName string `json:"serverName" octo:"label=Server name"`
	// Flows exposed as MCP tools. A tools/call runs the matching flow with the call
	// arguments as its body.
	Tools *struct{} `json:"tools" octo:"label=Tools,type=tool-list"`
	// Template resources advertised as MCP resources and served on resources/read.
	Resources *struct{} `json:"resources" octo:"label=Resources,type=mcp-resource-list"`
	// Template resources advertised as MCP prompts and rendered on prompts/get; the
	// supplied arguments are exposed to the template as the body.
	Prompts *struct{} `json:"prompts" octo:"label=Prompts,type=mcp-prompt-list"`
}

// cacheScopeMeta describes the cache-scope composite's editor slots.
type cacheScopeMeta struct {
	// CEL expression evaluated to the cache key.
	Key string `json:"key" octo:"label=Cache Key,type=cel,required"`
	// How long an entry stays fresh (e.g. '5m'); '0' never expires.
	TTL string `json:"ttl" octo:"label=TTL,type=string,default=60s"`
	// Flow whose result is cached and replayed on a hit.
	Body *struct{} `json:"body" octo:"label=Body,type=flow,required"`
}
