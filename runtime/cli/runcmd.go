package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/runtime"
	"github.com/juancavallotti/octo/services"
	"github.com/juancavallotti/octo/types"
)

// This file holds `octo run`: starting the configured connectors and flows, and
// reloading them on a config change under --watch.

// runCommand starts the configured connectors and flows until interrupted. With
// --watch it reloads on config changes.
func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	watch := fs.Bool("watch", false, "reload the config when it changes")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("parse run flags: %w", err)
	}
	if *configPath == "" {
		return errors.New("config path is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build the runtime services once and reuse them across watch-mode reloads, so
	// leases and connections are not churned on every config change. The CLI owns
	// their lifecycle; each Service generation only borrows them. The resource root
	// (the config directory) roots the standalone module's resource loader.
	svc, err := services.New(ctx, services.Options{ResourceRoot: configDir(*configPath)})
	if err != nil {
		return fmt.Errorf("init runtime services: %w", err)
	}
	defer func() { _ = svc.Close() }()
	teeDefaultLoggerToSink(svc)
	slog.Info("runtime services ready", "module", services.Module())

	if *watch {
		return runWithReload(ctx, *configPath, svc)
	}
	return runOnce(ctx, *configPath, svc)
}

// runOnce loads the config and runs a single service generation.
func runOnce(ctx context.Context, configPath string, svc core.RuntimeServices) error {
	config, err := runtime.LoadConfig(configPath, svc.Resources())
	if err != nil {
		return err
	}
	slog.Info("starting runtime", "version", Version, "connectors", len(config.Connectors), "flows", len(config.Flows))

	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithRuntimeServices(svc))
	go announceWhenReady(ctx, service)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	slog.Info("runtime stopped")
	return nil
}

// announceWhenReady prints the friendly startup banner once the service reports
// it is ready (every connector and flow started). It returns without printing if
// the context is cancelled first — including a failed startup, where the run path
// surfaces the error and cancels the context on the way out.
func announceWhenReady(ctx context.Context, service *runtime.Service) {
	select {
	case <-service.Started():
		fmt.Println(readyBanner())
	case <-ctx.Done():
	}
}

// runWithReload runs the service, tearing it down and rebuilding from the config
// whenever the watched path changes, until ctx is cancelled. A config that fails
// to load leaves the previous generation stopped and waits for the next change.
func runWithReload(ctx context.Context, configPath string, svc core.RuntimeServices) error {
	// The services' resource loader backs env-resource combination during load and
	// the blocks' resource reads, and (as a ResourceWatcher) drives reloads.
	loader := svc.Resources()
	changed, err := watchConfig(ctx, configPath, loader)
	if err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	slog.Info("watching config for changes", "path", configPath)

	for {
		config, loadErr := runtime.LoadConfig(configPath, loader)
		if loadErr != nil {
			slog.Error("config load failed, waiting for next change", "error", loadErr)
			if !waitForChange(ctx, changed) {
				return nil
			}
			continue
		}

		reload, runErr := runGeneration(ctx, config, changed, svc)
		if runErr != nil {
			// In watch mode a build/start failure is not fatal: keep the watcher
			// alive and wait for the next change, same as a config load error, so
			// fixing the file recovers without restarting the process.
			slog.Error("runtime failed, waiting for next change", "error", runErr)
			if !waitForChange(ctx, changed) {
				return nil
			}
			continue
		}
		if !reload {
			slog.Info("runtime stopped")
			return nil
		}
	}
}

// runGeneration runs one service generation and returns whether the caller should
// reload (rebuild from config) or stop.
func runGeneration(
	ctx context.Context, config types.Config, changed <-chan struct{}, svc core.RuntimeServices,
) (bool, error) {
	slog.Info("starting runtime", "version", Version, "connectors", len(config.Connectors), "flows", len(config.Flows))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithRuntimeServices(svc))
	go announceWhenReady(runCtx, service)
	go func() { done <- service.Run(runCtx) }()

	select {
	case <-ctx.Done():
		<-done
		return false, nil
	case <-changed:
		slog.Info("config changed, reloading")
		cancel()
		<-done
		return true, nil
	case runErr := <-done:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return false, runErr
		}
		// The service exited on its own without an error; wait for a change
		// before rebuilding so we do not spin.
		return waitForChange(ctx, changed), nil
	}
}

// waitForChange blocks until a config change arrives (true) or ctx is cancelled
// (false).
func waitForChange(ctx context.Context, changed <-chan struct{}) bool {
	select {
	case <-ctx.Done():
		return false
	case <-changed:
		return true
	}
}
