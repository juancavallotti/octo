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

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/runtime"
	"github.com/juancavallotti/octo/services"
	"github.com/juancavallotti/octo/types"
)

// This file holds `octo invoke`: calling one flow by name without starting any source,
// and printing what it produced. The debug features it can run under live in debug.go.

// defaultInvokeTimeout bounds how long `invoke` waits for the flow by default.
const defaultInvokeTimeout = 30 * time.Second

// invokeCommand calls a flow by name with data supplied on the command line (or
// stdin), printing the result body as JSON. Sources are not started. With
// --break-at it instead runs the flow until it reaches the addressed block, and
// prints a breakOutcome envelope holding the message at that point.
func invokeCommand(args []string) error {
	flags, err := parseInvokeFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
		return err
	}

	body, err := resolveData(flags.data)
	if err != nil {
		return err
	}
	vars, err := parseVariables(flags.vars)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Build services first: their resource loader (rooted at the config directory)
	// is needed to load the config's env resources.
	svc, err := services.New(ctx, services.Options{ResourceRoot: configDir(flags.configPath)})
	if err != nil {
		return fmt.Errorf("init runtime services: %w", err)
	}
	defer func() { _ = svc.Close() }()
	teeDefaultLoggerToSink(svc)

	config, err := runtime.LoadConfig(flags.configPath, svc.Resources())
	if err != nil {
		return err
	}

	req := invokeRequest{
		config:   config,
		flow:     flags.flowName,
		body:     body,
		vars:     vars,
		timeout:  flags.timeout,
		services: svc,
	}
	if flags.breakAt != "" {
		req.breakpoint = core.NewBreakpoint(flags.breakAt)
	}

	result, err := invokeFlow(ctx, req)
	if req.breakpoint != nil {
		return printBreakOutcome(req.breakpoint, err)
	}
	if err != nil {
		return err
	}
	return printFlowResult(flags.flowName, result)
}

// invokeFlags holds the parsed flags of `octo invoke`.
type invokeFlags struct {
	configPath string
	flowName   string
	data       string
	vars       string
	breakAt    string
	timeout    time.Duration
}

// parseInvokeFlags parses and validates the invoke flags. It returns an error
// wrapping flag.ErrHelp when the user asked for help, so the caller prints usage.
func parseInvokeFlags(args []string) (invokeFlags, error) {
	fs := flag.NewFlagSet("invoke", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress the default usage dump; we print our own

	var flags invokeFlags
	fs.StringVar(&flags.configPath, "config", "", "path to the runtime config (file or directory)")
	fs.StringVar(&flags.flowName, "flow", "", "name of the flow to invoke")
	fs.StringVar(&flags.data, "data", "", "JSON request body (reads stdin when omitted)")
	fs.StringVar(&flags.vars, "vars", "", "JSON object seeding the message variables")
	fs.StringVar(&flags.breakAt, "break-at", "",
		"run until this block, then print the message and stop (<flow>.<block>[<branch>].<block>)")
	fs.DurationVar(&flags.timeout, "timeout", defaultInvokeTimeout, "max time to wait for the flow")

	if err := fs.Parse(args); err != nil {
		return invokeFlags{}, fmt.Errorf("parse invoke flags: %w", err)
	}
	if flags.configPath == "" {
		return invokeFlags{}, errors.New("config path is required")
	}
	if flags.flowName == "" {
		return invokeFlags{}, errors.New("flow name is required (-flow)")
	}
	return flags, nil
}

// printFlowResult prints the flow's result body, the output of a plain invoke.
func printFlowResult(flowName string, result *types.Message) error {
	if result == nil {
		slog.Info("flow dropped the message", "flow", flowName)
		return nil
	}
	out, err := result.BodyJSON()
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

// invokeRequest is everything one `octo invoke` needs: the config to run, the flow
// to call and what to call it with, and the optional breakpoint to break on.
type invokeRequest struct {
	config     types.Config
	flow       string
	body       []byte
	vars       types.Variables // nil unless --vars was given
	timeout    time.Duration
	services   core.RuntimeServices
	breakpoint *core.Breakpoint // nil unless --break-at was given
}

// flowCallError marks an error raised by running the flow, as opposed to one from
// building or starting the service. The two mean different things under --break-at:
// a flow that fails is a debugging result to report in the envelope, while a service
// that will not start (an unresolvable breakpoint address, a bad config) is a bad
// request and must exit non-zero.
type flowCallError struct{ err error }

func (e *flowCallError) Error() string { return e.err.Error() }

func (e *flowCallError) Unwrap() error { return e.err }

// invokeFlow runs the service in invoke mode, waits until it is ready, calls the
// named flow, then tears the service down. It returns the flow's result (nil when
// the flow dropped the message, or when it stopped at a breakpoint before producing
// one).
func invokeFlow(ctx context.Context, req invokeRequest) (*types.Message, error) {
	opts := []runtime.ServiceOption{
		runtime.WithInvokeMode(),
		runtime.WithRuntimeServices(req.services),
	}
	if req.breakpoint != nil {
		opts = append(opts, runtime.WithBreakpoint(req.breakpoint))
	}

	service := runtime.NewService(req.config, core.DefaultRegistry(), opts...)
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

	msg, err := buildMessage(req.body, req.vars)
	if err != nil {
		return nil, err
	}

	callCtx, callCancel := context.WithTimeout(ctx, req.timeout)
	defer callCancel()
	result, err := service.Flows().Call(callCtx, req.flow, msg)
	if err != nil {
		return nil, &flowCallError{err: err}
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

// buildMessage creates a message, decoding body into it when non-empty and seeding
// vars when non-nil. Variables are how a real source hands a flow everything that is
// not the body — the HTTP source copies request headers into them — so both `invoke`
// and `eval` can seed them to reproduce what a block actually reads.
func buildMessage(body []byte, vars types.Variables) (*types.Message, error) {
	msg, err := types.NewMessage("")
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		if err := msg.SetBodyJSON(body); err != nil {
			return nil, err
		}
	}
	if vars != nil {
		msg.Variables = vars
	}
	return msg, nil
}

// parseVariables decodes a -vars JSON object into message variables. An empty string
// means "none given" and yields a nil map, which buildMessage leaves untouched.
func parseVariables(varsJSON string) (types.Variables, error) {
	if varsJSON == "" {
		return nil, nil
	}
	vars := types.Variables{}
	if err := json.Unmarshal([]byte(varsJSON), &vars); err != nil {
		return nil, fmt.Errorf("parse -vars JSON: %w", err)
	}
	return vars, nil
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
