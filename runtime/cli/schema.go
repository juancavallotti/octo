package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/schema"
)

// schemaCommand generates the editor capability catalogue from the registered
// block and connector metadata and prints it as JSON, or writes it to --out.
// Every connector is blank-imported by main, so all metadata is registered by
// the time this runs.
//
// During the spike only a subset of blocks carry metadata, so the output is a
// partial catalogue — intentionally. That is why this writes to stdout / an
// explicit path rather than overwriting the editor's capabilities.json; wiring a
// go:generate that replaces the live file waits until every block is migrated.
func schemaCommand(args []string) error {
	fs := flag.NewFlagSet("schema", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	out := fs.String("out", "", "write the JSON to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("parse schema flags: %w", err)
	}

	caps, err := schema.Generate(core.DefaultSchemaRegistry())
	if err != nil {
		return fmt.Errorf("generate schema: %w", err)
	}
	data, err := schema.Marshal(caps)
	if err != nil {
		return fmt.Errorf("marshal schema: %w", err)
	}

	if *out != "" {
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", *out, err)
		}
		return nil
	}
	// Marshal already ends the JSON with a newline.
	fmt.Print(string(data))
	return nil
}
