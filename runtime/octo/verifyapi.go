//go:build api

package main

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

	"github.com/juancavallotti/octo/runtime/services/api"
)

// verifyPlatformAPICommand checks a platform API against the contract this
// runtime expects, and prints what it found.
//
// It lives in the -tags api binary rather than in every build, unlike `octo
// openapi`, because it does not read a document — it drives the real client. The
// thing being verified is the code that will actually talk to the server, so
// `docker run --rm juancavallotti/octo-api verify-platform-api https://my-api` is
// the artifact under test checking itself.
//
// It writes, to a scratch prefix it announces before it starts. Pointing it at
// staging is the better idea.
func verifyPlatformAPICommand(args []string) error {
	fs := flag.NewFlagSet("verify-platform-api", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	asJSON := fs.Bool("json", false, "print the report as JSON instead of a table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usageText())
			return nil
		}
		return fmt.Errorf("parse verify-platform-api flags: %w", err)
	}

	// The URL may be given as an argument or through the same environment
	// variable the runtime itself reads, so verifying a deployment is a matter of
	// running this in it.
	if url := fs.Arg(0); url != "" {
		if err := os.Setenv(api.URLEnvVar, url); err != nil {
			return fmt.Errorf("set %s: %w", api.URLEnvVar, err)
		}
	}
	cfg, err := api.LoadConfig()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	report, err := api.Verify(ctx, cfg)
	if err != nil {
		return fmt.Errorf("verify %s: %w", cfg.BaseURL, err)
	}
	if err := printVerifyReport(report, *asJSON); err != nil {
		return err
	}
	// A non-zero exit is what makes this usable in CI: an implementation that
	// drifts from the contract fails the pipeline rather than the deployment.
	if report.Failed() {
		return errors.New("the platform API does not satisfy the contract this runtime expects")
	}
	return nil
}

// printVerifyReport writes the report in the requested shape.
func printVerifyReport(report api.VerifyReport, asJSON bool) error {
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
