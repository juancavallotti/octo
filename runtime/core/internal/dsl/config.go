package dsl

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/types"
)

// LoadFile reads the file at path and parses it into a Config.
func LoadFile(path string) (types.Config, error) {
	// The config path is supplied by the operator, so reading it is intended.
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-provided config path
	if err != nil {
		return types.Config{}, fmt.Errorf("read config file %q: %w", path, err)
	}

	return Parse(data)
}

// Parse decodes YAML config bytes into a Config.
func Parse(data []byte) (types.Config, error) {
	var config types.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return types.Config{}, fmt.Errorf("parse config: %w", err)
	}

	return config, nil
}
