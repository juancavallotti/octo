package dsl

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/types"
)

// LoadFile reads the file at path and parses it into a Config.
func LoadFile(path string) (types.Config, error) {
	data, err := ReadFile(path)
	if err != nil {
		return types.Config{}, err
	}

	return Parse(data)
}

// ReadFile reads the raw config bytes at path.
func ReadFile(path string) ([]byte, error) {
	// The config path is supplied by the operator, so reading it is intended.
	data, err := os.ReadFile(path) //nolint:gosec // G304: operator-provided config path
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	return data, nil
}

// Parse decodes YAML config bytes into a Config.
func Parse(data []byte) (types.Config, error) {
	doc, err := ParseDocument(data)
	if err != nil {
		return types.Config{}, err
	}
	return Decode(doc)
}

// ParseDocument parses YAML config bytes into their node tree, leaving every
// value as written. Callers that rewrite the document before it is typed — env
// substitution does, so a ${NAME} can fill an int field like workers — parse
// through here and Decode the result. An empty document yields a zero node,
// which Decode turns into a zero Config.
func ParseDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &doc, nil
}

// Decode types a parsed document into a Config.
func Decode(doc *yaml.Node) (types.Config, error) {
	var config types.Config
	// An empty file parses to a zero node, which has nothing to decode. Say so
	// rather than leaning on the decoder's handling of a node it never produced.
	if doc == nil || doc.Kind == 0 {
		return config, nil
	}
	if err := doc.Decode(&config); err != nil {
		return types.Config{}, fmt.Errorf("parse config: %w", err)
	}

	return config, nil
}
