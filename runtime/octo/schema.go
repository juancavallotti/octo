package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/schema"
	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
)

// The schemas `octo schema --kind` can print.
const (
	kindCapabilities = "capabilities"
	kindDebugConfig  = "debug-config"
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
	format := fs.String("format", schemafmt.FormatJSON, "print it as json or yaml")
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
	return schemafmt.Write(data, *format, *out)
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
