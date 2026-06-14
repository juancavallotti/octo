package dsl

import (
	"os"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/eip-go/types"
)

func LoadFile(path string) (types.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return types.Config{}, err
	}

	return Parse(data)
}

func Parse(data []byte) (types.Config, error) {
	var config types.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return types.Config{}, err
	}

	return config, nil
}
