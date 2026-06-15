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
	_ "github.com/juancavallotti/eip-go/connectors/database" // registers the "database" connector and "sql" block
	_ "github.com/juancavallotti/eip-go/connectors/http"
	_ "github.com/juancavallotti/eip-go/connectors/httpclient" // registers the "http-client" connector and "rest" block
	_ "github.com/juancavallotti/eip-go/connectors/logger"     // registers the "logger" connector and "log" block
	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/runtime"
	"github.com/juancavallotti/eip-go/types"
)

// defaultInvokeTimeout bounds how long `invoke` waits for the flow by default.
const defaultInvokeTimeout = 30 * time.Second

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
		return fmt.Errorf("parse run flags: %w", err)
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
			if !waitForChange(ctx, changed) {
				return nil
			}
			continue
		}

		reload, runErr := runGeneration(ctx, config, changed)
		if runErr != nil {
			return runErr
		}
		if !reload {
			slog.Info("runtime stopped")
			return nil
		}
	}
}

// runGeneration runs one service generation and returns whether the caller should
// reload (rebuild from config) or stop.
func runGeneration(ctx context.Context, config types.Config, changed <-chan struct{}) (bool, error) {
	slog.Info("starting runtime", "connectors", len(config.Connectors), "flows", len(config.Flows))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	service := runtime.NewService(config, core.DefaultRegistry())
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

// invokeCommand calls a flow by name with data supplied on the command line (or
// stdin), printing the result body as JSON. Sources are not started.
func invokeCommand(args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	flowName := fs.String("flow", "", "name of the flow to invoke")
	data := fs.String("data", "", "JSON request body (reads stdin when omitted)")
	timeout := fs.Duration("timeout", defaultInvokeTimeout, "max time to wait for the flow")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse invoke flags: %w", err)
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

	result, err := invokeFlow(ctx, config, *flowName, body, *timeout)
	if err != nil {
		return err
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

// invokeFlow runs the service in invoke mode, waits until it is ready, calls the
// named flow with body, then tears the service down. It returns the flow's result
// (nil when the flow dropped the message).
func invokeFlow(
	ctx context.Context, config types.Config, flowName string, body []byte, timeout time.Duration,
) (*types.Message, error) {
	service := runtime.NewService(config, core.DefaultRegistry(), runtime.WithInvokeMode())
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- service.Run(runCtx) }()

	ready, err := awaitReady(ctx, service, done)
	if err != nil {
		cancel()
		return nil, err
	}
	if !ready {
		cancel()
		return nil, nil //nolint:nilnil // ctx cancelled before invocation: no result, no error
	}
	defer func() {
		cancel()
		<-done
	}()

	msg, err := buildMessage(body)
	if err != nil {
		return nil, err
	}

	callCtx, callCancel := context.WithTimeout(ctx, timeout)
	defer callCancel()
	result, err := service.Flows().Call(callCtx, flowName, msg)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// awaitReady waits until the service's flows are started. It returns ready=true
// when callable; otherwise it drains the run goroutine and returns ready=false
// with any fatal run error (nil when ctx was cancelled first).
func awaitReady(ctx context.Context, service *runtime.Service, done <-chan error) (bool, error) {
	select {
	case <-service.Started():
		return true, nil
	case runErr := <-done:
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			return false, runErr
		}
		return false, errors.New("service stopped before the flow could be invoked")
	case <-ctx.Done():
		<-done
		return false, nil
	}
}

// buildMessage creates a message, decoding body into it when non-empty.
func buildMessage(body []byte) (*types.Message, error) {
	msg, err := types.NewMessage("")
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		if err := msg.SetBodyJSON(body); err != nil {
			return nil, err
		}
	}
	return msg, nil
}

// resolveData returns the request body bytes: the literal -data value, or stdin
// when -data is empty and stdin is piped. An empty result means no body.
func resolveData(data string) ([]byte, error) {
	if data != "" {
		return []byte(data), nil
	}
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, nil //nolint:nilerr // cannot stat stdin: treat as no body, not an error
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
