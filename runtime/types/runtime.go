package types

// Config is the top-level runtime configuration loaded from a config file.
type Config struct {
	Service ServiceConfig `yaml:"service"`
	// Env declares the environment variables this config may reference as ${NAME}
	// in settings values. Referencing an undeclared variable is an error.
	Env        []EnvVar          `yaml:"env,omitempty"`
	Connectors []ConnectorConfig `yaml:"connectors"`
	// Processors holds reusable, named processor definitions that flow blocks
	// reference by name via BlockConfig.Ref, mirroring how Connectors are
	// declared once and referenced by a flow's source.
	Processors []ProcessorConfig `yaml:"processors,omitempty"`
	Flows      []FlowConfig      `yaml:"flows,omitempty"`

	// Resources declares the external resources this config imports. Currently
	// only env resources are declared (the .env-convention files combined into the
	// runtime environment); template resources are resolved on demand by id.
	Resources ResourcesConfig `yaml:"resources,omitempty"`

	// ResolvedEnv holds the declared environment variables resolved to their
	// values (the same map used for ${NAME} substitution), so expressions can
	// read them as env.NAME. Populated during config load; not serialized.
	ResolvedEnv map[string]string `yaml:"-"`
}

// ResourcesConfig declares the external resources a config imports. Env lists the
// ids of .env-convention resources to combine into the runtime environment, in
// order (later ids overlay earlier ones). Template resources are not declared
// here; they are resolved on demand by id.
type ResourcesConfig struct {
	Env []string `yaml:"env,omitempty"`
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
