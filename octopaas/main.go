// Command octopaas is the command line for an Octo platform's public API.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/juancavallotti/octo/octopaas/internal/cmd"
)

// BuildDate is set at link time by the release build; a local build falls back
// on the commit time Go records.
var BuildDate string

func main() {

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	app := &cmd.App{
		Version:   Version,
		BuildDate: buildDate(),
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		Getenv:    os.Getenv,
	}
	code := cmd.Execute(ctx, app, os.Args[1:])
	stop()
	os.Exit(code)
}

func buildDate() string {

	if BuildDate != "" {
		return BuildDate
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" {
				return s.Value
			}
		}
	}
	return "unknown"
}
