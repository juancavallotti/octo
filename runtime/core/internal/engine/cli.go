// The cli-run block: run a local program, watch its output as it is produced,
// and carry on with how it ended.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// The event kinds a cli-run's events path may name. The two output streams,
// plus the terminal event that always fires.
const cliEventExit = "exit"

// cliEventKinds is the closed set a cli-run emit list may name.
var cliEventKinds = map[string]bool{
	commandStdout: true, commandStderr: true, cliEventExit: true,
}

// What a cli-run does with a non-zero exit status.
const (
	// onExitFail errors the block, so the flow's error chain sees it. The default:
	// a command that failed is usually a message that failed.
	onExitFail = "fail"
	// onExitContinue carries on, leaving the decision to a later block reading
	// body.exitCode — for the command whose failure is information rather than a
	// fault.
	onExitContinue = "continue"
)

// cliRun runs one program per message.
//
// It is synchronous, and that is the design rather than an accident. The
// alternative — taking the flow over and resuming it once per line — would end
// the caller's own chain at this block, so an HTTP request would be answered and
// its connection torn down while the command was still running, and every line
// after that would be written to a stream nobody was reading. A caller waiting on
// a command has to keep waiting, so the block blocks and the per-line work
// happens inside it.
type cliRun struct {
	command *command
	program *expr.Program
	// allowed is every program this block may run, as resolved absolute paths, or
	// nil when it was given no list and may run anything. It is the whole security
	// boundary, and it is checked on every message rather than once at build,
	// because the program is an expression: what runs can come from the message,
	// which is what lets an ai-agent tool branch hand a model a set of commands.
	allowed map[string]bool
	args    *expr.Program
	stdin   *expr.Program
	env     map[string]any
	events  *emitter
	onExit  string
	name    string
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) cliRun(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if err := allowSlots(cfg, blockKindCLIRun,
		"program", "args", "stdin", "allow", "allowInterpreters", "env", "workDir",
		"timeout", "maxOutputBytes", "onExit", "events", "emit"); err != nil {
		return nil, err
	}
	cmd, err := buildCommand(cfg)
	if err != nil {
		return nil, err
	}
	onExit, err := cliOnExit(cfg.OnExit)
	if err != nil {
		return nil, err
	}

	block := &cliRun{command: cmd, env: expr.EnvActivation(b.deps.Env), onExit: onExit, name: cfg.Name}
	if block.program, err = compileCLI(b, cfg.Program, "program"); err != nil {
		return nil, err
	}
	if block.allowed, err = buildAllowList(cfg, block.program, block.env); err != nil {
		return nil, err
	}
	if block.args, err = compileCLI(b, cfg.Args, "args"); err != nil {
		return nil, err
	}
	if block.stdin, err = compileCLI(b, cfg.Stdin, "stdin"); err != nil {
		return nil, err
	}
	if err := b.configureCLIEvents(block, cfg); err != nil {
		return nil, err
	}
	return block, nil
}

// buildCommand resolves everything decided once: where a command runs, what
// environment it sees, and its bounds.
func buildCommand(cfg types.BlockConfig) (*command, error) {
	if cfg.Program == "" {
		return nil, errors.New("cli-run block requires a program")
	}
	timeout, err := cliTimeout(cfg.Timeout)
	if err != nil {
		return nil, err
	}
	maxOutput := cfg.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutputBytes
	}
	return &command{
		workDir:        cfg.WorkDir,
		env:            commandEnv(cfg.Env),
		timeout:        timeout,
		maxOutputBytes: maxOutput,
	}, nil
}

// buildAllowList resolves the programs this block may run, or reports nil for a
// block that may run anything.
//
// An empty list means no restriction, and that is the deliberate default:
// requiring one to run a command you spelled out two lines above is ceremony,
// and it makes the block clumsy for exactly the local, iterative work it is most
// useful for.
//
// What it costs is worth naming. A block with no list whose program comes from
// the message will run whatever it is handed — which is fine when the caller is
// the flow author at a terminal, and is remote code execution when the caller is
// the internet. That combination gets a warning at build time, so the answer
// arrives at startup rather than in a postmortem; a constant program stays quiet,
// because it can only ever run the one thing.
//
// Every entry is resolved the same way the program will be, so a list may be
// written as bare names, absolute paths, or a mix. An entry that resolves to
// nothing fails the build rather than the first message that reached for it.
func buildAllowList(
	cfg types.BlockConfig, program *expr.Program, env map[string]any,
) (map[string]bool, error) {
	if len(cfg.Allow) == 0 {
		constant, isConstant := constantProgram(program, env)
		if !isConstant {
			slog.Warn("cli-run has no allow list and takes its program from the message, so it "+
				"will run whatever it is handed: declare an allow list before this flow is "+
				"reachable by anyone but you",
				"block", blockLabel(blockKindCLIRun, cfg.Name), "program", cfg.Program)
			return nil, nil
		}
		// A program named outright is still resolved now. There is no list to check
		// it against, but a literal that resolves to nothing can never run, and
		// saying so at startup costs nothing.
		if _, err := resolveProgram(constant); err != nil {
			return nil, fmt.Errorf("cli-run: %w", err)
		}
		return nil, nil
	}

	allowed := make(map[string]bool, len(cfg.Allow))
	for _, entry := range cfg.Allow {
		resolved, err := checkAllowEntry(entry, cfg.AllowInterpreters)
		if err != nil {
			return nil, fmt.Errorf("cli-run: %w", err)
		}
		allowed[resolved] = true
	}

	// A program that never varies and is not on the list can never run, so say so
	// now. Left to the runtime check it would build cleanly and then fail on every
	// single message, which is the failure this file exists to move to startup.
	if constant, ok := constantProgram(program, env); ok {
		resolved, err := resolveProgram(constant)
		if err != nil {
			return nil, fmt.Errorf("cli-run: %w", err)
		}
		if !allowed[resolved] {
			return nil, fmt.Errorf(
				"cli-run program is always %q, which the allow list does not include: it could "+
					"never run", constant)
		}
	}
	return allowed, nil
}

// constantProgram reports the program expression's value when it has one that
// does not depend on the message.
//
// It is decided by evaluating against an empty message: an expression reading
// body or vars fails on it, while a literal — or one built from env, which is
// resolved by now — succeeds with the value it will always have. An expression
// that happens to evaluate on an empty message but would differ on a real one is
// pinned to what it produced here, so a later value is refused rather than
// allowed. The mis-detection errs toward running less.
func constantProgram(program *expr.Program, env map[string]any) (string, bool) {
	msg, err := types.NewMessage("")
	if err != nil {
		return "", false
	}
	value, err := program.EvalString(expr.MessageActivation(msg, env))
	if err != nil || value == "" {
		return "", false
	}
	return value, true
}

// cliTimeout parses the configured bound, defaulting when unset. A command holds
// a flow worker for as long as it runs, so there is no unbounded option.
func cliTimeout(raw string) (time.Duration, error) {
	if raw == "" {
		return defaultCommandTimeout, nil
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("cli-run timeout %q: %w", raw, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("cli-run timeout %q must be positive", raw)
	}
	return timeout, nil
}

// cliOnExit validates the non-zero-exit policy.
func cliOnExit(raw string) (string, error) {
	switch raw {
	case "":
		return onExitFail, nil
	case onExitFail, onExitContinue:
		return raw, nil
	default:
		return "", fmt.Errorf("cli-run onExit %q must be %q or %q", raw, onExitFail, onExitContinue)
	}
}

// compileCLI compiles an optional CEL field, naming it in any error.
func compileCLI(b *builder, expression, field string) (*expr.Program, error) {
	if expression == "" {
		return nil, nil //nolint:nilnil // an unset optional expression is not an error
	}
	prog, err := expr.CompileMessage(b.deps.Resources, expression)
	if err != nil {
		return nil, fmt.Errorf("cli-run %s: %w", field, err)
	}
	return prog, nil
}

// configureCLIEvents builds the observer path, if there is one.
func (b *builder) configureCLIEvents(block *cliRun, cfg types.BlockConfig) error {
	if cfg.Events == nil {
		if len(cfg.Emit) > 0 {
			return errors.New("cli-run emit requires an events path to filter")
		}
		return nil
	}
	flow, err := b.branch(core.BranchEvents).subFlow(*cfg.Events)
	if err != nil {
		return fmt.Errorf("cli-run events: %w", err)
	}
	kinds, err := emitKinds(blockKindCLIRun, cliEventKinds, cfg.Emit)
	if err != nil {
		return err
	}
	block.events = &emitter{flow: flow, kinds: kinds, label: blockKindCLIRun}
	return nil
}

// Process runs the command and replaces the body with how it went.
func (c *cliRun) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	program, err := c.evalProgram(msg)
	if err != nil {
		return nil, err
	}
	args, err := c.evalArgs(msg)
	if err != nil {
		return nil, err
	}
	stdin, err := c.evalStdin(msg)
	if err != nil {
		return nil, err
	}

	result, err := c.command.run(ctx, program, args, stdin, c.lineHandler(ctx, msg))
	if errors.Is(err, errCLIStopped) {
		// The events path asked to stop, which is an ordinary end and not a
		// failure: nobody is listening any more. Carry back what the command
		// managed to produce and stop the chain, rather than routing a hung-up
		// caller into the flow's error path.
		if result != nil {
			msg.SetBody(cliResultBody(result))
		}
		msg.RequestStop()
		return msg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cli-run %s: %w", blockLabel(blockKindCLIRun, c.name), err)
	}

	c.emitExit(ctx, msg, result)
	msg.SetBody(cliResultBody(result))
	if c.onExit == onExitFail && result.exitCode != 0 {
		return nil, fmt.Errorf("cli-run %s: %q exited %d",
			blockLabel(blockKindCLIRun, c.name), program, result.exitCode)
	}
	return msg, nil
}

// evalProgram resolves which program to run, and refuses anything the block's
// allow list does not cover.
//
// This is the enforcement point, and it runs before a process is spawned. The
// program can come from the message — an ai-agent tool branch handing a model a
// set of commands is the case it exists for — so the check belongs here rather
// than at build time.
//
// It compares resolved paths, not the strings the author and the caller happened
// to type. "ls" and "/bin/ls" are the same program and match the same entry;
// "/bin/../bin/sh" cannot dodge an entry for "/bin/sh". A nil list means the
// block was given no restriction, so anything that resolves runs.
func (c *cliRun) evalProgram(msg *types.Message) (string, error) {
	name, err := c.program.EvalString(expr.MessageActivation(msg, c.env))
	if err != nil {
		return "", fmt.Errorf("cli-run program: %w", err)
	}
	resolved, err := resolveProgram(name)
	if err != nil {
		return "", fmt.Errorf("cli-run %s: %w", blockLabel(blockKindCLIRun, c.name), err)
	}
	if c.allowed != nil && !c.allowed[resolved] {
		return "", fmt.Errorf("cli-run %s: program %q is not in this block's allow list",
			blockLabel(blockKindCLIRun, c.name), name)
	}
	return resolved, nil
}

// lineHandler returns the per-line callback, or nil when nothing is watching, in
// which case the run costs nothing extra. Either way the output is captured; this
// only decides whether anyone hears about it while the command is still going.
func (c *cliRun) lineHandler(ctx context.Context, msg *types.Message) func(commandLine) error {
	if c.events == nil {
		return nil
	}
	return func(line commandLine) error {
		if c.events.send(ctx, msg, line.stream, map[string]any{
			"text":  line.text,
			"index": line.index,
		}) {
			// The events path asked to stop, which means nobody is listening any
			// more — an sse-event whose caller hung up is the ordinary cause. Failing
			// the handler is how that reaches the process: run stops reading, and the
			// command is killed rather than left producing output for no one.
			return errCLIStopped
		}
		return nil
	}
}

// errCLIStopped ends a run because the events path asked it to. It travels as an
// error because a line handler has no other channel, and is unwrapped into an
// ordinary stop rather than a failure.
var errCLIStopped = errors.New("cli-run events path requested stop")

// emitExit reports the terminal event.
//
// It always fires, even for a command that printed nothing — which is exactly
// why it is a separate event rather than a flag on the last line. A silent
// command would otherwise never report its exit status at all, and an SSE flow
// would have nowhere to send its done frame.
func (c *cliRun) emitExit(ctx context.Context, msg *types.Message, result *commandResult) {
	c.events.send(ctx, msg, cliEventExit, map[string]any{
		"exitCode": result.exitCode,
		"lines":    result.lines,
	})
}

// cliResultBody is what the rest of the flow carries on with.
//
// stdout and stderr are filled whether or not an events path watched the lines go
// by. Watching is an observation, not a consumption, and a block that reported
// "lines: 18" alongside an empty stdout was contradicting itself on its face.
func cliResultBody(result *commandResult) map[string]any {
	return map[string]any{
		"exitCode": result.exitCode,
		"stdout":   result.stdout,
		"stderr":   result.stderr,
		"lines":    result.lines,
	}
}

// evalArgs evaluates the argument expression, requiring a list of strings.
func (c *cliRun) evalArgs(msg *types.Message) ([]string, error) {
	if c.args == nil {
		return nil, nil
	}
	value, err := c.args.Eval(expr.MessageActivation(msg, c.env))
	if err != nil {
		return nil, fmt.Errorf("cli-run args: %w", err)
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("cli-run args must evaluate to a list of strings, got %T", value)
	}
	args := make([]string, 0, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("cli-run args[%d] must be a string, got %T", i, item)
		}
		args = append(args, s)
	}
	return args, nil
}

// evalStdin evaluates the standard-input expression.
func (c *cliRun) evalStdin(msg *types.Message) (string, error) {
	if c.stdin == nil {
		return "", nil
	}
	stdin, err := c.stdin.EvalString(expr.MessageActivation(msg, c.env))
	if err != nil {
		return "", fmt.Errorf("cli-run stdin: %w", err)
	}
	return stdin, nil
}
