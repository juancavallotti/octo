// Package runner runs a case against octo and says what came back.
//
// One case is one `octo invoke`, in its own process. That is not a shape we chose for
// isolation's sake — it is what the debug seam is. Mocks and spies are baked into the
// flow tree when it is BUILT, by rewriting the config, so a running service can only
// ever serve the one case it was built for. There is no way to run a second case
// through it.
//
// What dolphin writes for octo is a --run-debug-config file, unchanged: the input, the
// mocks, the spied addresses. A case IS a debugging session, so there is no second
// format on the wire, and the command a failing case prints is one the user can paste
// into a terminal and watch fail again.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
	"github.com/juancavallotti/octo/runtime/types"
)

// timeoutGrace is how much longer than the flow's own timeout the child process gets
// before dolphin kills it. octo bounds the flow call itself; this bounds octo. Without
// it, a child wedged before it ever reaches the flow — a connector that will not start,
// a DNS lookup with no deadline — would hang the whole suite with nothing to blame.
const timeoutGrace = 10 * time.Second

// childLogLevel quiets the child's startup logs. They are captured either way, and
// shown only when a case fails; at info level every case would print a page of
// connector chatter nobody asked for.
const childLogLevel = "LOG_LEVEL=error"

// debugConfigMode is the permission for the temp debug config written per case.
const debugConfigMode = 0o600

// errorField is how slog renders the error on octo's last log line before it exits. It
// is what octoError unwraps, so the user reads octo's complaint and not its logging.
const errorField = `error="`

// Outcome is what one case did: what octo reported, and what dolphin had to do to get
// it. It is deliberately not a verdict — deciding whether this is what the case
// EXPECTED is the assert package's job, and keeping the two apart is what lets a
// failure report the real outcome rather than just "no".
type Outcome struct {
	// Result is the message the flow produced. Nil when it dropped or failed.
	Result *types.Message
	// Dropped reports that the flow filtered the message out.
	Dropped bool
	// Error is the flow's failure, empty when it did not fail. It is a *flow* failure,
	// not a dolphin one: octo reports it in the envelope and exits 0.
	Error string
	// Spies is what each watched block saw, keyed by address.
	Spies map[string][]core.SpyRecord

	// Command is the octo invocation that produced this, ready to paste into a shell.
	// It is the single biggest thing the subprocess model buys: every failure hands you
	// a runnable command instead of a description of one.
	Command string
	// Stderr is the child's, shown only when something went wrong.
	Stderr string
	// Elapsed is how long the child took.
	Elapsed time.Duration
}

// envelope is `octo invoke --envelope`'s output. It mirrors the CLI's debugOutcome —
// the fields dolphin reads, and no more.
type envelope struct {
	Result  *types.Message `json:"result"`
	Dropped bool           `json:"dropped"`
	Error   string         `json:"error"`
	Spies   []spyTrace     `json:"spies"`
}

// spyTrace is what one spied block saw.
type spyTrace struct {
	Address string           `json:"address"`
	Records []core.SpyRecord `json:"records"`
}

// debugConfig is the file handed to `octo invoke --run-debug-config`. The field names
// are octo's, because this is octo's format.
type debugConfig struct {
	Input debugInput               `yaml:"input,omitempty"`
	Spies []string                 `yaml:"spies,omitempty"`
	Mocks map[string]core.MockSpec `yaml:"mocks,omitempty"`
}

type debugInput struct {
	Data any             `yaml:"data,omitempty"`
	Vars types.Variables `yaml:"vars,omitempty"`
}

// Options are what every case in a run shares.
type Options struct {
	// Octo is the path to the octo binary, already resolved and probed.
	Octo string
	// WorkDir is where the per-case debug configs are written. A failing case's file is
	// left behind on purpose: its Command names it, and a command that points at a file
	// dolphin has deleted is a command that cannot be run.
	WorkDir string
	// Verbose lets the child's logs through at their normal level.
	Verbose bool
}

// invoke executes one case and reports what octo said.
//
// The error return is for dolphin's own failures — octo could not be started, the
// config could not be written, the envelope could not be read. A flow that FAILED is
// not one of those: it comes back in Outcome.Error, because "the flow failed" is a
// perfectly good thing for a case to be testing.
func invoke(ctx context.Context, opts Options, target suite.Target, index int, c suite.Case) (Outcome, error) {
	timeout := target.File.TimeoutFor(c)

	configPath, err := writeDebugConfig(opts.WorkDir, target, index, c)
	if err != nil {
		return Outcome{}, err
	}

	args := invokeArgs(target, configPath, timeout)
	outcome := Outcome{Command: shellCommand(opts.Octo, args)}

	// The child gets longer than the flow does, so that a timeout is reported by octo —
	// which knows it was the flow that ran long — rather than by dolphin killing a
	// process it cannot explain.
	runCtx, cancel := context.WithTimeout(ctx, timeout+timeoutGrace)
	defer cancel()

	cmd := exec.CommandContext(runCtx, opts.Octo, args...) //nolint:gosec // G204: a resolved octo, and args dolphin built
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = childEnv(opts.Verbose)

	started := time.Now()
	runErr := cmd.Run()
	outcome.Elapsed = time.Since(started)
	outcome.Stderr = strings.TrimSpace(stderr.String())

	if runErr != nil {
		return outcome, invokeError(runCtx, runErr, outcome, timeout)
	}
	if err := decodeEnvelope(stdout.Bytes(), &outcome); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// invokeError explains a child that did not exit cleanly.
//
// Every one of these is a bad request rather than a test result: an unresolvable block
// address, a mock spec the engine refused, a config that does not parse, a flow that is
// not in it. octo exits non-zero for those and 0 for a flow that merely failed — which
// is exactly the line dolphin needs, and exactly why --envelope exists.
func invokeError(ctx context.Context, runErr error, outcome Outcome, timeout time.Duration) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("octo did not finish within %v (the case's timeout plus %v)", timeout, timeoutGrace)
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// The child's stderr is the whole story here — it holds octo's error — so lead
		// with it rather than with an exit status nobody can act on.
		return fmt.Errorf("octo rejected the run: %s", octoError(outcome.Stderr))
	}
	return fmt.Errorf("run octo: %w", runErr)
}

// octoError pulls octo's complaint out of its logs.
//
// octo is a service, so it reports a fatal error through slog, as a structured line:
//
//	time=… level=ERROR msg="cli stopped with error" error="spy \"demo.nope\": no block …"
//
// Reprinting that whole line at the user buries the one part they need — the message —
// in machinery they did not ask about. So the error= field is unquoted back out of it.
// A line that does not carry one is printed as it is: a guess about the format is worth
// making, and worth abandoning the moment it does not hold.
func octoError(stderr string) string {
	line := lastLine(stderr)

	_, after, found := strings.Cut(line, errorField)
	if !found {
		return firstLine(stderr)
	}
	unquoted, err := strconv.Unquote(`"` + strings.TrimSuffix(after, `"`) + `"`)
	if err != nil {
		return firstLine(stderr)
	}
	return unquoted
}

// lastLine is the final non-empty line of s. octo's fatal error is the last thing it
// logs, and a run that also logged warnings on the way down would otherwise report the
// first of those instead.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

// decodeEnvelope reads what octo printed into the outcome.
func decodeEnvelope(stdout []byte, outcome *Outcome) error {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return fmt.Errorf("octo printed no outcome (stderr: %s)", firstLine(outcome.Stderr))
	}

	var env envelope
	if err := json.Unmarshal(trimmed, &env); err != nil {
		return fmt.Errorf("read octo's outcome: %w (it printed: %s)", err, firstLine(string(trimmed)))
	}

	outcome.Result = env.Result
	outcome.Dropped = env.Dropped
	outcome.Error = env.Error
	if len(env.Spies) > 0 {
		outcome.Spies = make(map[string][]core.SpyRecord, len(env.Spies))
		for _, trace := range env.Spies {
			outcome.Spies[trace.Address] = trace.Records
		}
	}
	return nil
}

// writeDebugConfig writes the case out as the --run-debug-config file octo reads, and
// returns its path.
func writeDebugConfig(dir string, target suite.Target, index int, c suite.Case) (string, error) {
	input := target.File.InputFor(c)
	cfg := debugConfig{
		Input: debugInput{Data: input.Data, Vars: input.Vars},
		Spies: suite.SpiedBy(c),
		Mocks: target.File.MocksFor(c),
	}

	encoded, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("encode the run for %q: %w", c.Name, err)
	}

	// Named after the suite and the case's position, so a workdir full of them can be
	// read, and so two cases never collide.
	base := filepath.Base(target.File.Path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	path := filepath.Join(dir, fmt.Sprintf("%s.%d.yaml", stem, index))
	if err := os.WriteFile(path, encoded, debugConfigMode); err != nil {
		return "", fmt.Errorf("write the run for %q: %w", c.Name, err)
	}
	return path, nil
}

// invokeArgs is the octo invocation for a case.
func invokeArgs(target suite.Target, configPath string, timeout time.Duration) []string {
	return []string{
		"invoke",
		"--config", target.Config,
		"--flow", target.File.Flow,
		"--run-debug-config", configPath,
		"--timeout", timeout.String(),
		// Without this, a dropped flow prints nothing at all and a failed one prints its
		// error to the logs and exits 1 — three outcomes in three shapes, none of them
		// parseable. --envelope makes octo say all three the same way.
		"--envelope",
	}
}

// childEnv is the environment octo runs in. It inherits dolphin's — a flow's env
// resources and ${NAME} references resolve against it, so a suite can be run with the
// same variables as the real thing — with the log level quieted unless asked.
func childEnv(verbose bool) []string {
	env := os.Environ()
	if verbose {
		return env
	}
	return append(env, childLogLevel)
}

// shellCommand renders the invocation as a line the user can paste into a terminal.
func shellCommand(bin string, args []string) string {
	quoted := make([]string, 0, len(args)+1)
	quoted = append(quoted, bin)
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

// shellQuote quotes an argument that a shell would otherwise split or expand. A block
// address is the reason: `demo.each-order[body].classify-order` is a glob to a shell.
func shellQuote(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\"'\\$`*?[]{}()<>|&;#~!") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

// firstLine is the first line of s, for an error that must stay one line.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(nothing)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
