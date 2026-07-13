package main

// This file holds `dolphin run`. Today it is a preflight: it resolves the octo
// binary, proves it runs, and reports what it would test against — everything a
// suite needs before the first case executes. The cases themselves come next, and
// the config file's shape is deliberately still open, so nothing here reads it.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// errNotImplemented is what `run` fails with once the preflight has passed. It is a
// failure, and loudly so: a stub that exited 0 would go green in someone's CI and
// stay green for weeks without ever running a case, which is worse than having no
// suite at all.
var errNotImplemented = errors.New(
	"running the test cases is not implemented yet — dolphin found octo and read your flags, " +
		"but the config format is still being designed")

// runCommand runs the cases in a dolphin test config.
func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	configPath := fs.String("config", "", "path to the dolphin test config")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return usageErr("parse run flags: %w", err)
	}
	if *configPath == "" {
		return usageErr("config path is required")
	}
	// The config is checked for existence but not parsed: catching a typo'd path here
	// is worth it, and its contents are next iteration's problem.
	if _, err := os.Stat(*configPath); err != nil {
		return usageErr("read config %q: %w", *configPath, err)
	}

	bin, err := resolveOcto()
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}
	octoVersion, err := bin.version(context.Background())
	if err != nil {
		return &exitError{code: exitUsage, err: err}
	}

	printPreflight(bin, octoVersion, *configPath)
	return &exitError{code: exitNotImplemented, err: errNotImplemented}
}

// printPreflight reports what the run resolved — which octo, found how, and against
// what config — so that when a suite behaves unexpectedly, the first question
// ("which binary did it even use?") is already answered on screen.
func printPreflight(bin octoBin, octoVersion, configPath string) {
	fmt.Println(versionLine())
	fmt.Printf("  octo binary   %s  (from %s)\n", bin.Path, bin.Source)
	fmt.Printf("  octo version  %s\n", octoVersion)
	fmt.Printf("  config        %s\n", configPath)
}
