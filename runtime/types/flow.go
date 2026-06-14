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
	Connector string         `yaml:"connector"`
	Type      string         `yaml:"type"`
	Settings  map[string]any `yaml:"settings,omitempty"`
}

// BlockConfig describes one step in a flow. Leaf blocks use only Type, Name, and
// Settings. Composite kinds use explicit typed slots: a "scope" populates Main
// and optionally Alternative; a "fork" populates Branches. The Flow<->Block
// recursion (FlowConfig.Process -> []BlockConfig -> Main/Alternative/Branches ->
// FlowConfig) lets the parser build the whole tree in one pass.
type BlockConfig struct {
	Type     string         `yaml:"type"`
	Name     string         `yaml:"name,omitempty"`
	Settings map[string]any `yaml:"settings,omitempty"`

	// Main is the protected flow of a "scope" block.
	Main *FlowConfig `yaml:"main,omitempty"`
	// Alternative is the recovery flow of a "scope" block.
	Alternative *FlowConfig `yaml:"alternative,omitempty"`
	// Branches are the parallel flows of a "fork" block.
	Branches []FlowConfig `yaml:"branches,omitempty"`
}
