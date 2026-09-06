package ai

import "github.com/juancavallotti/octo/runtime/types"

// routerSettings configures the ai-router block.
type routerSettings struct {
	// Name of the LLM connector that picks the route.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Routing instruction; the model reads the body/variables before choosing.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// Describes when to fall back to the default path (low confidence / ambiguity).
	Guardrail string `json:"guardrail" octo:"label=Guardrail"`
	// Named, described branches; the model picks one by name.
	Routes []routeSettings `json:"routes" octo:"label=Routes,type=route-list,required"`
	// The guardrail path, run when the model is not confident.
	Default *types.FlowConfig `json:"default" octo:"label=Default,type=flow"`
}

// routeSettings is one branch of an ai-router: a Name and a Description the
// model uses to choose, plus the Process chain to run when it is chosen.
type routeSettings struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Process     []types.BlockConfig `json:"process"`
}

// retrySettings configures the ai-retry block.
type retrySettings struct {
	// Name of the LLM connector that revises the message between attempts.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Instruction for repairing the message from vars.error before a retry.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// How many times to revise and re-run the process chain.
	MaxAttempts int `json:"maxAttempts" octo:"label=Max attempts,default=3"`
	// The protected chain, re-run after each revision.
	Process []types.BlockConfig `json:"process" octo:"label=Process,type=block-list,required"`
	// Runs when attempts are exhausted; reads vars.error.
	Error []types.BlockConfig `json:"error" octo:"label=Error,type=block-list"`
}

// agentSettings configures the ai-agent block.
type agentSettings struct {
	// Name of the LLM connector that drives the agent loop.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Task instruction; the model calls tools as needed, then stops.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// Describes when to fall back to the default path (cannot complete with confidence).
	Guardrail string `json:"guardrail" octo:"label=Guardrail"`
	// CEL expression for the agent's opening user turn. Empty hands the model the
	// whole input body as a JSON document to work from, which is what an agent
	// transforming a payload wants; a conversational one states its question here so
	// the model answers it rather than replying in the shape it was handed.
	Input string `json:"input" octo:"label=Opening turn,type=cel"`
	// The shape the model is told to answer in. "json" suits an agent whose answer
	// is the next block's body; "text" suits one whose answer a person reads and
	// leaves the format to the prompt. The reply is parsed the same way either way.
	//nolint:lll // the enum tag (options + default) is inherently longer than 120 cols
	Answer string `json:"answer" octo:"label=Answer format,type=enum,enum=json|text,default=json"`
	// Cap on tool-calling turns before falling back to the guardrail.
	MaxIterations int `json:"maxIterations" octo:"label=Max iterations"`
	// CEL expression for the conversation thread id. When set, the agent loads the
	// thread's prior transcript before its run and saves it after; empty disables
	// memory.
	MemoryThreadID string `json:"memoryThreadId" octo:"label=Memory thread ID,type=cel"`
	// Stable name for the logical agent. Setting it opts the block into the
	// runtime's first-class memory: durable conversation history the platform can
	// list and replay, working memory checkpointed during the run, and — with user
	// memory on — curated facts about a person. It is stated rather than derived
	// because a derived name is a position in a file, and renaming the block would
	// destroy the conversations stored under it (issue #359).
	//
	// It must be unique across the agents that share a deployment. Two blocks
	// declaring the same agentId share one memory, which is what replicas of one
	// logical agent want and what two different agents almost never do.
	AgentID string `json:"agentId" octo:"label=Agent ID"`
	// CEL expression for the person the agent is talking to. Scopes user memory and
	// labels stored conversations so the platform can list one person's threads.
	UserID string `json:"userId" octo:"label=User ID,type=cel"`
	// Whether completed turns are recorded to durable conversation history. Unlike
	// working memory this record is never compacted, so it stays readable after the
	// agent has summarized its own context away. Requires an agent ID.
	//
	// Deliberately carries NO schema default, though the runtime's default is
	// "record". The editor seeds a new block with every field that declares one, so
	// a default here would write `history: record` into a block that has no agentId
	// yet — which is a flow that does not build, produced by dropping a block on a
	// canvas. The runtime applies the default in configureAgentStore instead, where
	// it can see whether there is an agent to record under.
	History string `json:"history" octo:"label=Conversation history,type=enum,enum=record|off"`
	// Give the agent remember/forget/search_memory tools so it can keep curated
	// facts about the person it is talking to and carry them into later
	// conversations. Requires an agent ID and a user ID.
	UserMemory bool `json:"userMemory" octo:"label=User memory"`
	// Boolean CEL condition that ends the run already working on this message's
	// conversation instead of starting one — a header, a body field, whatever the
	// caller uses to say stop. Requires a memory thread ID, since that is what
	// names the conversation.
	StopWhen string `json:"stopWhen" octo:"label=Stop when,type=cel"`
	// How long a tool call that needs a person waits for the answer before it is
	// denied on their behalf. A run parked on a call nobody is going to answer is
	// billed for the wait, so there is always a limit; this sets it.
	//nolint:lll // the tag carries a label and a default and is longer than 120 cols
	AuthorizeTimeout string `json:"authorizeTimeout" octo:"label=Authorization timeout,default=5m"`
	// CEL expression resolving the authorization an incoming message answers — the
	// id the tool_authorization event carried. A non-empty result makes the
	// invocation an answer rather than a message: it is handed to the run working on
	// this conversation and the flow stops. Required by any tool that declares an
	// authorize condition, and needs a memory thread ID.
	AuthorizeID string `json:"authorizeId" octo:"label=Authorization ID,type=cel"`
	// Boolean CEL condition deciding what an answer says. Read only on an
	// invocation the ID above already identified as one, so anything but a plain
	// yes — an expression that fails to evaluate included — denies the call.
	AuthorizeAllow string `json:"authorizeAllow" octo:"label=Authorization allowed,type=cel"`
	// Token budget for the whole prompt — system instructions, tool schemas and
	// conversation together — measured from what the provider reports it read. The
	// transcript is compacted when the prompt would exceed it. Applies with or
	// without memory: a run can talk itself past the model's window on its own.
	//nolint:lll // the tag carries a label and a default and is longer than 120 cols
	ContextMaxTokens int `json:"contextMaxTokens" octo:"label=Context max tokens,default=200000"`
	// MemoryMaxTokens is the name ContextMaxTokens replaced. It is kept, untagged,
	// only so a flow still using it fails with a sentence naming the new key
	// rather than with "unknown field". Remove it, and its rejection in
	// validateAgentConfig, after a release.
	MemoryMaxTokens int `json:"memoryMaxTokens"`
	// How to shrink memory over budget: prune drops the oldest turns; summarize
	// folds them into a running summary.
	//nolint:lll // the enum tag (options + default) is inherently longer than 120 cols
	MemoryCompaction string `json:"memoryCompaction" octo:"label=Memory compaction,type=enum,enum=prune|summarize,default=prune"`
	// Keep transcripts in the volatile tier (Redis in a cluster) rather than the
	// persistent one. For a conversation whose loss costs nothing — a specialist in
	// another agent's tool slot — never for one somebody will ask to see again.
	MemoryVolatile bool `json:"memoryVolatile" octo:"label=Volatile memory"`
	// Drive the provider's streaming API so the model's output reaches the events
	// path as it is produced. Requires an events path and a provider that streams.
	Stream bool `json:"stream" octo:"label=Stream model output"`
	// Which event types reach the events path. Empty emits every type this block can
	// produce; a type left out is never built at all.
	Emit []string `json:"emit" octo:"label=Emit events,type=string-list"`
	// Observer path, run once per agent event with the event as the message body.
	// Its result is discarded, so it reports on the run without taking part in it.
	Events *types.FlowConfig `json:"events" octo:"label=Events,type=flow"`
	// Names a conversation, once, on the exchange that opened it. It is handed the
	// question and the answer and whatever string it returns becomes the title;
	// returning nothing names nothing. The engine does the writing, so this chain
	// never has to know where the conversation is stored. Needs an agentId.
	NameThread *types.FlowConfig `json:"nameThread" octo:"label=Name conversation,type=flow"`
	// Named, described branches wired to the model as callable functions.
	Tools []agentToolSettings `json:"tools" octo:"label=Tools,type=tool-list,required"`
	// Named instruction resources the agent can load on demand. Each skill's name
	// and description are shown to the model up front; it calls the implicit
	// load_skill tool to pull the resource's full content into the conversation.
	Skills []skillSettings `json:"skills" octo:"label=Skills,type=skill-list"`
	// The guardrail path, run when the agent cannot complete the task.
	Default *types.FlowConfig `json:"default" octo:"label=Default,type=flow"`
}

// agentToolSettings is one branch of an ai-agent, wired to the model as a
// callable function. Name and Description tell the model what the tool does;
// InputSchema is the JSON Schema for its arguments (a JSON document, written
// inline as a string); the Process chain runs the tool, its arguments arriving
// as the message body and its output body returned to the model as the result.
type agentToolSettings struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	InputSchema string              `json:"inputSchema,omitempty"`
	Process     []types.BlockConfig `json:"process"`
	// Authorize is a boolean CEL condition over the call the model asked for,
	// deciding whether a person has to allow it before the branch runs. It reads
	// `input` (the decoded arguments), `tool.name`, `tool.id`, and the message
	// scope; see expr.ToolCallVars.
	//
	// Empty is free, and free is the default. Most tools are reads, and asking
	// about every one of them trains a person to click yes — which is the failure
	// this is meant to prevent rather than cause. It is written per tool because
	// the danger is a property of the tool and its arguments, not of the agent:
	// `input.method != "GET"` gates the write and leaves the read alone.
	Authorize string `json:"authorize,omitempty"`

	// Title, Annotations and OutputSchema are MCP metadata (see mcpToolSettings).
	// They are declared here only so an ai-agent that is given one fails with a
	// sentence naming where the field belongs, rather than with "unknown field".
	Title        string              `json:"title,omitempty"`
	Annotations  *mcpToolAnnotations `json:"annotations,omitempty"`
	OutputSchema string              `json:"outputSchema,omitempty"`
}

// skillSettings is one skill available to an ai-agent: a Name and Description
// advertised to the model up front, and Resource, the template resource whose
// rendered content the implicit load_skill tool returns when the model loads the
// skill.
type skillSettings struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Resource    string `json:"resource"`
}

// mcpRouterSettings configures the mcp-router block.
type mcpRouterSettings struct {
	// Name reported in the MCP initialize response; defaults to the block name.
	ServerName string `json:"serverName" octo:"label=Server name"`
	// Flows exposed as MCP tools. A tools/call runs the matching flow with the call
	// arguments as its body.
	Tools []mcpToolSettings `json:"tools" octo:"label=Tools,type=tool-list"`
	// Template resources advertised as MCP resources and served on resources/read.
	Resources []mcpResourceSettings `json:"resources" octo:"label=Resources,type=mcp-resource-list"`
	// Template resources advertised as MCP prompts and rendered on prompts/get; the
	// supplied arguments are exposed to the template as the body.
	Prompts []mcpPromptSettings `json:"prompts" octo:"label=Prompts,type=mcp-prompt-list"`
}

// mcpToolSettings is one tool an mcp-router advertises: the same Name,
// Description, InputSchema and Process an agent tool has, plus the metadata the
// protocol carries and an LLM tool call does not. An ai-agent tool has no place
// for these, and a decode of one into agentToolSettings says so.
type mcpToolSettings struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	InputSchema string              `json:"inputSchema,omitempty"`
	Process     []types.BlockConfig `json:"process"`
	// Title is a display name for people, where Name is the identifier the client
	// calls by. An MCP client shows this in a consent prompt or a tool picker.
	Title string `json:"title,omitempty"`
	// Annotations are hints about what calling this tool does, so a client can
	// decide how much ceremony to put in front of it.
	Annotations *mcpToolAnnotations `json:"annotations,omitempty"`
	// OutputSchema is the JSON Schema of what the tool branch returns. Declaring it
	// makes the router send the branch's result as structuredContent alongside the
	// text block, so a client can consume it as data rather than re-parsing prose.
	OutputSchema string `json:"outputSchema,omitempty"`
	// Authorize is an ai-agent setting (see agentToolSettings). It is declared
	// here only so an mcp-router that is given one fails with a sentence saying
	// why it has no effect: the protocol has its own consent step, run by the
	// client, and a second one here would be configuration that reads as a
	// boundary and is not.
	Authorize string `json:"authorize,omitempty"`
}

// mcpToolAnnotations are the MCP hints an mcp-router advertises about a tool.
//
// Every field is a pointer because "not stated" and "stated false" are different
// answers and the protocol's defaults are not all the same — readOnlyHint and
// idempotentHint default false, openWorldHint and destructiveHint default true.
// A bool would silently assert the default for every hint an author left out.
//
// They are hints, not enforcement. The protocol says clients must treat them as
// untrusted, and the runtime does not check a tool branch against them: a tool
// annotated readOnly can still write, and it is the author's job not to.
type mcpToolAnnotations struct {
	// ReadOnlyHint says the tool does not modify anything.
	ReadOnlyHint *bool `json:"readOnlyHint,omitempty"`
	// DestructiveHint says an update it performs may be destructive rather than
	// additive. Only meaningful when the tool is not read-only.
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	// IdempotentHint says calling it twice with the same arguments has no more
	// effect than calling it once. Only meaningful when the tool is not read-only.
	IdempotentHint *bool `json:"idempotentHint,omitempty"`
	// OpenWorldHint says the tool touches something outside the server — the web, a
	// third-party API — rather than a closed domain the server owns.
	OpenWorldHint *bool `json:"openWorldHint,omitempty"`
}

// mcpResourceSettings is one resource an mcp-router advertises. Exactly one of
// URI and URITemplate is set: a URI is one fixed document, listed on
// resources/list; a URITemplate is a family of them, listed on
// resources/templates/list instead. Name/Description/MimeType are advertised
// metadata; Resource is the template resource whose rendered content is returned.
type mcpResourceSettings struct {
	URI string `json:"uri,omitempty"`
	// URITemplate is an RFC 6570 level-1 template — literal text with {name}
	// placeholders, e.g. "contacts://contact/{id}" — that stands for a family of
	// documents rather than one. A client fills the placeholders in and reads the
	// concrete uri; resources/read matches it back against the template and
	// exposes what each placeholder took to the rendered template as the message
	// body (body.id), the same bargain a prompt's arguments strike.
	//
	// Only simple expansion is supported: no operators (+ # . / ; ? &), no explode,
	// no prefix lengths. Each of those changes what a match even means, and no
	// client has asked for one.
	URITemplate string `json:"uriTemplate,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	Resource    string `json:"resource"`
}

// mcpPromptSettings is one prompt an mcp-router advertises. Name is the id
// clients get it by; Description and Arguments are advertised metadata; Resource
// is the template resource whose rendered content becomes the prompt message.
// The prompt arguments are exposed to the template as the message body
// (body.<arg>).
type mcpPromptSettings struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Arguments   []mcpPromptArg `json:"arguments,omitempty"`
	Resource    string         `json:"resource"`
}

// mcpPromptArg describes one argument of an mcp-router prompt, advertised on
// prompts/list so clients know what to supply to prompts/get.
type mcpPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// toolSpec is what building a tool branch needs, common to an agent tool and an
// MCP tool: the two settings structs each reduce to it.
type toolSpec struct {
	name        string
	description string
	inputSchema string
	process     []types.BlockConfig
}
