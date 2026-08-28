package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
	"github.com/juancavallotti/octo/runtime/services/api/openapi"
)

// openapiFileMode is the permission for a contract written to a file. git tracks
// only the executable bit, so this matches what schemafmt writes.
const openapiFileMode = 0o600

// openapiCommand prints the platform API contract — the OpenAPI document a server
// must implement for a runtime started with RUNTIME_SERVICES_MODULE=api.
//
// It is in EVERY build, not only the one with -tags api, and that is the point.
// The openapi package is data with no registration and no dependencies, so it
// costs an untagged binary nothing to carry — and a person deciding whether to
// implement this contract should be able to read it from whatever octo they
// already have, version-matched to the runtime they will run, rather than from
// whatever the website last published. `octo schema` is in every build for the
// same reason.
func openapiCommand(args []string) error {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	format := fs.String("format", schemafmt.FormatYAML, "print it as yaml or json")
	out := fs.String("out", "", "write the contract to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usageText())
			return nil
		}
		return fmt.Errorf("parse openapi flags: %w", err)
	}

	data, err := openapiDocument(*format)
	if err != nil {
		return err
	}
	return writeOpenAPI(data, *out)
}

// openapiDocument renders the contract in the requested format.
//
// YAML is the default and comes back verbatim rather than through schemafmt,
// because the document's comments are half of it — they say why each route is
// shaped the way it is — and re-encoding would drop every one. JSON is for
// tooling that will not read YAML.
func openapiDocument(format string) ([]byte, error) {
	switch format {
	case schemafmt.FormatYAML:
		return openapi.Spec(), nil
	case schemafmt.FormatJSON:
		data, err := openapi.SpecJSON()
		if err != nil {
			return nil, fmt.Errorf("render the platform API contract: %w", err)
		}
		return data, nil
	default:
		return nil, fmt.Errorf("unknown format %q (want %q or %q)",
			format, schemafmt.FormatYAML, schemafmt.FormatJSON)
	}
}

// writeOpenAPI sends the document to path, or to stdout when path is empty.
func writeOpenAPI(data []byte, path string) error {
	if path == "" {
		fmt.Print(string(data))
		return nil
	}
	if err := os.WriteFile(path, data, openapiFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
