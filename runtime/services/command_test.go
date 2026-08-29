package services

import (
	"strings"
	"testing"
)

// testCommand is a command with whatever name and help a test needs.
type testCommand struct {
	name  string
	usage string
	ran   *[]string
}

func (c testCommand) Name() string  { return c.name }
func (c testCommand) Usage() string { return c.usage }

func (c testCommand) Run(args []string) error {
	if c.ran != nil {
		*c.ran = args
	}
	return nil
}

// register cleans up after itself, so tests do not leak into one another through
// the package-level registry.
func register(t *testing.T, cmd Command) {
	t.Helper()
	RegisterCommand(cmd)
	t.Cleanup(func() {
		commandMu.Lock()
		defer commandMu.Unlock()
		delete(commands, cmd.Name())
	})
}

func TestLookupCommandRunsIt(t *testing.T) {
	var got []string
	register(t, testCommand{name: "probe", ran: &got})

	cmd, ok := LookupCommand("probe")
	if !ok {
		t.Fatal("LookupCommand did not find a registered command")
	}
	if err := cmd.Run([]string{"--json", "http://x"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 2 || got[0] != "--json" {
		t.Fatalf("Run received %v, want the arguments after the command name", got)
	}
}

func TestLookupUnregisteredCommand(t *testing.T) {
	if _, ok := LookupCommand("nothing-registered-this"); ok {
		t.Fatal("LookupCommand found a command nobody registered")
	}
}

// Two commands answering to one word is a build mistake, and the failure belongs
// at startup rather than at whichever one the map happened to keep.
func TestDuplicateCommandPanics(t *testing.T) {
	register(t, testCommand{name: "clash"})
	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate name did not panic")
		}
	}()
	RegisterCommand(testCommand{name: "clash"})
}

func TestRegisterCommandRejectsNonsense(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  Command
	}{
		{"nil command", nil},
		{"empty name", testCommand{name: ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("RegisterCommand accepted it")
				}
			}()
			RegisterCommand(tc.cmd)
		})
	}
}

// The names are what the CLI's unknown-command error lists, so they have to be
// stable rather than in map order.
func TestCommandNamesAreSorted(t *testing.T) {
	register(t, testCommand{name: "zebra"})
	register(t, testCommand{name: "aardvark"})

	names := CommandNames()
	var seen []string
	for _, name := range names {
		if name == "zebra" || name == "aardvark" {
			seen = append(seen, name)
		}
	}
	if len(seen) != 2 || seen[0] != "aardvark" {
		t.Fatalf("CommandNames = %v, want them sorted", names)
	}
}

// A command that documents nothing is undiscoverable, but an empty section must
// not leave a blank hole in the help page either.
func TestCommandUsageSkipsTheUndocumented(t *testing.T) {
	register(t, testCommand{name: "documented", usage: "Documented flags:\n  --x"})
	register(t, testCommand{name: "silent", usage: ""})

	usage := CommandUsage()
	if !strings.Contains(usage, "Documented flags:") {
		t.Fatalf("CommandUsage lost a documented command:\n%s", usage)
	}
	if strings.Contains(usage, "\n\n\n") {
		t.Fatalf("CommandUsage left a hole where a silent command was:\n%s", usage)
	}
}

// Unlike a provider, a command is not module-selected: it is available whenever
// its package is compiled in. verify-platform-api is the reason — you run it to
// find out whether a server is ready, before you would ever select the module.
func TestCommandsAreNotModuleSelected(t *testing.T) {
	t.Setenv(ModuleEnvVar, "something-else-entirely")
	register(t, testCommand{name: "unselected"})

	if _, ok := LookupCommand("unselected"); !ok {
		t.Fatal("a command was hidden because another module is selected")
	}
}
