package types

// Config is the top-level runtime configuration loaded from a config file.
type Config struct {
	Service    ServiceConfig     `yaml:"service"`
	Connectors []ConnectorConfig `yaml:"connectors"`
	Flows      []FlowConfig      `yaml:"flows,omitempty"`
}

// ServiceConfig describes the runtime service identity and environment.
type ServiceConfig struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment,omitempty"`
}

// ConnectorConfig describes a single connector instance and its settings.
type ConnectorConfig struct {
	Name     string         `yaml:"name"`
	Type     string         `yaml:"type"`
	Settings map[string]any `yaml:"settings,omitempty"`
}
