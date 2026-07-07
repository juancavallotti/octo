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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "github.com/juancavallotti/octo/connectors/cron"
	_ "github.com/juancavallotti/octo/connectors/database" // registers the "database" connector and "sql" block
	_ "github.com/juancavallotti/octo/connectors/events"   // registers the "events" connector + source and the "publish-event" block
	_ "github.com/juancavallotti/octo/connectors/http"
	_ "github.com/juancavallotti/octo/connectors/httpclient"    // registers the "http-client" connector and "rest" block
	_ "github.com/juancavallotti/octo/connectors/llm/aiblocks"  // registers the "ai-mapping" block
	_ "github.com/juancavallotti/octo/connectors/llm/anthropic" // registers the "llm-anthropic" connector
	_ "github.com/juancavallotti/octo/connectors/llm/gemini"    // registers the "llm-gemini" connector
	_ "github.com/juancavallotti/octo/connectors/llm/openai"    // registers the "llm-openai" connector
	_ "github.com/juancavallotti/octo/connectors/logger"        // registers the "logger" connector and "log" block
	_ "github.com/juancavallotti/octo/connectors/notion"        // registers the "notion" connector and its blocks
	_ "github.com/juancavallotti/octo/connectors/queue"         // registers the "queue" connector + source and the "queue-dispatch" block
	_ "github.com/juancavallotti/octo/connectors/slack"         // registers the "slack" connector and its blocks
	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/core/runtime"
	"github.com/juancavallotti/octo/services"
	"github.com/juancavallotti/octo/types"
	// The active runtime-services provider is selected at build time by tag:
	// standalone by default, k8s with -tags k8s. See providers_*.go.
)

// defaultInvokeTimeout bounds how long `invoke` waits for the flow by default.
const defaultInvokeTimeout = 30 * time.Second

func main() {
	level, levelErr := core.ParseLevel(os.Getenv("LOG_LEVEL"))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	if levelErr != nil {
		slog.Warn("invalid LOG_LEVEL, defaulting to info", "error", levelErr)
	}

	if err := run(os.Args[1:]); err != nil {
		slog.Error("cli stopped with error", "error", err)
		os.Exit(1)
	}
}

// teeDefaultLoggerToSink re-installs the process default logger so it also writes
// to svc's central log sink, when the active runtime-services module ships logs
// (the k8s module). The standalone module ships nothing, so the default logger is
// left as the stderr handler installed in main. Called once after the services are
// built; the stderr base keeps applying LOG_LEVEL while the sink captures records
// for the central aggregator.
func teeDefaultLoggerToSink(svc core.RuntimeServices) {
	shipper, ok := svc.(core.LogShipper)
	if !ok {
		return
	}
	sink := shipper.LogSink()
	if sink == nil {
		return
	}
	level, _ := core.ParseLevel(os.Getenv("LOG_LEVEL"))
	base := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(core.TeeHandler(base, sink)))
}

// run dispatches to a subcommand. The default (no subcommand, or a leading flag)
// is "run", so `cli -config x.yaml` keeps working.
// usage is the top-level help page, printed for `octo`, `octo --help`, and a
// subcommand's --help.
const usage = `octo — run and invoke octo integration flows

Usage:
  octo [run] --config <path> [--watch]                       Start connectors and flows (default)
  octo invoke --config <path> --flow <name> [--data <json>]  Call one flow and print its result
  octo eval --expr <cel> [--data <json>]                     Evaluate a CEL expression and print the result
  octo version                                               Print the version and build date
  octo --help                                                Show this help

Run flags:
  --config <path>   path to the runtime config (file or directory)
  --watch           reload the config when it changes

Invoke flags:
  --config <path>    path to the runtime config (file or directory)
  --flow <name>      name of the flow to invoke
  --data <json>      JSON request body (reads stdin when omitted)
  --timeout <dur>    max time to wait for the flow (default 30s)

Eval flags:
  --expr <cel>       CEL expression to evaluate (required)
  --data <json>      JSON object bound to body (reads stdin when omitted)
  --vars <json>      JSON object bound to vars
  --env <json>       JSON object bound to env

Flags accept one or two dashes (--config or -config).`

func run(args []string) error {
	// Handle top-level help and version before subcommand dispatch: the run/invoke
	// flagsets do not define them, so `octo --help` / `octo --version` must be
	// intercepted here. Go's flag package treats -x and --x alike, so honor both
	// dash forms.
	if len(args) == 0 {
		fmt.Println(usage)
		return nil
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		fmt.Println(usage)
		return nil
	case "-version", "--version", "version":
		fmt.Println(versionLine())
		return nil
	}

	cmd := "run"
	if !strings.HasPrefix(args[0], "-") {
		cmd = args[0]
		args = args[1:]
	}

	switch cmd {
	case "run":
		return runCommand(args)
	case "invoke":
		return invokeCommand(args)
	case "eval":
		return evalCommand(args)
	default:
		return fmt.Errorf("unknown command %q (expected \"run\", \"invoke\", \"eval\", or \"version\")", cmd)
	}
}

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

// configDir returns the directory a config path is rooted at — the directory
// itself when path is a config directory, else the config file's parent. It is
// the resource root handed to the runtime-services provider.
func configDir(configPath string) string {
	if info, err := os.Stat(configPath); err == nil && !info.IsDir() {
		return filepath.Dir(configPath)
	}
	return configPath
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

// invokeCommand calls a flow by name with data supplied on the command line (or
// stdin), printing the result body as JSON. Sources are not started.
func invokeCommand(args []string) error {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	configPath := fs.String("config", "", "path to the runtime config (file or directory)")
	flowName := fs.String("flow", "", "name of the flow to invoke")
	data := fs.String("data", "", "JSON request body (reads stdin when omitted)")
	timeout := fs.Duration("timeout", defaultInvokeTimeout, "max time to wait for the flow")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build services first: their resource loader (rooted at the config directory)
	// is needed to load the config's env resources.
	svc, err := services.New(ctx, services.Options{ResourceRoot: configDir(*configPath)})
	if err != nil {
		return fmt.Errorf("init runtime services: %w", err)
	}
	defer func() { _ = svc.Close() }()
	teeDefaultLoggerToSink(svc)

	config, err := runtime.LoadConfig(*configPath, svc.Resources())
	if err != nil {
		return err
	}

	result, err := invokeFlow(ctx, config, *flowName, body, *timeout, svc)
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

// evalOutcome is the JSON envelope `octo eval` prints on stdout. OK distinguishes a
// successful evaluation (Result holds the JSON-native value, which may itself be
// false/0/null) from a compile or eval failure (Error holds the message). Both cases
// exit 0 — a bad expression is a normal result, not a CLI failure — so a consumer can
// parse the envelope rather than inspecting exit codes. Result is always emitted (never
// omitempty) so a legitimate falsey result is not confused with its absence.
type evalOutcome struct {
	OK     bool   `json:"ok"`
	Result any    `json:"result"`
	Error  string `json:"error,omitempty"`
}

// evalCommand evaluates a CEL expression against an ad-hoc message, without a config
// or any runtime services — CEL compilation and evaluation are pure. The --data object
// is bound to `body` (like invoke's request body), --vars to `vars`, and --env to
// `env`; the remaining message variables (eventID, correlationID, now) get their
// defaults. It prints an evalOutcome envelope so the result and the error path are
// unambiguous.
func evalCommand(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own
	expression := fs.String("expr", "", "CEL expression to evaluate")
	data := fs.String("data", "", "JSON object bound to body (reads stdin when omitted)")
	varsJSON := fs.String("vars", "", "JSON object bound to vars")
	envJSON := fs.String("env", "", "JSON object bound to env")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return fmt.Errorf("parse eval flags: %w", err)
	}
	if *expression == "" {
		return errors.New("expression is required (-expr)")
	}

	body, err := resolveData(*data)
	if err != nil {
		return err
	}
	msg, err := buildMessage(body)
	if err != nil {
		return err
	}
	if *varsJSON != "" {
		vars := types.Variables{}
		if err := json.Unmarshal([]byte(*varsJSON), &vars); err != nil {
			return fmt.Errorf("parse -vars JSON: %w", err)
		}
		msg.Variables = vars
	}
	// A non-nil env map keeps env.NAME a missing-key error rather than a null-deref,
	// matching how a real run materializes its resolved env (see expr.EnvActivation).
	env := map[string]any{}
	if *envJSON != "" {
		if err := json.Unmarshal([]byte(*envJSON), &env); err != nil {
			return fmt.Errorf("parse -env JSON: %w", err)
		}
	}

	out, err := json.Marshal(evalExpression(*expression, msg, env))
	if err != nil {
		return fmt.Errorf("marshal eval outcome: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// evalExpression compiles expression through the single message-CEL seam and evaluates
// it against msg and env, returning the envelope octo eval prints. A compile or eval
// failure is reported as an OK=false outcome (not a Go error) so the caller prints it
// and exits 0. A nil resource loader is passed: CompileMessage substitutes a no-op
// loader, so standalone evaluation has no integration resources (templateResource is
// unavailable) — expressions over body/vars/env/eventID/correlationID/now still work.
func evalExpression(expression string, msg *types.Message, env map[string]any) evalOutcome {
	program, err := expr.CompileMessage(nil, expression)
	if err != nil {
		return evalOutcome{OK: false, Error: err.Error()}
	}
	result, err := program.Eval(expr.MessageActivation(msg, env))
	if err != nil {
		return evalOutcome{OK: false, Error: err.Error()}
	}
	return evalOutcome{OK: true, Result: result}
}

// invokeFlow runs the service in invoke mode, waits until it is ready, calls the
// named flow with body, then tears the service down. It returns the flow's result
// (nil when the flow dropped the message).
func invokeFlow(
	ctx context.Context, config types.Config, flowName string, body []byte, timeout time.Duration,
	svc core.RuntimeServices,
) (*types.Message, error) {
	service := runtime.NewService(config, core.DefaultRegistry(),
		runtime.WithInvokeMode(), runtime.WithRuntimeServices(svc))
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
