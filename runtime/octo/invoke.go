package main

// This file holds `octo invoke`: calling one flow by name, without starting any
// source, and printing what it produced. The debug features it can run under —
// --break-at, --spies, --mocks — live in debug.go.

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

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/runtime"
	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

// defaultInvokeTimeout bounds how long `invoke` waits for the flow by default.
const defaultInvokeTimeout = 30 * time.Second

// invokeCommand calls a flow by name with data supplied on the command line (or
// stdin), printing the result message as JSON. Sources are not started. With --break-at,
// --spies or both it instead prints a debugOutcome envelope: the message at the
// addressed block, and what each spy saw.
func invokeCommand(args []string) error {
	flags, err := parseInvokeFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Println(usage)
			return nil
		}
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

	req, err := buildInvokeRequest(flags, svc)
	if err != nil {
		return err
	}

	result, err := invokeFlow(ctx, req)

	// A run that was asked to observe something reports the debug envelope. A run that
	// was only asked to mock is still a plain invoke — mocking changes what the flow
	// does, but it produces no observations of its own, so it prints its result message
	// like any other run. --envelope asks for it outright, and --envelope-out asks for
	// it somewhere other than stdout.
	if req.envelope || req.envelopeOut != "" || req.breakpoint != nil || req.spies != nil {
		return reportDebugOutcome(req, result, err)
	}
	if err != nil {
		return err
	}
	return printFlowResult(flags.flowName, result)
}

// buildInvokeRequest turns the parsed flags into the request invokeFlow runs: the
// config to run, what to call the flow with, and the debug features to run it under.
// Everything is resolved here, before the service is built, so a bad address or a
// malformed mock spec fails as the bad request it is rather than mid-run.
//
// A --run-debug-config supplies any of those, and a flag overrides the section it
// corresponds to — whole-section, not key by key. Merging a --spies list into the
// file's would leave no way to run the file with fewer spies than it lists; replacing
// it outright means a flag always says exactly what the run will do.
func buildInvokeRequest(flags invokeFlags, svc core.RuntimeServices) (invokeRequest, error) {
	debug, err := loadDebugConfig(flags.runDebugConfig)
	if err != nil {
		return invokeRequest{}, err
	}

	body, err := resolveBody(flags.data, debug)
	if err != nil {
		return invokeRequest{}, err
	}
	vars, err := resolveVars(flags.vars, debug)
	if err != nil {
		return invokeRequest{}, err
	}
	config, err := runtime.LoadConfig(flags.configPath, svc.Resources())
	if err != nil {
		return invokeRequest{}, err
	}

	req := invokeRequest{
		config:      config,
		flow:        flags.flowName,
		body:        body,
		vars:        vars,
		timeout:     flags.timeout,
		services:    svc,
		envelope:    flags.envelope,
		envelopeOut: flags.envelopeOut,
	}
	if breakAt := override(flags.breakAt, debug.BreakAt); breakAt != "" {
		req.breakpoint = core.NewBreakpoint(breakAt)
	}
	if req.spies, err = resolveSpies(flags.spies, debug); err != nil {
		return invokeRequest{}, err
	}
	if req.mocks, err = resolveMocks(flags.mocks, debug); err != nil {
		return invokeRequest{}, err
	}
	return req, nil
}

// override returns the flag when it was given, else the debug config's value.
func override(fromFlag, fromFile string) string {
	if fromFlag != "" {
		return fromFlag
	}
	return fromFile
}

// resolveBody picks the request body: --data, else the debug config's input.data, else
// piped stdin.
//
// The debug config comes BEFORE stdin, which is worth spelling out because the flag on
// its own reads the other way round. `invoke` with no --data waits on stdin, and a
// debug config exists precisely so the run is fully described in the file — so
// consulting stdin first would make `invoke --run-debug-config x.yaml` block forever in
// any script that inherits an open stdin, waiting for a body it already has. A file
// that carries no input.data still falls through to stdin, so piping into a config that
// only lists spies and mocks works as it reads.
func resolveBody(data string, debug debugConfig) ([]byte, error) {
	if data != "" {
		return []byte(data), nil
	}
	body, err := debug.body()
	if err != nil {
		return nil, err
	}
	if len(body) > 0 {
		return body, nil
	}
	return resolveData("")
}

// resolveVars picks the message variables: --vars, else the debug config's input.vars.
func resolveVars(vars string, debug debugConfig) (types.Variables, error) {
	parsed, err := parseVariables(vars)
	if err != nil {
		return nil, err
	}
	if parsed != nil {
		return parsed, nil
	}
	return debug.Input.Vars, nil
}

// resolveSpies picks the spy collector: --spies, else the debug config's list.
func resolveSpies(spies string, debug debugConfig) (*core.Spies, error) {
	if strings.TrimSpace(spies) != "" {
		return parseSpies(spies)
	}
	return debug.spies()
}

// resolveMocks picks the mock set: --mocks, else the debug config's specs.
func resolveMocks(mocks string, debug debugConfig) (*core.Mocks, error) {
	if strings.TrimSpace(mocks) != "" {
		return parseMocks(mocks)
	}
	return debug.mocks()
}

// invokeFlags holds the parsed flags of `octo invoke`.
type invokeFlags struct {
	configPath string
	flowName   string
	data       string
	vars       string
	breakAt    string
	spies      string
	mocks      string
	// runDebugConfig is a file supplying any of the above; a flag overrides the
	// section it corresponds to.
	runDebugConfig string
	timeout        time.Duration
	// envelope asks for the debug envelope even when nothing was observed.
	envelope bool
	// envelopeOut writes the envelope to a file instead of stdout.
	envelopeOut string
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
	fs.StringVar(&flags.spies, "spies", "",
		"comma-separated block addresses to record every message that crosses them")
	fs.StringVar(&flags.mocks, "mocks", "",
		`JSON object of block addresses to stand in for: {"<address>":{"cases":[…],"default":{…}}}`)
	fs.StringVar(&flags.runDebugConfig, "run-debug-config", "",
		"YAML/JSON file holding the whole run: input, breakAt, spies, mocks (a flag overrides its section)")
	fs.DurationVar(&flags.timeout, "timeout", defaultInvokeTimeout, "max time to wait for the flow")
	fs.BoolVar(&flags.envelope, "envelope", false,
		"always print the outcome envelope, and report a flow failure in it rather than exiting non-zero")
	fs.StringVar(&flags.envelopeOut, "envelope-out", "",
		"write the outcome envelope to this file instead of stdout (implies --envelope)")

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

// printFlowResult prints the flow's result message, the output of a plain invoke:
// the whole envelope — event_id, variables, body — and not the body alone.
//
// A flow spends much of its work building variables up with set-variable, enrich and
// the like, and they are as much its result as the body is. Printing only the body
// made a run that stopped early at a --break-at report *more* than the same run
// allowed to finish, which is the wrong way round; both paths now say the same thing
// by "the result": the message.
func printFlowResult(flowName string, result *types.Message) error {
	if result == nil {
		slog.Info("flow dropped the message", "flow", flowName)
		return nil
	}
	out, err := json.Marshal(result.Reported())
	if err != nil {
		return fmt.Errorf("encode flow result: %w", err)
	}
	fmt.Println(string(out))
	return nil
}

// invokeRequest is everything one `octo invoke` needs: the config to run, the flow
// to call and what to call it with, and the debug features to run it under.
type invokeRequest struct {
	config     types.Config
	flow       string
	body       []byte
	vars       types.Variables // nil unless --vars was given
	timeout    time.Duration
	services   core.RuntimeServices
	breakpoint *core.Breakpoint // nil unless --break-at was given
	spies      *core.Spies      // nil unless --spies was given
	mocks      *core.Mocks      // nil unless --mocks was given

	// envelope reports the outcome in the debug envelope even when the run observed
	// nothing. Without it, a plain invoke says what happened three different ways: a
	// completed flow prints its message, a dropped one prints *nothing* (a log line on
	// stderr), and a failed one exits non-zero with the error in another log line. A
	// caller reading stdout cannot tell a drop from a result, and would have to parse
	// failures out of logs — so a program driving invoke (dolphin, the test runner)
	// asks for the envelope, which already models all three outcomes, and reads one
	// shape. A flow failure becomes envelope.error and exits 0: the flow failing is
	// the answer to the question that was asked, not a failure of the CLI.
	envelope bool

	// envelopeOut writes the envelope to this file rather than stdout.
	//
	// stdout is not the CLI's alone: a `log` block over a logger connector writes there
	// too, so a flow that logs anything interleaves its own JSON with the envelope and a
	// caller parsing stdout gets two documents where it expected one. That is not a
	// misconfiguration to warn about — logging is what the block is for. So a program
	// driving invoke asks for the envelope on a channel the flow cannot reach, and reads
	// a file it named itself.
	envelopeOut string
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
	if req.spies != nil {
		opts = append(opts, runtime.WithSpies(req.spies))
	}
	if req.mocks != nil {
		opts = append(opts, runtime.WithMocks(req.mocks))
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
