package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juancavallotti/eip-go/core/internal/dsl"
	"github.com/juancavallotti/eip-go/types"
)

// LoadConfig reads and parses the runtime config at path. When path is a
// directory, every *.yaml/*.yml file in it is parsed and merged into one config
// (see MergeConfigs); otherwise the single file is parsed.
func LoadConfig(path string) (types.Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return types.Config{}, fmt.Errorf("stat config path %q: %w", path, err)
	}
	if info.IsDir() {
		return loadDir(path)
	}
	return dsl.LoadFile(path)
}

// ParseConfig parses the runtime config from raw config bytes.
func ParseConfig(data []byte) (types.Config, error) {
	return dsl.Parse(data)
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
	merged := types.Config{}
	serviceSet := false
	connectorNames := make(map[string]struct{})
	processorNames := make(map[string]struct{})
	flowNames := make(map[string]struct{})

	for _, cfg := range configs {
		if cfg.Service != (types.ServiceConfig{}) {
			if serviceSet {
				return types.Config{}, fmt.Errorf("service identity is declared in more than one config file")
			}
			merged.Service = cfg.Service
			serviceSet = true
		}

		for _, c := range cfg.Connectors {
			if _, dup := connectorNames[c.Name]; dup {
				return types.Config{}, fmt.Errorf("connector %q is defined more than once", c.Name)
			}
			connectorNames[c.Name] = struct{}{}
			merged.Connectors = append(merged.Connectors, c)
		}
		for _, p := range cfg.Processors {
			if _, dup := processorNames[p.Name]; dup {
				return types.Config{}, fmt.Errorf("processor %q is defined more than once", p.Name)
			}
			processorNames[p.Name] = struct{}{}
			merged.Processors = append(merged.Processors, p)
		}
		for _, f := range cfg.Flows {
			if f.Name != "" {
				if _, dup := flowNames[f.Name]; dup {
					return types.Config{}, fmt.Errorf("flow %q is defined more than once", f.Name)
				}
				flowNames[f.Name] = struct{}{}
			}
			merged.Flows = append(merged.Flows, f)
		}
	}

	return merged, nil
}
