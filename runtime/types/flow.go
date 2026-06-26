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
// "foreach" populates Items/As/Body. The AI composites use Connector/Prompt and
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
	// Body is the flow a "foreach" or "cache-scope" block runs; foreach runs it
	// once per element, cache-scope runs it on a cache miss.
	Body *FlowConfig `yaml:"body,omitempty"`

	// Key is the cache-key expression of a "cache-scope" block (evaluated per
	// message). TTL is how long a cached entry stays fresh, a duration string
	// ("5m"); empty uses the default and "0" never expires.
	Key string `yaml:"key,omitempty"`
	TTL string `yaml:"ttl,omitempty"`

	// Connector names the LLM connector the AI composites (ai-router, ai-agent,
	// ai-retry) call through.
	Connector string `yaml:"connector,omitempty"`
	// Prompt is the routing/task/repair instruction given to the model by the AI
	// composites.
	Prompt string `yaml:"prompt,omitempty"`
	// Guardrail describes when the model should fall back to the Default path; it
	// is used by ai-router and ai-agent.
	Guardrail string `yaml:"guardrail,omitempty"`
	// Routes are the named, described branches of an "ai-router" block. The model
	// picks one; Default is the guardrail taken when it is not confident.
	Routes []RouteConfig `yaml:"routes,omitempty"`
	// Tools are the named, described branches of an "ai-agent" block, each wired
	// to the model as a callable function.
	Tools []ToolConfig `yaml:"tools,omitempty"`
	// MaxIterations caps how many tool-calling turns an "ai-agent" runs before
	// falling back to the guardrail (default applied by the builder).
	MaxIterations int `yaml:"maxIterations,omitempty"`
	// MaxAttempts caps how many times an "ai-retry" re-runs its Process chain
	// after an LLM-driven revision before falling through to Error (default
	// applied by the builder).
	MaxAttempts int `yaml:"maxAttempts,omitempty"`
}

// CaseConfig is one branch of a "switch" block: a boolean When expression and an
// inline flow (its process chain and optional name) to run when When is the first
// case to evaluate true.
type CaseConfig struct {
	When string     `yaml:"when"`
	Flow FlowConfig `yaml:",inline"`
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
}
