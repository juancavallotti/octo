// Running a local program: checking it is one the flow is allowed to run,
// spawning it, pumping its two output streams, and reporting how it ended.
//
// This is the machinery under the cli-run block, kept apart from it so the block
// file is about the flow shape and this one is about the process.
package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The two output streams a command line can come from. They are the event kinds
// a cli-run's events path filters on, so they are spelled once here.
const (
	commandStdout = "stdout"
	commandStderr = "stderr"
)

// defaultCommandTimeout bounds a command the flow put no limit on. A command
// holds a flow worker for as long as it runs, so "no limit" is not an option
// worth offering.
const defaultCommandTimeout = 30 * time.Second

// defaultMaxOutputBytes caps captured output, so a chatty program cannot grow the
// runtime's heap without bound.
const defaultMaxOutputBytes int64 = 1 << 20

// maxLineBytes caps one line of output. bufio.Scanner's own default is 64 KiB and
// it reports an over-long line as an error, which would fail a command for the
// shape of its output rather than for what it did — so the limit is raised, and a
// line past it is reported as truncated instead of failing the run.
const maxLineBytes = 1 << 20

// commandWaitDelay is how long a child gets after its context is cancelled before
// it is killed outright. exec.CommandContext kills the process itself, but a
// grandchild holding the output pipe open would keep Wait blocked forever;
// WaitDelay is what bounds that.
const commandWaitDelay = 5 * time.Second

// interpreters are refused on a cli-run's allow list unless allowInterpreters
// says otherwise. An interpreter's arguments ARE a program, so listing one turns
// the list into a formality — the block could then run anything through it.
//
// It only applies to an explicit list, because that is the only case where it
// means anything: a block with no list has already said "anything", so there is
// nothing left to guard.
//
// This is a guardrail against an easy mistake, not a sandbox, and the list can
// never be complete: git runs a pager, tar has --to-command, find has -exec. The
// list is the boundary either way.
var interpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "csh": true, "fish": true,
	"env": true, "xargs": true, "awk": true, "gawk": true,
	"python": true, "python3": true, "perl": true, "ruby": true, "node": true, "php": true,
}

// resolveProgram turns a program name into the absolute path that will actually
// run: a bare name is looked up on $PATH, an absolute path is checked as it
// stands, and either way the result is cleaned.
//
// Resolving both the allow list and the program through the same function is what
// makes matching them honest. "ls" and "/bin/ls" name the same binary and now
// compare equal, and so do "/bin/sh" and "/bin/../bin/sh" — a spelling can
// neither dodge the list nor sneak onto it.
func resolveProgram(program string) (string, error) {
	if program == "" {
		return "", errors.New(
			"program expression produced an empty name, so there is nothing to run")
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return "", fmt.Errorf("program %q: %w", program, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("program %q: %w", program, err)
	}
	return filepath.Clean(abs), nil
}

// checkAllowEntry resolves one allow-list entry when the flow is built, so an
// entry naming a program that is not there fails at startup rather than on the
// first message that reached for it. It returns the resolved path the entry
// stands for.
func checkAllowEntry(program string, allowInterpreters bool) (string, error) {
	resolved, err := resolveProgram(program)
	if err != nil {
		return "", fmt.Errorf("allowed %w", err)
	}
	if !allowInterpreters && interpreters[filepath.Base(resolved)] {
		return "", fmt.Errorf(
			"allowed program %q is an interpreter: its arguments are themselves a program, so "+
				"allowing it allows everything it can reach. Set allowInterpreters: true if that "+
				"is the intent", program)
	}
	return resolved, nil
}

// commandLine is one line of a running command's output.
type commandLine struct {
	// stream is which of the process's two outputs this came from.
	stream string
	// text is the line without its trailing newline.
	text string
	// index counts lines across both streams in arrival order, from zero.
	index int
}

// commandResult is how a finished command turned out.
type commandResult struct {
	// exitCode is the process's exit status: 0 for success, and non-zero for a
	// failure, a signal, or the timeout, the way a shell would report it.
	exitCode int
	// stdout and stderr hold the captured output, up to the configured cap. They
	// are filled whether or not anything watched the lines go by: the events path
	// is an observer, so watching output must not consume it.
	stdout string
	stderr string
	// lines counts the output lines produced, streamed or buffered.
	lines int
}

// command is everything about running a program that was decided when the flow
// was built: where it runs, what environment it sees, and its bounds. Which
// program, and with what arguments, comes per message.
type command struct {
	workDir        string
	env            []string
	timeout        time.Duration
	maxOutputBytes int64
}

// run executes the program with these arguments, reporting each output line to on
// as it is read. The output is captured on the result either way.
//
// on is called on the calling goroutine, in order, between reads of the process's
// pipes — so a slow handler backpressures the child rather than buffering without
// bound. That is what lets a flow stream a chatty command to an SSE caller with
// no queue to size: while the handler runs, neither pipe is drained, and the
// process blocks on write.
//
// A non-zero exit is reported on the result, not as an error: it is an outcome of
// the command, and whether it should fail the flow is the block's decision. An
// error means the command could not be run or could not be waited for.
func (c *command) run(
	ctx context.Context, program string, args []string, stdin string, on func(commandLine) error,
) (*commandResult, error) {
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	//nolint:gosec // G204: program is the absolute path cliRun.evalProgram resolved and then
	// checked against this block's allow list, when it declares one. args are passed as argv
	// with no shell, so nothing in them is interpreted.
	cmd := exec.CommandContext(runCtx, program, args...)
	cmd.Dir = c.workDir
	// Exactly the declared passthrough and nothing else, so a secret in the
	// runtime's own environment cannot reach a program a flow runs. A nil slice
	// would inherit everything, which is the opposite of the intent.
	cmd.Env = c.env
	// A child that ignores the kill, or leaves a grandchild holding the output
	// pipe, must not block Wait forever.
	cmd.WaitDelay = commandWaitDelay

	return runCommand(cmd, stdin, on, c.maxOutputBytes)
}

// runCommand starts the process, pumps both output streams to EOF, and waits.
func runCommand(
	cmd *exec.Cmd, stdin string, on func(commandLine) error, maxOutput int64,
) (*commandResult, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("cli-run: stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("cli-run: stderr pipe: %w", err)
	}
	cmd.Stdin = strings.NewReader(stdin)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cli-run: start %q: %w", cmd.Path, err)
	}

	sink := &lineSink{on: on, max: maxOutput}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); sink.pump(commandStdout, stdout) }()
	go func() { defer wg.Done(); sink.pump(commandStderr, stderr) }()
	// Both pipes must be drained to EOF before Wait, which closes them.
	wg.Wait()

	result := &commandResult{lines: sink.count()}
	result.stdout, result.stderr = sink.buffered()
	if result.exitCode, err = waitFor(cmd); err != nil {
		return nil, err
	}
	// A handler failure is reported after the wait, so the process is reaped
	// either way — the caller asked to stop reading, not to leak a child.
	if handlerErr := sink.err(); handlerErr != nil {
		return nil, handlerErr
	}
	return result, nil
}

// waitFor waits for the process, separating "it exited badly" from "it could not
// be run". A non-zero exit is an outcome the caller decides about; anything else
// is a failure to report.
func waitFor(cmd *exec.Cmd) (int, error) {
	err := cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, fmt.Errorf("cli-run: wait for %q: %w", cmd.Path, err)
}

// commandEnv resolves the named variables from the runtime's environment into the
// child's, skipping ones that are not set. It always returns a non-nil slice:
// exec treats a nil Env as "inherit everything", which is what this setting
// exists to prevent.
func commandEnv(names []string) []string {
	env := make([]string, 0, len(names))
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// lineSink turns the two pipes into lines: buffering every one, and handing it to
// the caller as it arrives when there is a caller to hand it to.
//
// It does both rather than choosing, because they answer different questions. The
// handler is how a line reaches a watcher WHILE the command runs; the buffer is
// what the rest of the flow reads AFTER it ends. Making the second conditional on
// the first is what once made a block with an events path report a line count and
// no output — the watcher had quietly eaten it.
//
// Both pumps share it under one mutex, which does double duty: it keeps the index
// and the buffers consistent, and it serializes the handler, so on is called one
// line at a time even though two goroutines are reading. That serialization is
// what makes the backpressure work.
type lineSink struct {
	mu      sync.Mutex
	on      func(commandLine) error
	max     int64
	index   int
	written int64
	out     strings.Builder
	errOut  strings.Builder
	handler error
}

// pump reads one stream to EOF, reporting each line.
func (s *lineSink) pump(stream string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineBytes)
	for scanner.Scan() {
		s.add(stream, scanner.Text())
	}
	// A read error — an over-long line, or a pipe closed under us — ends this
	// stream but not the command. The process's own exit status is the answer that
	// matters, and failing a run because its output was oddly shaped would be the
	// wrong call; saying so in the output is not.
	if err := scanner.Err(); err != nil {
		s.add(stream, fmt.Sprintf("[output truncated: %v]", err))
	}
}

// add records one line and reports it, and stops calling the handler once one has
// failed.
func (s *lineSink) add(stream, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	line := commandLine{stream: stream, text: text, index: s.index}
	s.index++
	s.buffer(stream, text)

	// A handler that has already failed is not called again: it asked to stop.
	// The buffering above continues regardless, so the flow still sees whatever
	// the command produced before it was told nobody was listening.
	if s.on == nil || s.handler != nil {
		return
	}
	if err := s.on(line); err != nil {
		s.handler = err
	}
}

// buffer appends a line to the captured output, up to the cap. Past it output is
// dropped rather than growing the heap; the line count still reflects everything
// the command produced, so a truncated capture is visible rather than silent.
func (s *lineSink) buffer(stream, text string) {
	if s.written >= s.max {
		return
	}
	s.written += int64(len(text)) + 1
	if stream == commandStderr {
		s.errOut.WriteString(text)
		s.errOut.WriteByte('\n')
		return
	}
	s.out.WriteString(text)
	s.out.WriteByte('\n')
}

func (s *lineSink) buffered() (stdout, stderr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.out.String(), s.errOut.String()
}

func (s *lineSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index
}

func (s *lineSink) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler
}
