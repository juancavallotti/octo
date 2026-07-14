package main

// This file holds `dolphin schema`: the shape of a _test.yaml, as JSON Schema, so an
// editor can complete one and an agent can write one without having read the source.

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
)

// testConfigSchema is the JSON Schema for a _test.yaml. It is hand-written rather than
// generated: pulling in a JSON-Schema reflector to produce one static file is not worth
// the dependency, and the descriptions here carry the rules a generated schema could not
// know — that `body` is exact while `vars` is a subset, that `count: 0` is an assertion,
// that an unmatched mock fails the block rather than falling through to the real one.
//
// A drift test keeps it honest, walking the Go structs and this document together, so a
// field added to either side without the other fails CI.
//
//go:embed testconfig.schema.json
var testConfigSchema []byte

// schemaCommand prints the schema of a dolphin test file.
func schemaCommand(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	format := fs.String("format", schemafmt.FormatJSON, "print it as json or yaml")
	out := fs.String("out", "", "write the schema to this file instead of stdout")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return usageErr("parse schema flags: %w", err)
	}

	if err := schemafmt.Write(testConfigSchema, *format, *out); err != nil {
		return usageErr("%w", err)
	}
	return nil
}
