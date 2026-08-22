package types

// FlowConfig is the recursive unit of pipeline composition. The root flow,
// listed under Config.Flows, binds a Source and a worker-pool size; sub-flows
// nested inside a composite block reuse the same shape but must not set Source,
// Workers, Buffer, Pool, or Error (the core builder validates this).
type FlowConfig struct {
	Name    string        `yaml:"name,omitempty"`
	Source  *SourceConfig `yaml:"source,omitempty"`
	Process []BlockConfig `yaml:"process"`
	// Error is the root flow's error path: when the Process chain returns an
	// error, the runtime exposes it as vars.error and runs this chain; on success
	// its output becomes the flow's result (recovery). It is a bare block chain,
	// like Process. Root flows only.
	Error   []BlockConfig `yaml:"error,omitempty"`
	Workers int           `yaml:"workers,omitempty"`
	Buffer  int           `yaml:"buffer,omitempty"`
	// Pool sizes the shared worker pool the root flow owns and passes down to
	// composite blocks that schedule work concurrently (e.g. a fork's branches).
	// Root flows only; defaults when unset.
	Pool int `yaml:"pool,omitempty"`
}

// SourceConfig binds a flow's entry point to a connector instance and a
// connector-specific source type.
type SourceConfig struct {
	// Connector is the Name of a configured connector instance, not its Type.
	Connector string   `yaml:"connector"`
	Type      string   `yaml:"type"`
	Settings  Settings `yaml:"settings,omitempty"`
}

// BlockConfig describes one step in a flow. Leaf blocks use only Type, Name, and
// Settings. Composite kinds use explicit typed slots: a "handle-errors" populates
// Process and Error; a "fork" populates Branches; an "if" populates
// Condition/Then/Else; a "switch" populates Cases and optionally Default; a
// "foreach" populates Items/As/Body; an "enrich" populates Body and optionally
// SetBody/SetVars. The AI composites use Connector/Prompt and
// their own slots: an "ai-router" populates Routes (+ Default as the guardrail);
// an "ai-agent" populates Tools (+ Default), MaxIterations; an "ai-retry"
// populates Process/Error and MaxAttempts. The Flow<->Block recursion
// (FlowConfig.Process -> []BlockConfig -> the composite slots -> FlowConfig) lets
// the parser build the whole tree in one pass.
type BlockConfig struct {
	Type     string   `yaml:"type"`
	Name     string   `yaml:"name,omitempty"`
	Settings Settings `yaml:"settings,omitempty"`

	// Ref names a reusable processor defined under Config.Processors. When set,
	// the block takes its type and base settings from that definition; any
	// Settings here override the referenced ones key-by-key. A block sets either
	// Ref or Type, not both (an inline Type equal to the referenced type is the
	// one allowed overlap).
	Ref string `yaml:"ref,omitempty"`

	// Process is the happy-path block chain of a "handle-errors" block. It is a
	// bare block list, like a flow's Process, so a handle-errors block reads as a
	// mini-flow embedded inline.
	Process []BlockConfig `yaml:"process,omitempty"`
	// Error is the error-path block chain of a "handle-errors" block: it runs when
	// the Process chain errors, with the error exposed as vars.error.
	Error []BlockConfig `yaml:"error,omitempty"`
	// Branches are the parallel flows of a "fork" block.
	Branches []FlowConfig `yaml:"branches,omitempty"`

	// Condition is the boolean expression of an "if" block.
	Condition string `yaml:"condition,omitempty"`
	// Then is the flow an "if" block runs when its condition is true.
	Then *FlowConfig `yaml:"then,omitempty"`
	// Else is the flow an "if" block runs when its condition is false (optional).
	Else *FlowConfig `yaml:"else,omitempty"`

	// Cases are the ordered, condition-guarded flows of a "switch" block.
	Cases []CaseConfig `yaml:"cases,omitempty"`
	// Default is the flow a "switch" block runs when no case matches (optional).
	Default *FlowConfig `yaml:"default,omitempty"`

	// Items is the expression a "foreach" block evaluates to the array it
	// iterates.
	Items string `yaml:"items,omitempty"`
	// As is the variable name a "foreach" block binds each element to; it
	// defaults to "item" when unset.
	As string `yaml:"as,omitempty"`
	// Mode is how a "foreach" block treats its body's results: "iterate" (the
	// default) threads the message through the body once per element, while "map"
	// collects each element's resulting body into an array that replaces the
	// message body.
	Mode string `yaml:"mode,omitempty"`
	// Body is the flow a "foreach", "cache-scope", or "enrich" block runs; foreach
	// runs it once per element, cache-scope runs it on a cache miss, enrich runs it
	// once on an isolated copy of the message.
	Body *FlowConfig `yaml:"body,omitempty"`

	// SetBody is an "enrich" block's CEL expression for the body to propagate back
	// to the message. It is evaluated against the scope's result (the message after
	// the body flow ran on an isolated clone), so it can reference the enriched
	// body/vars. Empty leaves the incoming body unchanged.
	SetBody string `yaml:"setBody,omitempty"`
	// SetVars is an "enrich" block's map of variable name to CEL expression. Each
	// expression is evaluated against the scope's result and set on the message, so
	// the block enriches exactly the variables it names.
	SetVars map[string]string `yaml:"setVars,omitempty"`

	// Key is the cache-key expression of a "cache-scope" block (evaluated per
	// message). TTL is how long a cached entry stays fresh, a duration string
	// ("5m"); empty uses the default and "0" never expires.
	Key string `yaml:"key,omitempty"`
	TTL string `yaml:"ttl,omitempty"`

	// Rules are the assertions of a "validate" block; all must evaluate true or
	// the block rejects the message.
	Rules []RuleConfig `yaml:"rules,omitempty"`
	// OnReject is the shared "filter" slot: the sub-flow a filter block (validate,
	// jwt-validate) runs when it rejects a message, before requesting the flow
	// stop. It shapes the terminal response itself. Empty uses the block's
	// built-in default response.
	OnReject *FlowConfig `yaml:"onReject,omitempty"`
	// RejectStatus is the HTTP status a filter block's built-in default response
	// uses when it rejects (and no OnReject sub-flow is set). Zero applies the
	// block's own default.
	RejectStatus int `yaml:"rejectStatus,omitempty"`

	// Connector names the LLM connector the AI composites (ai-router, ai-agent,
	// ai-retry) call through.
	Connector string `yaml:"connector,omitempty"`
	// Prompt is the routing/task/repair instruction given to the model by the AI
	// composites.
	Prompt string `yaml:"prompt,omitempty"`
	// Guardrail describes when the model should fall back to the Default path; it
	// is used by ai-router and ai-agent.
	Guardrail string `yaml:"guardrail,omitempty"`
	// Answer is the shape an "ai-agent" is told to reply in: "json" (the default)
	// or "text". It decides one sentence of the system prompt and nothing else —
	// the reply is parsed the same way either way, becoming a structured body when
	// it happens to be JSON and a string when it does not.
	//
	// The default exists because an agent's answer is the next block's body, so a
	// flow reading body.tier needs the model to have been asked for an object. An
	// agent whose answer a person reads wants "text", and needs it: told to reply
	// in JSON here and in prose by its own prompt, which instruction a model obeys
	// is decided by the provider rather than by the flow.
	Answer string `yaml:"answer,omitempty"`
	// Input is a CEL expression producing the text of an "ai-agent"'s opening user
	// turn. Empty hands the model the whole input body as a JSON document to work
	// from, which is what an agent transforming a payload wants.
	//
	// A conversational agent wants the opposite: state the question, and the model
	// answers it. Handed a body, a model that does not reason before replying tends
	// to answer with a body of the same shape — so an agent whose reply is read as
	// prose says here which part of the message is the question.
	Input string `yaml:"input,omitempty"`
	// Routes are the named, described branches of an "ai-router" block. The model
	// picks one; Default is the guardrail taken when it is not confident.
	Routes []RouteConfig `yaml:"routes,omitempty"`
	// Tools are the named, described branches of an "ai-agent" block, each wired
	// to the model as a callable function.
	Tools []ToolConfig `yaml:"tools,omitempty"`
	// Skills are named instruction resources an "ai-agent" can load on demand.
	// The agent is told each skill's name and description up front and is given
	// an implicit load_skill tool to pull a skill's full content (a rendered
	// template resource) into the conversation when it needs it.
	Skills []SkillConfig `yaml:"skills,omitempty"`
	// MaxIterations caps how many tool-calling turns an "ai-agent" runs before
	// falling back to the guardrail (default applied by the builder).
	MaxIterations int `yaml:"maxIterations,omitempty"`
	// MemoryThreadID enables per-thread conversation memory for an "ai-agent": a
	// CEL expression resolved to the thread id whose transcript is loaded before
	// the run and saved after. Empty disables memory.
	MemoryThreadID string `yaml:"memoryThreadId,omitempty"`
	// ContextMaxTokens is an "ai-agent"'s token budget for its whole prompt —
	// system instructions, tool schemas and conversation together — measured from
	// what the provider reports it read. The transcript is compacted when the
	// prompt would exceed it. Default applied by the builder.
	ContextMaxTokens int `yaml:"contextMaxTokens,omitempty"`
	// MemoryMaxTokens is the name ContextMaxTokens replaced, kept only so a flow
	// still using it fails to build instead of silently losing its budget: flow
	// YAML is decoded permissively, so an unknown key is dropped without a word.
	// Remove it, and its rejection in the ai-agent builder, after a release.
	MemoryMaxTokens int `yaml:"memoryMaxTokens,omitempty"`
	// MemoryCompaction is how an "ai-agent" shrinks memory over budget: "prune"
	// (drop oldest, the default) or "summarize" (fold the oldest turns into a summary).
	MemoryCompaction string `yaml:"memoryCompaction,omitempty"`
	// Events is the observer sub-flow a block runs once per event it reports, with
	// the event as the message body. Its result is discarded: the sub-flow
	// reports, it does not take part in the run. An "ai-agent" reports on its own
	// turns and tool calls; a "cli-run" reports its command's output a line at a
	// time.
	Events *FlowConfig `yaml:"events,omitempty"`
	// Emit lists which event types reach Events. Empty emits every type the block's
	// configuration can produce. A type left out is never built, so this is the
	// cheap lever; finer choices belong in the sub-flow itself.
	Emit []string `yaml:"emit,omitempty"`
	// Stream makes an "ai-agent" drive its provider's streaming API, so the model's
	// output reaches Events as it is produced rather than only once a turn ends.
	Stream bool `yaml:"stream,omitempty"`
	// MaxAttempts caps how many times an "ai-retry" re-runs its Process chain
	// after an LLM-driven revision before falling through to Error (default
	// applied by the builder).
	MaxAttempts int `yaml:"maxAttempts,omitempty"`

	// Program is a "cli-run" CEL expression naming the program to execute: a bare
	// name, resolved through $PATH, or an absolute path. Its result is resolved
	// and then checked against Allow on every message, when Allow is set.
	//
	// It is an expression rather than a literal so the choice of program can come
	// from the message — which is what lets an "ai-agent" tool branch hand a model
	// a set of commands and let it pick. In that arrangement Allow is the only
	// thing standing between the model and arbitrary execution.
	Program string `yaml:"program,omitempty"`
	// Args is a "cli-run" CEL expression yielding the argument list, which must
	// evaluate to a list of strings. Arguments reach the program as argv and no
	// shell is involved, so nothing in them is interpreted: a semicolon is a
	// semicolon.
	Args string `yaml:"args,omitempty"`
	// Stdin is a "cli-run" CEL expression whose string result is written to the
	// process's standard input. Empty writes nothing and closes it, so a program
	// that reads stdin sees EOF rather than hanging.
	Stdin string `yaml:"stdin,omitempty"`
	// Allow is the set of programs a "cli-run" may execute, written as bare names,
	// absolute paths, or a mix. Entries are matched after resolution, so a bare
	// name matches the absolute path $PATH resolves it to. Symlinks are not
	// followed — see resolveProgram for why.
	//
	// Empty means no restriction. That keeps the block usable for the local,
	// iterative work it is best at, where requiring a list that repeats the
	// program two lines above would be ceremony. It also means a block with no
	// Allow whose Program comes from the message runs whatever it is handed — the
	// builder warns about exactly that combination, and a flow anyone else can
	// reach should declare a list.
	Allow []string `yaml:"allow,omitempty"`
	// AllowInterpreters permits an interpreter (sh, bash, python, env, xargs, …)
	// on a "cli-run" Allow list. One is refused by default because its arguments
	// are themselves a program, which makes the list a formality. It applies only
	// to an explicit list — a block with no list has already said "anything".
	AllowInterpreters bool `yaml:"allowInterpreters,omitempty"`
	// Env names the environment variables a "cli-run" passes to the child.
	// Nothing else is passed: the command starts with these and nothing more, so
	// a secret in the runtime's own environment cannot reach it. Most programs
	// want at least PATH and HOME.
	Env []string `yaml:"env,omitempty"`
	// WorkDir is the directory a "cli-run" runs its command in. Empty uses the
	// runtime's own.
	WorkDir string `yaml:"workDir,omitempty"`
	// Timeout is how long a "cli-run" command may run before it is killed, a
	// duration string ("30s"). A command holds a flow worker for as long as it
	// runs, so there is no unbounded option; empty applies the block's default.
	Timeout string `yaml:"timeout,omitempty"`
	// MaxOutputBytes caps the output a "cli-run" captures onto its result. It
	// applies whether or not an Events path is watching, since watching output
	// does not consume it. Lines still counts everything the command produced, so
	// a capture cut short by this cap is visible rather than silent.
	MaxOutputBytes int64 `yaml:"maxOutputBytes,omitempty"`
	// OnExit is what a "cli-run" does with a non-zero exit status: "fail" (the
	// default) errors the block so the flow's error chain sees it, "continue"
	// carries on and leaves the decision to a later block reading body.exitCode.
	OnExit string `yaml:"onExit,omitempty"`

	// ServerName is the MCP server name an "mcp-router" reports in its initialize
	// response; it defaults to the block name (then "octo-mcp") when unset.
	ServerName string `yaml:"serverName,omitempty"`
	// Resources are the template resources an "mcp-router" advertises as MCP
	// resources (each with its own uri) and serves on resources/read.
	Resources []MCPResourceConfig `yaml:"resources,omitempty"`
	// Prompts are the template resources an "mcp-router" advertises as MCP prompts
	// (each with named arguments) and renders on prompts/get. The "mcp-router"
	// reuses the Tools slot to expose flows as MCP tools.
	Prompts []MCPPromptConfig `yaml:"prompts,omitempty"`

	// BuildResponse is the shared "takeover" slot: the sub-flow a block runs on the
	// original message once it has taken the flow asynchronous, to shape what the
	// caller gets back before the chain stops. It is the asynchronous sibling of
	// OnReject — the work carries on elsewhere, so this is the caller's receipt
	// rather than its result. Empty stops with the message as it stands.
	BuildResponse *FlowConfig `yaml:"buildResponse,omitempty"`

	// Delimiter is the separator a "split" block cuts on in mode "delimiter".
	Delimiter string `yaml:"delimiter,omitempty"`
	// ChunkSize is how many runes a "split" block puts in each element in mode
	// "chunk".
	ChunkSize int `yaml:"chunkSize,omitempty"`
	// OnError is a "split" block's policy when an element cannot be dispatched:
	// "skip" (the default) logs and carries on, "abort" fails the block. It governs
	// dispatch only — an element that fails once it is running fails in its own
	// invocation, which is the point of splitting.
	OnError string `yaml:"onError,omitempty"`

	// StoreKey is the identity an "aggregate" block's group state and leader
	// election are namespaced by. It defaults to the block's own address, which is
	// stable until the block moves; setting it explicitly keeps in-flight groups
	// alive across an edit to the flow.
	StoreKey string `yaml:"storeKey,omitempty"`
	// Correlation is an "aggregate" block's CEL expression for which group a
	// message belongs to. It defaults to vars.groupId, the variable a "split"
	// block sets, which is what pairs the two with no configuration.
	Correlation string `yaml:"correlation,omitempty"`
	// Strategy is how an "aggregate" block combines messages: "append" (the
	// default) collects bodies into an array, "expression" folds through Expression.
	Strategy string `yaml:"strategy,omitempty"`
	// Expression is an "aggregate" block's fold, evaluated with the group so far in
	// vars.group; its result becomes the new accumulator.
	Expression string `yaml:"expression,omitempty"`
	// CompletionSize is an "aggregate" block's capacity condition: a CEL expression
	// yielding the number of messages the group expects. It is an expression rather
	// than a number so it can be dynamic — "batches of 100" and "however many this
	// split produced" are then the same field. It defaults to vars.groupSize.
	CompletionSize string `yaml:"completionSize,omitempty"`
	// CompletionTimeout is an "aggregate" block's time condition, a duration string
	// ("5s"). It is the only condition that fires without a message arriving, so it
	// is what bounds a group whose other conditions may never be met.
	CompletionTimeout string `yaml:"completionTimeout,omitempty"`
	// CompletionExpression is an "aggregate" block's predicate condition, evaluated
	// after each fold against the message and vars.group.
	CompletionExpression string `yaml:"completionExpression,omitempty"`
	// TimeoutFrom is what CompletionTimeout is measured from: "first" (the default,
	// a fixed window from the group opening) or "last" (a sliding idle window).
	TimeoutFrom string `yaml:"timeoutFrom,omitempty"`
	// MaxGroups caps how many groups an "aggregate" block keeps open at once, so a
	// high-cardinality correlation cannot grow without bound.
	MaxGroups int `yaml:"maxGroups,omitempty"`
	// OnOverflow is what an "aggregate" block does at MaxGroups: "fail" (the
	// default) errors the message into the flow's error chain, "complete" releases
	// the oldest group early, "drop" discards the message.
	OnOverflow string `yaml:"onOverflow,omitempty"`
}

// MCPResourceConfig is one resource an "mcp-router" advertises. Exactly one of
// URI and URITemplate is set: a URI is one fixed document, listed on
// resources/list; a URITemplate is a family of them, listed on
// resources/templates/list instead. Name/Description/MimeType are advertised
// metadata; Resource is the template resource whose rendered content is returned.
type MCPResourceConfig struct {
	URI string `yaml:"uri,omitempty"`
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
	URITemplate string `yaml:"uriTemplate,omitempty"`
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	MimeType    string `yaml:"mimeType,omitempty"`
	Resource    string `yaml:"resource"`
}

// MCPPromptConfig is one prompt an "mcp-router" advertises. Name is the id clients
// get it by; Description and Arguments are advertised metadata; Resource is the
// template resource whose rendered content becomes the prompt message. The prompt
// arguments are exposed to the template as the message body (body.<arg>).
type MCPPromptConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description,omitempty"`
	Arguments   []MCPPromptArg `yaml:"arguments,omitempty"`
	Resource    string         `yaml:"resource"`
}

// MCPPromptArg describes one argument of an "mcp-router" prompt, advertised on
// prompts/list so clients know what to supply to prompts/get.
type MCPPromptArg struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
}

// CaseConfig is one branch of a "switch" block: a boolean When expression and an
// inline flow (its process chain and optional name) to run when When is the first
// case to evaluate true.
type CaseConfig struct {
	When string     `yaml:"when"`
	Flow FlowConfig `yaml:",inline"`
}

// RuleConfig is one assertion of a "validate" block: a boolean CEL Expr that must
// hold, and the Message surfaced (in vars.validationErrors and the built-in
// response) when it does not.
type RuleConfig struct {
	Expr    string `yaml:"expr"`
	Message string `yaml:"message,omitempty"`
}

// RouteConfig is one branch of an "ai-router" block: a Name and a Description the
// model uses to choose, plus the Process chain to run when it is chosen. Process
// is a bare block list (not an inline FlowConfig) so the route's own Name does not
// collide with FlowConfig's name field on decode; a route never needs the other
// flow-level fields (source, workers, error).
type RouteConfig struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Process     []BlockConfig `yaml:"process"`
}

// ToolConfig is one branch of an "ai-agent" block, wired to the model as a
// callable function. Name and Description tell the model what the tool does;
// InputSchema is the JSON Schema for its arguments (a JSON document, written
// inline as a string); the Process chain runs the tool, its arguments arriving as
// the message body and its output body returned to the model as the result.
// Process is a bare block list (not an inline FlowConfig) for the same
// name-collision reason as RouteConfig.
type ToolConfig struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	InputSchema string        `yaml:"inputSchema,omitempty"`
	Process     []BlockConfig `yaml:"process"`

	// Title, Annotations and OutputSchema are MCP metadata, advertised by an
	// "mcp-router" and rejected on an "ai-agent": an LLM tool call has no protocol
	// carrying them, so accepting them there would be config that silently does
	// nothing.

	// Title is a display name for people, where Name is the identifier the client
	// calls by. An MCP client shows this in a consent prompt or a tool picker.
	Title string `yaml:"title,omitempty"`
	// Annotations are hints about what calling this tool does, so a client can
	// decide how much ceremony to put in front of it.
	Annotations *ToolAnnotations `yaml:"annotations,omitempty"`
	// OutputSchema is the JSON Schema of what the tool branch returns. Declaring it
	// makes the router send the branch's result as structuredContent alongside the
	// text block, so a client can consume it as data rather than re-parsing prose.
	OutputSchema string `yaml:"outputSchema,omitempty"`
}

// ToolAnnotations are the MCP hints an "mcp-router" advertises about a tool.
//
// Every field is a pointer because "not stated" and "stated false" are different
// answers and the protocol's defaults are not all the same — readOnlyHint and
// idempotentHint default false, openWorldHint and destructiveHint default true.
// A bool would silently assert the default for every hint an author left out.
//
// They are hints, not enforcement. The protocol says clients must treat them as
// untrusted, and the runtime does not check a tool branch against them: a tool
// annotated readOnly can still write, and it is the author's job not to.
type ToolAnnotations struct {
	// ReadOnlyHint says the tool does not modify anything.
	ReadOnlyHint *bool `yaml:"readOnlyHint,omitempty"`
	// DestructiveHint says an update it performs may be destructive rather than
	// additive. Only meaningful when the tool is not read-only.
	DestructiveHint *bool `yaml:"destructiveHint,omitempty"`
	// IdempotentHint says calling it twice with the same arguments has no more
	// effect than calling it once. Only meaningful when the tool is not read-only.
	IdempotentHint *bool `yaml:"idempotentHint,omitempty"`
	// OpenWorldHint says the tool touches something outside the server — the web, a
	// third-party API — rather than a closed domain the server owns.
	OpenWorldHint *bool `yaml:"openWorldHint,omitempty"`
}

// SkillConfig is one skill available to an "ai-agent": a Name and Description
// advertised to the model up front, and Resource, the template resource whose
// rendered content the implicit load_skill tool returns when the model loads the
// skill. Skills keep the base prompt small while making deep, situational
// instructions available just-in-time.
type SkillConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Resource    string `yaml:"resource"`
}
