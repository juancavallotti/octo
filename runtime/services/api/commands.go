package api

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/services/api/openapi"
)

// The two CLI commands this module brings with it.
//
// They live here, and reach the CLI through the module's own registration,
// because both are about a contract only a binary carrying this module can
// speak. A standalone runtime that offered to print the platform API contract
// would be inviting somebody to implement an interface it has no provider for —
// and they would find out only after writing a server. Registering from here
// means the commands exist exactly where they are useful, with no build-tagged
// case in package main deciding it on their behalf.
//
// Somebody still deciding whether to implement the contract reads it on the
// documentation site, which is where that belongs.

// commandFileMode is the permission for a contract written to a file. git tracks
// only the executable bit, so this matches what schemafmt writes.
const commandFileMode = 0o600

// registerCommands is called from the module's init manifest in api.go.
func registerCommands() {
	services.RegisterCommand(openAPICommand{})
	services.RegisterCommand(verifyCommand{})
}

// openAPICommand prints the platform API contract this runtime expects.
type openAPICommand struct{}

func (openAPICommand) Name() string { return "openapi" }

func (openAPICommand) Usage() string {
	return `Platform API contract (octo openapi [--format json] [--out <path>]):
  --format <fmt>     yaml (default) or json
  --out <path>       write it to a file instead of stdout

  "octo openapi" prints the platform API contract: the OpenAPI document a server
  must implement for a runtime started with RUNTIME_SERVICES_MODULE=api, which is
  how Octo runs on Cloud Run, or against a platform service of your own, or beside
  a sidecar. The YAML carries the prose explaining each route; the JSON is for
  tooling that will not read it.`
}

func (c openAPICommand) Run(args []string) error {
	fs := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	format := fs.String("format", schemafmt.FormatYAML, "print it as yaml or json")
	out := fs.String("out", "", "write the contract to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(c.Usage())
			return nil
		}
		return fmt.Errorf("parse openapi flags: %w", err)
	}

	data, err := document(*format)
	if err != nil {
		return err
	}
	return write(data, *out)
}

// document renders the contract in the requested format.
//
// YAML is the default and comes back verbatim rather than through schemafmt,
// because the document's comments are half of it — they say why each route is
// shaped the way it is — and re-encoding would drop every one. JSON is for
// tooling that will not read YAML.
func document(format string) ([]byte, error) {
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

// write sends the document to path, or to stdout when path is empty.
func write(data []byte, path string) error {
	if path == "" {
		fmt.Print(string(data))
		return nil
	}
	if err := os.WriteFile(path, data, commandFileMode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// verifyCommand checks a platform API against the contract and prints what it
// found.
//
// It drives the real client, so the thing being verified is the code that will
// actually talk to the server: `docker run --rm juancavallotti/octo-api
// verify-platform-api https://my-api` is the artifact under test checking itself.
type verifyCommand struct{}

func (verifyCommand) Name() string { return "verify-platform-api" }

func (verifyCommand) Usage() string {
	return `Platform API verification (octo verify-platform-api <url> [--json]):
  --json             print the report as JSON instead of a table

  "octo verify-platform-api <url>" drives the real client against your
  implementation and prints, check by check, which contract rules it satisfies. It
  exits non-zero on any failure, so it belongs in the pipeline that ships your
  server. The URL may also come from OCTO_PLATFORM_API_URL, so running it inside a
  deployment needs no argument. It WRITES, under a scratch prefix it names before
  it starts; point it at staging.`
}

func (c verifyCommand) Run(args []string) error {
	fs := flag.NewFlagSet(c.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	asJSON := fs.Bool("json", false, "print the report as JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(c.Usage())
			return nil
		}
		return fmt.Errorf("parse verify-platform-api flags: %w", err)
	}

	// The URL may be given as an argument or through the same variable the
	// runtime itself reads, so verifying a deployment is a matter of running this
	// inside it.
	if url := fs.Arg(0); url != "" {
		if err := os.Setenv(URLEnvVar, url); err != nil {
			return fmt.Errorf("set %s: %w", URLEnvVar, err)
		}
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := Verify(ctx, cfg)
	if err != nil {
		return fmt.Errorf("verify %s: %w", cfg.BaseURL, err)
	}
	if err := printReport(report, *asJSON); err != nil {
		return err
	}
	// A non-zero exit is what makes this usable in CI: an implementation that
	// drifts from the contract fails the pipeline rather than the deployment.
	if report.Failed() {
		return errors.New("the platform API does not satisfy the contract this runtime expects")
	}
	return nil
}

// printReport writes the report in the requested shape.
func printReport(report VerifyReport, asJSON bool) error {
	if !asJSON {
		fmt.Print(report.Format())
		return nil
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the verification report: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}
