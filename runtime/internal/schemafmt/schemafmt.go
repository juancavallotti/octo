// Package schemafmt renders a JSON Schema document the way both CLIs print one.
//
// octo and dolphin each publish a schema — the capability catalogue and a
// --run-debug-config file for one, a _test.yaml suite for the other — and each
// offers the same two choices about it: print it as JSON or as YAML, to stdout or to
// a file. That is the whole of what lives here. The documents themselves are the
// CLIs' business; only the rendering is shared.
package schemafmt

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// The formats a schema can be printed in.
const (
	FormatJSON = "json"
	FormatYAML = "yaml"
)

// fileMode is the permission for a schema written to a file. git tracks only the
// executable bit, so this is owner-read/write; a committed file's mode is unaffected.
const fileMode = 0o600

// Format renders the JSON document in the requested format.
//
// The YAML conversion goes through a yaml.Node rather than a map, because a map would
// sort the keys and a schema read top to bottom is worth more than an alphabetized
// one: `type` and `description` belong above the properties they describe. JSON is a
// subset of YAML, so the node decodes straight from the JSON bytes with its order
// intact.
func Format(data []byte, format string) ([]byte, error) {
	switch format {
	case FormatJSON:
		return data, nil
	case FormatYAML:
		var node yaml.Node
		if err := yaml.Unmarshal(data, &node); err != nil {
			return nil, fmt.Errorf("read schema as yaml: %w", err)
		}
		blockStyle(&node)

		encoded, err := yaml.Marshal(&node)
		if err != nil {
			return nil, fmt.Errorf("encode schema as yaml: %w", err)
		}
		return encoded, nil
	default:
		return nil, fmt.Errorf("unknown format %q (want %q or %q)", format, FormatJSON, FormatYAML)
	}
}

// Write renders the document and sends it to path, or to stdout when path is empty.
// Both schema documents already end with a newline, so it prints them as they are.
func Write(data []byte, format, path string) error {
	rendered, err := Format(data, format)
	if err != nil {
		return err
	}
	if path == "" {
		fmt.Print(string(rendered))
		return nil
	}
	if err := os.WriteFile(path, rendered, fileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// blockStyle strips the flow style the JSON source carries, so the document is
// re-emitted as indented YAML rather than as the one long braced line it came in as.
// Parsing JSON as YAML records every mapping and sequence as flow-style, and Marshal
// honors that faithfully — which would hand back something that is YAML only in the
// sense that JSON already is.
func blockStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		blockStyle(child)
	}
}
