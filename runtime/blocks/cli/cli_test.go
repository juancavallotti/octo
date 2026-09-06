package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// catPath is the program these tests run. /bin/cat is a regular executable file
// at that path on macOS, Debian and Alpine (where it is a busybox symlink, which
// os.Stat follows), it needs no shell, and echoing stdin makes its output
// predictable. A platform without it skips rather than fails.
const catPath = "/bin/cat"

func requireProgram(t *testing.T, path string) {
	t.Helper()
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		t.Skipf("%s is not available here", path)
	}
}

// cliConfig is a cli-run that echoes the body's text back through cat.
func cliConfig() types.BlockConfig {
	return types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": `"` + catPath + `"`, "stdin": "body.text"}}
}

// buildCLI builds a cli-run block, failing the test if it does not build.
//
//nolint:ireturn // a built block holds the MessageProcessor interface
func buildCLI(t *testing.T, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	return buildBlock(t, cliRegistry(), depsRes(mapResources{}), cfg)
}

// tryBuildCLI builds a cli-run block, for a test that expects the build to fail.
//
//nolint:ireturn // a built block holds the MessageProcessor interface
func tryBuildCLI(cfg types.BlockConfig) (core.MessageProcessor, error) {
	return tryBuildBlock(cliRegistry(), depsRes(mapResources{}), cfg)
}

// cliRegistry adds a block that asks the flow to stop, which is what an
// sse-event with `ifClosed: stop` does when its caller has hung up. The engine
// package cannot import the http connector, so the behaviour is stood in for.
func cliRegistry() *core.BlockRegistry {
	reg := recordRegistry(&[]any{})
	reg.MustRegister("cli.stop", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.RequestStop()
			return msg, nil
		}), nil
	})
	return reg
}

// runCLI pushes one message through the block and returns the resulting body.
func runCLI(t *testing.T, proc core.MessageProcessor, text string) map[string]any {
	t.Helper()
	msg := newMessageBody(t, `{"text":`+quote(text)+`}`)
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	body, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want a map", out.Body)
	}
	return body
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "\n", `\n`) + `"`
}

func TestCLIRunBuffersOutput(t *testing.T) {
	requireProgram(t, catPath)
	body := runCLI(t, buildCLI(t, cliConfig()), "one\ntwo\nthree\n")

	if body["exitCode"] != 0 {
		t.Errorf("exitCode = %v, want 0", body["exitCode"])
	}
	if body["stdout"] != "one\ntwo\nthree\n" {
		t.Errorf("stdout = %q", body["stdout"])
	}
	if body["lines"] != 3 {
		t.Errorf("lines = %v, want 3", body["lines"])
	}
}

// TestCLIRunBuildValidation: every one of these would otherwise surface as a
// broken command in production, on the first message that needed it.
func TestCLIRunBuildValidation(t *testing.T) {
	requireProgram(t, catPath)
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(notExecutable, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cases := []struct {
		name  string
		edit  func(*types.BlockConfig)
		wants string
	}{
		{"no program", func(c *types.BlockConfig) { c.Settings["program"] = "" }, "requires a program"},
		{
			"a program that does not exist",
			func(c *types.BlockConfig) { c.Settings["program"] = `"/nope/nothing"` },
			"/nope/nothing",
		},
		{
			"a directory",
			func(c *types.BlockConfig) { c.Settings["program"] = `"` + dir + `"` },
			dir,
		},
		{
			"a non-executable file",
			func(c *types.BlockConfig) { c.Settings["program"] = `"` + notExecutable + `"` },
			notExecutable,
		},
		{
			"an allow list entry that does not exist",
			func(c *types.BlockConfig) {
				c.Settings["program"], c.Settings["allow"] = "body.program", []string{"definitely-not-a-program"}
			},
			"definitely-not-a-program",
		},
		{
			// A program that never varies and is not on the list can never run.
			"a constant program the allow list omits",
			func(c *types.BlockConfig) { c.Settings["allow"] = []string{"/usr/bin/false"} },
			"could never run",
		},
		{"a bad timeout", func(c *types.BlockConfig) { c.Settings["timeout"] = "soon" }, "timeout"},
		{"a negative timeout", func(c *types.BlockConfig) { c.Settings["timeout"] = "-1s" }, "must be positive"},
		{"an unknown onExit", func(c *types.BlockConfig) { c.Settings["onExit"] = "shrug" }, "onExit"},
		{"emit with no events path", func(c *types.BlockConfig) { c.Settings["emit"] = []string{"stdout"} }, "requires an events path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cliConfig()
			tc.edit(&cfg)
			_, err := tryBuildCLI(cfg)
			if err == nil {
				t.Fatalf("build should have failed for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q should mention %q", err, tc.wants)
			}
		})
	}
}

// An interpreter's arguments are themselves a program, so allowing one makes the
// allowlist a formality. It has to be opted into deliberately.
func TestCLIRunRefusesAnInterpreterUnlessAsked(t *testing.T) {
	const shell = "/bin/sh"
	requireProgram(t, shell)

	// The check is on the allow LIST, which is the only place it means anything:
	// a block with no list has already said "anything".
	cfg := cliConfig()
	cfg.Settings["program"] = `"` + shell + `"`
	cfg.Settings["allow"] = []string{shell}
	_, err := tryBuildCLI(cfg)
	if err == nil {
		t.Fatal("an interpreter on the allow list should be refused by default")
	}
	if !strings.Contains(err.Error(), "allowInterpreters") {
		t.Errorf("error %q should name the setting that permits it", err)
	}

	cfg.Settings["allowInterpreters"] = true
	if _, err := tryBuildCLI(cfg); err != nil {
		t.Errorf("allowInterpreters: true should permit a shell: %v", err)
	}
}

// TestCLIRunResolvesThroughPath: a bare name is looked up on $PATH, so a flow can
// say "cat" without hard-coding where the platform keeps it.
func TestCLIRunResolvesThroughPath(t *testing.T) {
	requireProgram(t, catPath)
	cfg := cliConfig()
	cfg.Settings["program"] = `"cat"`
	body := runCLI(t, buildCLI(t, cfg), "one\n")
	if body["exitCode"] != 0 {
		t.Errorf("a bare name should resolve through $PATH: %v", body)
	}
}

// TestCLIRunMatchesResolvedPaths is why both sides go through resolveProgram: a
// spelling should neither dodge an allow list nor sneak onto one.
//
// The absolute path is looked up rather than hardcoded, because where cat lives
// is not the same everywhere — /bin/cat on macOS, /usr/bin/cat on a usr-merged
// Linux. Symlinks are not followed (see resolveProgram), so the two are NOT
// interchangeable and the test must not pretend they are.
func TestCLIRunMatchesResolvedPaths(t *testing.T) {
	catAbs, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat is not on PATH here")
	}
	catAbs, err = filepath.Abs(catAbs)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	cfg := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		// Listed by bare name; the calls below name it two other ways.
		Settings: types.Settings{"program": "body.program", "allow": []string{"cat"}, "stdin": `"hi\n"`}}
	proc := buildCLI(t, cfg)

	// The same program, spelled as the bare name, as the absolute path the bare
	// name resolves to, and with a .. segment that cleans to that same path.
	// Built by concatenation, not filepath.Join: Join cleans as it goes, which
	// would collapse the ".." before the block ever saw it.
	dir := filepath.Dir(catAbs)
	viaDotDot := dir + "/../" + filepath.Base(dir) + "/" + filepath.Base(catAbs)
	for _, spelling := range []string{"cat", catAbs, viaDotDot} {
		if _, err := proc.Process(context.Background(),
			newMessageBody(t, `{"program":`+quote(spelling)+`}`)); err != nil {
			t.Errorf("%q names the same program as the allow list entry: %v", spelling, err)
		}
	}
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"program":"echo"}`)); err == nil {
		t.Error("a different program must still be refused")
	}
}

// TestCLIRunWithNoAllowListRunsAnything is the development default the block is
// tuned for: no list means no restriction.
func TestCLIRunWithNoAllowListRunsAnything(t *testing.T) {
	requireProgram(t, catPath)
	cfg := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": "body.program", "stdin": `"hi\n"`}}
	proc := buildCLI(t, cfg)
	if _, err := proc.Process(context.Background(),
		newMessageBody(t, `{"program":"`+catPath+`"}`)); err != nil {
		t.Errorf("with no allow list any resolvable program should run: %v", err)
	}
	// It still has to exist.
	if _, err := proc.Process(context.Background(),
		newMessageBody(t, `{"program":"definitely-not-a-program"}`)); err == nil {
		t.Error("a program that does not resolve should still fail")
	}
}

func TestCLIRunArgsMustBeStrings(t *testing.T) {
	requireProgram(t, catPath)
	cfg := cliConfig()
	cfg.Settings["args"] = "[1, 2]"
	proc := buildCLI(t, cfg)
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"text":"x"}`)); err == nil {
		t.Error("args that are not strings should fail the message")
	}
}

// A non-zero exit is the block's decision, not the runner's.
func TestCLIRunOnExit(t *testing.T) {
	const falsePath = "/usr/bin/false"
	requireProgram(t, falsePath)

	failing := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": `"` + falsePath + `"`}}
	if _, err := buildCLI(t, failing).Process(context.Background(), newMessageBody(t, `{}`)); err == nil {
		t.Error("onExit defaults to fail, so a non-zero exit should error the block")
	}

	failing.Settings["onExit"] = onExitContinue
	out, err := buildCLI(t, failing).Process(context.Background(), newMessageBody(t, `{}`))
	if err != nil {
		t.Fatalf("onExit: continue should not error: %v", err)
	}
	body := out.Body.(map[string]any)
	if body["exitCode"] == 0 {
		t.Errorf("exitCode = %v, want non-zero so a later block can decide", body["exitCode"])
	}
}

func TestCLIRunTimeoutKillsTheCommand(t *testing.T) {
	const sleepPath = "/bin/sleep"
	requireProgram(t, sleepPath)

	cfg := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": `"` + sleepPath + `"`, "args": `["30"]`, "timeout": "100ms", "onExit": onExitContinue}}
	out, err := buildCLI(t, cfg).Process(context.Background(), newMessageBody(t, `{}`))
	if err != nil {
		t.Fatalf("a killed command should report an exit code, not an error: %v", err)
	}
	if out.Body.(map[string]any)["exitCode"] == 0 {
		t.Error("a command killed by the timeout should not report success")
	}
}

// The child sees only the declared passthrough, so a secret in the runtime's own
// environment cannot reach a program a flow runs.
func TestCommandEnvPassesOnlyWhatIsDeclared(t *testing.T) {
	t.Setenv("CLI_TEST_ALLOWED", "yes")
	t.Setenv("CLI_TEST_SECRET", "no")

	env := commandEnv([]string{"CLI_TEST_ALLOWED", "CLI_TEST_UNSET"})
	if len(env) != 1 || env[0] != "CLI_TEST_ALLOWED=yes" {
		t.Fatalf("commandEnv = %v, want only the declared, set variable", env)
	}
	// Never nil: exec treats a nil Env as "inherit everything", which is what this
	// setting exists to prevent.
	if commandEnv(nil) == nil {
		t.Error("commandEnv(nil) must be non-nil so the child inherits nothing")
	}
}

// TestCLIRunDynamicProgram is the case the allow list exists for: the program
// comes from the message, which is what lets an ai-agent tool branch hand a model
// a set of commands and let it pick.
func TestCLIRunDynamicProgram(t *testing.T) {
	requireProgram(t, catPath)
	cfg := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": "body.program", "allow": []string{catPath}, "stdin": `"hello\n"`}}
	proc := buildCLI(t, cfg)

	out, err := proc.Process(context.Background(), newMessageBody(t, `{"program":"`+catPath+`"}`))
	if err != nil {
		t.Fatalf("an allowed program should run: %v", err)
	}
	if out.Body.(map[string]any)["stdout"] != "hello\n" {
		t.Errorf("stdout = %v", out.Body.(map[string]any)["stdout"])
	}

	// The enforcement point. A program off the list is refused before anything is
	// spawned, however it got into the message.
	_, err = proc.Process(context.Background(), newMessageBody(t, `{"program":"/bin/echo"}`))
	if err == nil {
		t.Fatal("a program that is not on the allow list must be refused")
	}
	if !strings.Contains(err.Error(), "allow list") {
		t.Errorf("error %q should say why", err)
	}

	// A name that does not resolve to an allowed program is refused however it is
	// spelled.
	for _, sneaky := range []string{"/bin/cat ", "/bin/cat\n", "/usr/bin/env"} {
		if _, err := proc.Process(context.Background(),
			newMessageBody(t, `{"program":`+quote(sneaky)+`}`)); err == nil {
			t.Errorf("%q should not be allowed", sneaky)
		}
	}
}

// TestCLIRunCollectsOutputWhileWatchingIt is the regression for a block that
// reported "lines: 18" alongside an empty stdout.
//
// The events path is an observer: it reports on the run without taking part in
// it. Buffering used to be skipped whenever a handler existed, so watching the
// output quietly consumed it — and an EMPTY events path, which does nothing at
// all, was enough to trigger it. Both shapes are covered here because the empty
// one is a real configuration: an author who wants the block and not the
// streaming.
func TestCLIRunCollectsOutputWhileWatchingIt(t *testing.T) {
	requireProgram(t, catPath)

	cases := map[string]*types.FlowConfig{
		"no events path":       nil,
		"an empty events path": {Process: nil},
		"an events path that does something": {
			Process: []types.BlockConfig{{Type: "record", Settings: types.Settings{"tag": "seen"}}},
		},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := cliConfig()
			cfg.Settings["events"] = events
			body := runCLI(t, buildCLI(t, cfg), "one\ntwo\nthree\n")

			if body["lines"] != 3 {
				t.Errorf("lines = %v, want 3", body["lines"])
			}
			// The claim: a line count and the output it counted always agree.
			if body["stdout"] != "one\ntwo\nthree\n" {
				t.Errorf("stdout = %q, want the command's output regardless of the events path",
					body["stdout"])
			}
		})
	}
}

// TestCLIRunStopsTheCommandWhenNobodyIsListening is the claim the docs make about
// `ifClosed: stop`: a caller that hangs up ends the command rather than leaving
// it to run to completion producing output nobody will read.
//
// Two halves, and both were once missing. The handler failing has to actually
// kill the child — it used to only stop the handler being called, while the pumps
// drained the pipes to EOF — and the resulting error has to read as a stop rather
// than get routed into the flow's error path.
func TestCLIRunStopsTheCommandWhenNobodyIsListening(t *testing.T) {
	const yesPath = "/usr/bin/yes"
	requireProgram(t, yesPath)

	// `yes` never ends on its own, so if the stop does not kill it this test hangs
	// until the timeout below rather than passing.
	cfg := types.BlockConfig{
		Type: blockKindCLIRun, Name: "run",
		Settings: types.Settings{"program": `"` + yesPath + `"`, "timeout": "20s", "events": &types.FlowConfig{Process: []types.BlockConfig{{Type: "cli.stop"}}}}}
	proc := buildCLI(t, cfg)

	done := make(chan *types.Message, 1)
	go func() {
		out, err := proc.Process(context.Background(), newMessageBody(t, `{}`))
		if err != nil {
			t.Errorf("a stop is not a failure: %v", err)
		}
		done <- out
	}()

	select {
	case out := <-done:
		if out == nil || !out.StopRequested() {
			t.Fatalf("the block should have requested the flow stop, got %v", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command was not killed when the events path asked to stop")
	}
}
