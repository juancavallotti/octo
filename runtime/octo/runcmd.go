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
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/runtime"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/services/tracing"
	"github.com/juancavallotti/octo/runtime/types"
)

// This file holds `octo run`: starting the configured connectors and flows, and
// reloading them on a config change under --watch.

// hostedStopTimeout bounds how long the hosted runtime services get to shut down
// between them. It is generous, because the point is to cap a service that has
// wedged rather than to hurry one that is draining.
const hostedStopTimeout = 30 * time.Second

// runCommand starts the configured connectors and flows until interrupted. With
// --watch it reloads on config changes.
func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	watch := fs.Bool("watch", false, "reload the config when it changes")
	// Hosted runtime services bring their own flags — they have no YAML to
	// configure them — so they must register before the parse that would reject an
	// unknown flag.
	hosted := services.Hosted()
	for _, service := range hosted {
		service.Flags(fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usageText())
			return nil
		}
		return fmt.Errorf("parse run flags: %w", err)
	}
	if *configPath == "" {
		return errors.New("config path is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the hosted services first, before anything that can take a while. A
	// readiness probe answering "starting" through connector startup is the point of
	// having one; a probe that refuses the connection until the runtime is up says
	// nothing a TCP check could not.
	health := services.NewHealth()
	stopHosted, err := startHosted(ctx, hosted, health)
	if err != nil {
		return err
	}
	defer stopHosted()

	// Readiness drops the moment a signal lands, before flows drain, so a load
	// balancer stops sending work while there are still workers to finish what is in
	// flight. That gap is most of the value of separating readiness from liveness.
	context.AfterFunc(ctx, func() { health.SetState(services.StateDraining) })

	// Build the runtime services once and reuse them across watch-mode reloads, so
	// leases and connections are not churned on every config change. The CLI owns
	// their lifecycle; each Service generation only borrows them. The resource root
	// (the config directory) roots the standalone module's resource loader.
	svc, err := services.New(ctx, services.Options{
		ResourceRoot: configDir(*configPath),
		// Where traces go is the module's choice — a file for standalone, a
		// broker subject for k8s — so it builds the publisher along with its
		// queues and its store, from the flags the hosted service resolved above.
		Tracing: tracing.Options(),
	})
	if err != nil {
		return fmt.Errorf("init runtime services: %w", err)
	}
	// Closing the module is what drains and closes the trace sink, so a failure
	// here means an incomplete trace — the one thing the person who asked for it
	// cannot find out any other way.
	defer func() {
		if closeErr := svc.Close(); closeErr != nil {
			slog.Error("closing runtime services", "error", closeErr)
		}
	}()
	teeDefaultLoggerToSink(svc)
	// Hand the module's publisher to the process. Everything that emits a trace
	// looks it up here — the engine, the connectors, the sources — and none of
	// them has the runtime services in hand. Nothing has emitted anything yet:
	// core.Tracer() is the no-op until this line, and no flow runs until below.
	core.SetTracer(svc.Traces())
	slog.Info("runtime services ready", "module", services.Module())

	if *watch {
		return runWithReload(ctx, *configPath, svc, hosted, health)
	}
	return runOnce(ctx, *configPath, svc, hosted, health)
}

// startHosted starts every hosted service in registration order and returns the
// function that stops them in reverse. A service that fails to start stops the
// ones already up and fails the run: binding a port is a startup error, not a log
// line to be discovered later.
func startHosted(ctx context.Context, hosted []services.HostedService, health *services.Health) (func(), error) {
	started := make([]services.HostedService, 0, len(hosted))
	stop := func() {
		// Shutdown runs after the run context is cancelled, so it needs a context
		// that is not already done — otherwise a graceful drain has nowhere to
		// happen. It is bounded all the same: a service that will not stop must not
		// be able to hold the process open forever.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), hostedStopTimeout)
		defer cancel()
		for i := len(started) - 1; i >= 0; i-- {
			if err := started[i].Stop(stopCtx); err != nil {
				slog.Error("hosted service failed to stop", "service", started[i].Name(), "error", err)
			}
		}
	}

	for _, service := range hosted {
		if err := service.Start(ctx, health); err != nil {
			stop()
			return nil, fmt.Errorf("start %s service: %w", service.Name(), err)
		}
		started = append(started, service)
	}
	return stop, nil
}

// announceConfig hands the loaded config to every hosted service that asked to see
// it. It is called once per generation, so a service must treat it as "the config
// is now this" rather than "begin".
func announceConfig(hosted []services.HostedService, config types.Config) {
	for _, service := range hosted {
		if aware, ok := service.(services.ConfigAware); ok {
			aware.Configured(config)
		}
	}
}

// runOnce loads the config and runs a single service generation.
func runOnce(
	ctx context.Context, configPath string, svc core.RuntimeServices,
	hosted []services.HostedService, health *services.Health,
) error {
	config, err := runtime.LoadConfig(configPath, svc.Resources())
	if err != nil {
		return err
	}
	slog.Info("starting runtime", "version", Version, "connectors", len(config.Connectors), "flows", len(config.Flows))
	announceConfig(hosted, config)

	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithRuntimeServices(svc))
	go announceWhenReady(ctx, service, health)
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		health.SetState(services.StateStopped)
		return err
	}
	health.SetState(services.StateStopped)
	slog.Info("runtime stopped")
	return nil
}

// announceWhenReady prints the friendly startup banner and opens the readiness
// gate once the service reports it is ready — every connector and flow started,
// which for an http-backed flow includes its listener being bound. It returns
// without doing either if the context is cancelled first, including a failed
// startup, where the run path surfaces the error and cancels the context on the
// way out.
func announceWhenReady(ctx context.Context, service *runtime.Service, health *services.Health) {
	select {
	case <-service.Started():
		// Both channels can be ready at once, and select picks between them at
		// random. A generation that is already being torn down must not reopen
		// readiness on its way out, so re-check: a cancelled context here means
		// this announcement is stale before it is written.
		if ctx.Err() != nil {
			return
		}
		health.SetState(services.StateReady)
		fmt.Println(readyBanner())
	case <-ctx.Done():
	}
}

// runWithReload runs the service, tearing it down and rebuilding from the config
// whenever the watched path changes, until ctx is cancelled. A config that fails
// to load leaves the previous generation stopped and waits for the next change.
func runWithReload(
	ctx context.Context, configPath string, svc core.RuntimeServices,
	hosted []services.HostedService, health *services.Health,
) error {
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
			// A config that will not load leaves nothing serving, so readiness must
			// say so for however long it takes someone to fix the file.
			health.SetState(services.StateReloading)
			if !waitForChange(ctx, changed) {
				return nil
			}
			continue
		}
		announceConfig(hosted, config)

		reload, runErr := runGeneration(ctx, config, changed, svc, health)
		if runErr != nil {
			// In watch mode a build/start failure is not fatal: keep the watcher
			// alive and wait for the next change, same as a config load error, so
			// fixing the file recovers without restarting the process.
			slog.Error("runtime failed, waiting for next change", "error", runErr)
			health.SetState(services.StateReloading)
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
	health *services.Health,
) (bool, error) {
	slog.Info("starting runtime", "version", Version, "connectors", len(config.Connectors), "flows", len(config.Flows))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithRuntimeServices(svc))
	announced := make(chan struct{})
	go func() {
		defer close(announced)
		announceWhenReady(runCtx, service, health)
	}()
	go func() { done <- service.Run(runCtx) }()

	// settle records this generation's final readiness state. It cancels first so
	// the announcer always wakes — a generation that failed before reporting ready
	// leaves Started() closed forever — and waits for it to exit, so a late
	// announcement can never land after the state written here.
	settle := func(state services.State) {
		cancel()
		<-announced
		health.SetState(state)
	}

	select {
	case <-ctx.Done():
		<-done
		settle(services.StateStopped)
		return false, nil
	case <-changed:
		slog.Info("config changed, reloading")
		// Readiness goes false for the whole rebuild: the old generation's sources
		// are about to stop and the new one's are not up yet. settle writes it again
		// once the generation has fully stopped; this write is the prompt one.
		health.SetState(services.StateReloading)
		cancel()
		<-done
		settle(services.StateReloading)
		return true, nil
	case runErr := <-done:
		settle(services.StateReloading)
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
