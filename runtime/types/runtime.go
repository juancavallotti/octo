package types

// Config is the top-level runtime configuration loaded from a config file.
type Config struct {
	Service    ServiceConfig     `yaml:"service"`
	Connectors []ConnectorConfig `yaml:"connectors"`
	// Processors holds reusable, named processor definitions that flow blocks
	// reference by name via BlockConfig.Ref, mirroring how Connectors are
	// declared once and referenced by a flow's source.
	Processors []ProcessorConfig `yaml:"processors,omitempty"`
	Flows      []FlowConfig      `yaml:"flows,omitempty"`
}

// ServiceConfig describes the runtime service identity and environment.
type ServiceConfig struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment,omitempty"`
}

// ConnectorConfig describes a single connector instance and its settings.
type ConnectorConfig struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Settings Settings `yaml:"settings,omitempty"`
}

// ProcessorConfig is a reusable, named processor definition. Flow blocks select
// one by name through BlockConfig.Ref; the block's effective type is this Type
// and its effective settings are these Settings shallow-merged with any
// block-level overrides.
type ProcessorConfig struct {
	Name     string   `yaml:"name"`
	Type     string   `yaml:"type"`
	Settings Settings `yaml:"settings,omitempty"`
}
