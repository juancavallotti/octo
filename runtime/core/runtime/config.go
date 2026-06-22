package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/core/internal/dsl"
	"github.com/juancavallotti/octo/types"
)

// LoadConfig reads and parses the runtime config at path. When path is a
// directory, every *.yaml/*.yml file in it is parsed and merged into one config
// (see MergeConfigs); otherwise the single file is parsed.
func LoadConfig(path string) (types.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return types.Config{}, fmt.Errorf("stat config path %q: %w", path, err)
	}

	var cfg types.Config
	if info.IsDir() {
		cfg, err = loadDir(path)
	} else {
		cfg, err = dsl.LoadFile(path)
	}
	if err != nil {
		return types.Config{}, err
	}

	if err := applyEnv(&cfg); err != nil {
		return types.Config{}, err
	}
	return cfg, nil
}

// ParseConfig parses the runtime config from raw config bytes, resolving declared
// environment variables and substituting ${NAME} references.
func ParseConfig(data []byte) (types.Config, error) {
	cfg, err := dsl.Parse(data)
	if err != nil {
		return types.Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return types.Config{}, err
	}
	return cfg, nil
}

// loadDir parses and merges every YAML config file in dir. Files are loaded in
// lexical order so duplicate-name errors are deterministic.
func loadDir(dir string) (types.Config, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return types.Config{}, fmt.Errorf("read config dir %q: %w", dir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	if len(paths) == 0 {
		return types.Config{}, fmt.Errorf("no .yaml/.yml config files in %q", dir)
	}
	sort.Strings(paths)

	configs := make([]types.Config, 0, len(paths))
	for _, p := range paths {
		cfg, loadErr := dsl.LoadFile(p)
		if loadErr != nil {
			return types.Config{}, loadErr
		}
		configs = append(configs, cfg)
	}
	return MergeConfigs(configs)
}

// isYAML reports whether name has a YAML extension.
func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}

// MergeConfigs combines multiple parsed configs into one: it concatenates
// connectors, processors, and flows, rejecting duplicate names so every
// connector, processor, and flow stays uniquely addressable. The service
// identity may be declared in at most one file.
func MergeConfigs(configs []types.Config) (types.Config, error) {
	m := newConfigMerger()
	for _, cfg := range configs {
		if err := m.add(cfg); err != nil {
			return types.Config{}, err
		}
	}
	return m.merged, nil
}

// configMerger accumulates configs while enforcing unique names per kind.
type configMerger struct {
	merged     types.Config
	serviceSet bool
	connectors map[string]struct{}
	processors map[string]struct{}
	flows      map[string]struct{}
	env        map[string]struct{}
}

func newConfigMerger() *configMerger {
	return &configMerger{
		connectors: make(map[string]struct{}),
		processors: make(map[string]struct{}),
		flows:      make(map[string]struct{}),
		env:        make(map[string]struct{}),
	}
}

// add folds one config into the merge, rejecting duplicate names and a second
// service declaration.
func (m *configMerger) add(cfg types.Config) error {
	if err := m.addService(cfg.Service); err != nil {
		return err
	}
	for _, e := range cfg.Env {
		// Duplicate declarations across files are allowed (the same variable may be
		// used in several configs); the first declaration wins.
		if _, dup := m.env[e.Name]; dup {
			continue
		}
		m.env[e.Name] = struct{}{}
		m.merged.Env = append(m.merged.Env, e)
	}
	for _, c := range cfg.Connectors {
		if err := claimName(m.connectors, c.Name, "connector"); err != nil {
			return err
		}
		m.merged.Connectors = append(m.merged.Connectors, c)
	}
	for _, p := range cfg.Processors {
		if err := claimName(m.processors, p.Name, "processor"); err != nil {
			return err
		}
		m.merged.Processors = append(m.merged.Processors, p)
	}
	for _, f := range cfg.Flows {
		if f.Name != "" {
			if err := claimName(m.flows, f.Name, "flow"); err != nil {
				return err
			}
		}
		m.merged.Flows = append(m.merged.Flows, f)
	}
	return nil
}

// addService records the service identity, rejecting a second declaration.
func (m *configMerger) addService(svc types.ServiceConfig) error {
	if svc == (types.ServiceConfig{}) {
		return nil
	}
	if m.serviceSet {
		return fmt.Errorf("service identity is declared in more than one config file")
	}
	m.merged.Service = svc
	m.serviceSet = true
	return nil
}

// claimName records name in seen, erroring if it was already present. kind labels
// the error.
func claimName(seen map[string]struct{}, name, kind string) error {
	if _, dup := seen[name]; dup {
		return fmt.Errorf("%s %q is defined more than once", kind, name)
	}
	seen[name] = struct{}{}
	return nil
}
