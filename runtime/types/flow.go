package types

// FlowConfig is the recursive unit of pipeline composition. The root flow,
// listed under Config.Flows, binds a Source and a worker-pool size; sub-flows
// nested inside a composite block reuse the same shape but must not set Source,
// Workers, or Buffer (the core builder validates this).
type FlowConfig struct {
	Name    string        `yaml:"name,omitempty"`
	Source  *SourceConfig `yaml:"source,omitempty"`
	Process []BlockConfig `yaml:"process"`
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
// Settings. Composite kinds use explicit typed slots: a "scope" populates Main
// and optionally Alternative; a "fork" populates Branches; an "if" populates
// Condition/Then/Else; a "switch" populates Cases and optionally Default; a
// "foreach" populates Items/As/Body. The Flow<->Block recursion (FlowConfig.Process
// -> []BlockConfig -> the composite slots -> FlowConfig) lets the parser build the
// whole tree in one pass.
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

	// Main is the protected flow of a "scope" block.
	Main *FlowConfig `yaml:"main,omitempty"`
	// Alternative is the recovery flow of a "scope" block.
	Alternative *FlowConfig `yaml:"alternative,omitempty"`
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
	// Body is the flow a "foreach" block runs once per element.
	Body *FlowConfig `yaml:"body,omitempty"`
}

// CaseConfig is one branch of a "switch" block: a boolean When expression and an
// inline flow (its process chain and optional name) to run when When is the first
// case to evaluate true.
type CaseConfig struct {
	When string     `yaml:"when"`
	Flow FlowConfig `yaml:",inline"`
}
