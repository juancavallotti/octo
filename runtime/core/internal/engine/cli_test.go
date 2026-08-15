package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
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
		Program: `"` + catPath + `"`,
		Stdin:   "body.text",
	}
}

// buildCLI builds a cli-run block, failing the test if it does not build.
//
//nolint:ireturn // a built block holds the MessageProcessor interface
func buildCLI(t *testing.T, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	block, err := newCLIBuilder().block(cfg)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return block.Processor
}

func newCLIBuilder() *builder {
	return &builder{reg: agentRegistry(&[]any{}), pool: pool.New(0, 0), deps: depsRes(mapResources{})}
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
		{"no program", func(c *types.BlockConfig) { c.Program = "" }, "requires a program"},
		{
			// The case the allow list exists for: a program from the message, with
			// nothing saying what it may be.
			"a dynamic program with no allow list",
			func(c *types.BlockConfig) { c.Program = "body.program" },
			"requires an allow list",
		},
		{
			"a bare name",
			func(c *types.BlockConfig) { c.Program = `"cat"` },
			"must be an absolute path",
		},
		{
			"a missing path",
			func(c *types.BlockConfig) { c.Program = `"/nope/nothing"` },
			"no such file",
		},
		{
			"a directory",
			func(c *types.BlockConfig) { c.Program = `"` + dir + `"` },
			"not a regular file",
		},
		{
			"a non-executable file",
			func(c *types.BlockConfig) { c.Program = `"` + notExecutable + `"` },
			"not executable",
		},
		{
			"a bad entry on the allow list",
			func(c *types.BlockConfig) { c.Program, c.Allow = "body.program", []string{"cat"} },
			"must be an absolute path",
		},
		{"a bad timeout", func(c *types.BlockConfig) { c.Timeout = "soon" }, "timeout"},
		{"a negative timeout", func(c *types.BlockConfig) { c.Timeout = "-1s" }, "must be positive"},
		{"an unknown onExit", func(c *types.BlockConfig) { c.OnExit = "shrug" }, "onExit"},
		{"emit with no events path", func(c *types.BlockConfig) { c.Emit = []string{"stdout"} }, "requires an events path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cliConfig()
			tc.edit(&cfg)
			_, err := newCLIBuilder().block(cfg)
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

	cfg := cliConfig()
	cfg.Program = `"` + shell + `"`
	_, err := newCLIBuilder().block(cfg)
	if err == nil {
		t.Fatal("an interpreter should be refused by default")
	}
	if !strings.Contains(err.Error(), "allowInterpreters") {
		t.Errorf("error %q should name the setting that permits it", err)
	}

	cfg.AllowInterpreters = true
	if _, err := newCLIBuilder().block(cfg); err != nil {
		t.Errorf("allowInterpreters: true should permit a shell: %v", err)
	}
}

func TestCLIRunArgsMustBeStrings(t *testing.T) {
	requireProgram(t, catPath)
	cfg := cliConfig()
	cfg.Args = "[1, 2]"
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
		Type: blockKindCLIRun, Name: "run", Program: `"` + falsePath + `"`,
	}
	if _, err := buildCLI(t, failing).Process(context.Background(), newMessageBody(t, `{}`)); err == nil {
		t.Error("onExit defaults to fail, so a non-zero exit should error the block")
	}

	failing.OnExit = onExitContinue
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
		Type: blockKindCLIRun, Name: "run", Program: `"` + sleepPath + `"`,
		Args: `["30"]`, Timeout: "100ms", OnExit: onExitContinue,
	}
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
		Program: "body.program",
		Allow:   []string{catPath},
		Stdin:   `"hello\n"`,
	}
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

	// Exact match only: no prefix matching, no path normalization into the list.
	for _, sneaky := range []string{"/bin/cat ", "/bin/../bin/cat", "/bin/cat\n"} {
		if _, err := proc.Process(context.Background(),
			newMessageBody(t, `{"program":`+quote(sneaky)+`}`)); err == nil {
			t.Errorf("%q should not match the allow list", sneaky)
		}
	}
}

// TestCLIRunRejectsAConstantProgramTheAllowListOmits: a program that never
// varies and is not on the list can never run. Left to the runtime check it
// would build cleanly and then fail on every single message.
func TestCLIRunRejectsAConstantProgramTheAllowListOmits(t *testing.T) {
	requireProgram(t, catPath)
	cfg := cliConfig()
	cfg.Allow = []string{"/usr/bin/false"}
	_, err := newCLIBuilder().block(cfg)
	if err == nil {
		t.Fatal("a constant program outside the allow list should fail the build")
	}
	if !strings.Contains(err.Error(), "could never run") {
		t.Errorf("error %q should say why", err)
	}
}
