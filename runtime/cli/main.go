package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/juancavallotti/eip-go/connectors/cron"
	_ "github.com/juancavallotti/eip-go/connectors/http"
	_ "github.com/juancavallotti/eip-go/connectors/logger"
	_ "github.com/juancavallotti/eip-go/connectors/processors/log"
	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/runtime"
	"github.com/juancavallotti/eip-go/types"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(os.Args[1:]); err != nil {
		slog.Error("cli stopped with error", "error", err)
		os.Exit(1)
	}
}

// run dispatches to a subcommand. The default (no subcommand, or a leading flag)
// is "run", so `cli -config x.yaml` keeps working.
func run(args []string) error {
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runCommand(args)
	case "invoke":
		return invokeCommand(args)
	default:
		return fmt.Errorf("unknown command %q (expected \"run\" or \"invoke\")", cmd)
	}
}

// runCommand starts the configured connectors and flows until interrupted. With
// --watch it reloads on config changes.
func runCommand(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	watch := fs.Bool("watch", false, "reload the config when it changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("config path is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *watch {
		return runWithReload(ctx, *configPath)
	}
	return runOnce(ctx, *configPath)
}

// runOnce loads the config and runs a single service generation.
func runOnce(ctx context.Context, configPath string) error {
	config, err := runtime.LoadConfig(configPath)
	if err != nil {
		return err
	}
	slog.Info("starting runtime", "connectors", len(config.Connectors), "flows", len(config.Flows))

	service := runtime.NewService(config, core.DefaultRegistry())
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	slog.Info("runtime stopped")
	return nil
}

// runWithReload runs the service, tearing it down and rebuilding from the config
// whenever the watched path changes, until ctx is cancelled. A config that fails
// to load leaves the previous generation stopped and waits for the next change.
func runWithReload(ctx context.Context, configPath string) error {
	changed, err := watchConfig(ctx, configPath)
	if err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	slog.Info("watching config for changes", "path", configPath)

	for {
		config, loadErr := runtime.LoadConfig(configPath)
		if loadErr != nil {
			slog.Error("config load failed, waiting for next change", "error", loadErr)
			select {
			case <-ctx.Done():
				return nil
			case <-changed:
				continue
			}
		}

		slog.Info("starting runtime", "connectors", len(config.Connectors), "flows", len(config.Flows))
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		service := runtime.NewService(config, core.DefaultRegistry())
		go func() { done <- service.Run(runCtx) }()

		select {
		case <-ctx.Done():
			cancel()
			<-done
			slog.Info("runtime stopped")
			return nil
		case <-changed:
			slog.Info("config changed, reloading")
			cancel()
			<-done
		case runErr := <-done:
			cancel()
			if runErr != nil && !errors.Is(runErr, context.Canceled) {
				return runErr
			}
			// The service exited on its own without an error; wait for a change
			// before rebuilding so we do not spin.
			select {
			case <-ctx.Done():
				return nil
			case <-changed:
			}
		}
	}
}

// invokeCommand calls a flow by name with data supplied on the command line (or
// stdin), printing the result body as JSON. Sources are not started.
func invokeCommand(args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	flowName := fs.String("flow", "", "name of the flow to invoke")
	data := fs.String("data", "", "JSON request body (reads stdin when omitted)")
	timeout := fs.Duration("timeout", 30*time.Second, "max time to wait for the flow")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("config path is required")
	}
	if *flowName == "" {
		return errors.New("flow name is required (-flow)")
	}

	body, err := resolveData(*data)
	if err != nil {
		return err
	}

	config, err := runtime.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithInvokeMode())
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- service.Run(runCtx) }()

	// Wait until the flows are started (and thus callable) before invoking.
	select {
	case <-service.Started():
	case runErr := <-done:
		cancel()
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return runErr
		}
		return errors.New("service stopped before the flow could be invoked")
	case <-ctx.Done():
		cancel()
		<-done
		return nil
	}

	msg, err := types.NewMessage("")
	if err != nil {
		cancel()
		<-done
		return err
	}
	if len(body) > 0 {
		if err := msg.SetBodyJSON(body); err != nil {
			cancel()
			<-done
			return err
		}
	}

	callCtx, callCancel := context.WithTimeout(ctx, *timeout)
	result, callErr := service.Flows().Call(callCtx, *flowName, msg)
	callCancel()

	// Tear the service down before reporting.
	cancel()
	<-done

	if callErr != nil {
		return callErr
	}
	if result == nil {
		slog.Info("flow dropped the message", "flow", *flowName)
		return nil
	}

	out, err := result.BodyJSON()
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

// resolveData returns the request body bytes: the literal -data value, or stdin
// when -data is empty and stdin is piped. An empty result means no body.
func resolveData(data string) ([]byte, error) {
	if data != "" {
		return []byte(data), nil
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, nil
	}
	// Only read stdin when it is piped/redirected, not an interactive terminal.
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, nil
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, errors.New("stdin is not valid JSON")
	}
	return raw, nil
}
