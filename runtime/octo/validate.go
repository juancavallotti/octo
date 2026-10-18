package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/runtime"
	"github.com/juancavallotti/octo/runtime/services"
)

// This file holds `octo validate`: loading a config the way `run` would and checking
// it statically, without starting a connector or a flow.

// validateOutcome is the envelope `octo validate --format json` prints. OK is true
// exactly when Issues is empty; Issues is never null, so a consumer can range over it
// without a nil check.
//
// UnsetEnv lists the required environment variables nothing supplied. They are not
// issues: validation stands in an empty value for them, because a draft is checked
// where its secrets are not, and the place that runs it is the one that supplies
// them.
type validateOutcome struct {
	OK       bool            `json:"ok"`
	Issues   []runtime.Issue `json:"issues"`
	UnsetEnv []string        `json:"unsetEnv,omitempty"`
}

// validateCommand loads the config at --config — one file, or a directory merged the
// way `run` merges it — and reports what runtime.Validate finds. A config that fails
// to load is reported as one issue with no location, since the load stops at the
// first problem. It exits non-zero when there is any issue, so CI can gate on it; the
// JSON envelope is printed either way.
func validateCommand(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	format := fs.String("format", "text", "print the issues as text or json")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usageText())
			return nil
		}
		return fmt.Errorf("parse validate flags: %w", err)
	}
	if *configPath == "" {
		return errors.New("validate: --config is required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("validate: unknown format %q (want text or json)", *format)
	}

	outcome, err := validateConfig(*configPath)
	if err != nil {
		return err
	}
	if err := printOutcome(os.Stdout, *format, outcome); err != nil {
		return err
	}
	if !outcome.OK {
		return fmt.Errorf("config has %d issue(s)", len(outcome.Issues))
	}
	return nil
}

// validateConfig loads and checks the config. The runtime services are built only
// for their resource loader: env resources and templates load through it, exactly
// as they do for `run`.
func validateConfig(configPath string) (validateOutcome, error) {
	svc, err := services.New(context.Background(), services.Options{ResourceRoot: configDir(configPath)})
	if err != nil {
		return validateOutcome{}, fmt.Errorf("init runtime services: %w", err)
	}
	defer func() { _ = svc.Close() }()

	outcome := validateOutcome{Issues: []runtime.Issue{}}
	cfg, err := runtime.LoadConfig(configPath, svc.Resources(), runtime.WithUnsetEnv(func(name string) {
		outcome.UnsetEnv = append(outcome.UnsetEnv, name)
	}))
	if err != nil {
		outcome.Issues = append(outcome.Issues, runtime.Issue{Message: err.Error()})
		return outcome, nil
	}
	issues, err := runtime.Validate(cfg, core.DefaultRegistry(), core.DefaultBlockRegistry(), core.DefaultSchemaRegistry(),
		svc.Resources())
	if err != nil {
		return validateOutcome{}, err
	}
	outcome.Issues = append(outcome.Issues, issues...)
	outcome.OK = len(outcome.Issues) == 0
	return outcome, nil
}

func printOutcome(w io.Writer, format string, outcome validateOutcome) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(outcome); err != nil {
			return fmt.Errorf("encode outcome: %w", err)
		}
		return nil
	}
	lines := make([]string, 0, len(outcome.Issues)+len(outcome.UnsetEnv))
	for _, issue := range outcome.Issues {
		line := issue.Message
		if issue.Where != "" {
			line = issue.Where + ": " + line
		}
		lines = append(lines, line)
	}
	for _, name := range outcome.UnsetEnv {
		lines = append(lines, fmt.Sprintf(
			"note: required environment variable %q is not set (checked with an empty value)", name))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("print outcome: %w", err)
		}
	}
	return nil
}
