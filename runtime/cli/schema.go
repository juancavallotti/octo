package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/schema"
)

// schemaFileMode is the permission for a schema written via --out. git tracks only
// the executable bit, so this is owner-read/write; the committed file's mode is
// unaffected.
const schemaFileMode = 0o600

// The schemas `octo schema --kind` can print.
const (
	kindCapabilities = "capabilities"
	kindDebugConfig  = "debug-config"
)

// The formats `octo schema --format` can print them in.
const (
	formatJSON = "json"
	formatYAML = "yaml"
)

// debugConfigSchema is the JSON Schema for a --run-debug-config file. It is
// hand-written rather than generated: the schema reflector in core/schema produces
// octo's capability catalogue — a different document for a different consumer — and
// pulling in a JSON-Schema library to generate one static file is not worth the
// dependency. A drift test keeps it honest, failing when a field is added to the Go
// types without being described here.
//
//go:embed debugconfig.schema.json
var debugConfigSchema []byte

// schemaCommand prints a schema: by default the editor capability catalogue,
// generated from the registered block and connector metadata (every connector is
// blank-imported by main, so all metadata is registered by the time this runs), and
// with --kind debug-config the schema of a --run-debug-config file.
//
// During the spike only a subset of blocks carry metadata, so the capability output is
// a partial catalogue — intentionally. That is why this writes to stdout / an explicit
// path rather than overwriting the editor's capabilities.json.
func schemaCommand(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	kind := fs.String("kind", kindCapabilities, "which schema to print: capabilities or debug-config")
	format := fs.String("format", formatJSON, "print it as json or yaml")
	out := fs.String("out", "", "write the schema to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("parse schema flags: %w", err)
	}

	data, err := schemaDocument(*kind)
	if err != nil {
		return err
	}
	if data, err = formatSchema(data, *format); err != nil {
		return err
	}

	if *out != "" {
		if err := os.WriteFile(*out, data, schemaFileMode); err != nil {
			return fmt.Errorf("write %s: %w", *out, err)
		}
		return nil
	}
	// Both documents already end with a newline.
	fmt.Print(string(data))
	return nil
}

// schemaDocument returns the named schema as JSON.
func schemaDocument(kind string) ([]byte, error) {
	switch kind {
	case kindCapabilities:
		caps, err := schema.Generate(core.DefaultSchemaRegistry())
		if err != nil {
			return nil, fmt.Errorf("generate schema: %w", err)
		}
		data, err := schema.Marshal(caps)
		if err != nil {
			return nil, fmt.Errorf("marshal schema: %w", err)
		}
		return data, nil
	case kindDebugConfig:
		return debugConfigSchema, nil
	default:
		return nil, fmt.Errorf("unknown schema kind %q (want %q or %q)", kind, kindCapabilities, kindDebugConfig)
	}
}

// formatSchema renders the JSON document in the requested format.
//
// The YAML conversion goes through a yaml.Node rather than a map, because a map would
// sort the keys and a schema read top to bottom is worth more than an alphabetized
// one: `type` and `description` belong above the properties they describe. JSON is a
// subset of YAML, so the node decodes straight from the JSON bytes with its order
// intact.
func formatSchema(data []byte, format string) ([]byte, error) {
	switch format {
	case formatJSON:
		return data, nil
	case formatYAML:
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
		return nil, fmt.Errorf("unknown format %q (want %q or %q)", format, formatJSON, formatYAML)
	}
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
