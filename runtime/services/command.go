package services

import (
	"sort"
	"strings"
	"sync"
)

// Command is the third thing a runtime service may contribute: a subcommand on
// the CLI.
//
// It exists for the same reason HostedService.Flags does. A service that is not
// declared in any config still has things to say to the person running the
// binary, and the only way it can say them is through the CLI — so a module
// brings its flags, its help, and (here) its commands with it, rather than
// package main growing a build-tagged case for each.
//
// Unlike a provider, a command is NOT module-selected: it is available whenever
// its package is compiled in, whatever RUNTIME_SERVICES_MODULE says. That is
// deliberate. `octo verify-platform-api` is a thing you run to find out whether a
// server is ready, before you would ever set the variable that selects the
// module — requiring the selection first would make it useless exactly when it is
// needed.
//
// What it buys is that a capability appears in a binary if and only if that
// binary can act on it. The platform API contract is the worked example: printing
// it from a build with no api provider would invite somebody to implement an
// interface that binary cannot talk to, and they would find out only after
// writing a server.
type Command interface {
	// Name is the subcommand as typed, e.g. "openapi".
	Name() string
	// Usage returns the help section appended to `octo --help`, or "" for a
	// command not worth documenting. A command that documents nothing is
	// undiscoverable, so this is almost never empty.
	Usage() string
	// Run executes the command with the arguments following its name. A non-nil
	// error fails the process; flag.ErrHelp is the caller's to handle.
	Run(args []string) error
}

var (
	commandMu sync.Mutex
	commands  = map[string]Command{}
)

// RegisterCommand adds a subcommand. Like RegisterHosted and unlike Register it
// is never a no-op — a binary chooses which commands it ships by which packages
// it blank-imports.
//
// Registering a name twice panics rather than picking a winner: two commands
// answering to one word is a build mistake, and the failure should happen at
// startup rather than at whichever one the map happened to keep.
func RegisterCommand(cmd Command) {
	if cmd == nil {
		panic("services: nil command")
	}
	name := cmd.Name()
	if name == "" {
		panic("services: command with no name")
	}
	commandMu.Lock()
	defer commandMu.Unlock()
	if _, taken := commands[name]; taken {
		panic("services: duplicate command " + name)
	}
	commands[name] = cmd
}

// LookupCommand returns the command registered under name.
//
//nolint:ireturn // returns the Command interface the CLI dispatches through
func LookupCommand(name string) (Command, bool) {
	commandMu.Lock()
	defer commandMu.Unlock()
	cmd, ok := commands[name]
	return cmd, ok
}

// CommandNames returns every registered command name, sorted, for an error
// message that tells the reader what this binary actually offers.
func CommandNames() []string {
	commandMu.Lock()
	defer commandMu.Unlock()
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CommandUsage returns every registered command's help section, in name order so
// the page is stable, for appending to the CLI's usage page.
func CommandUsage() string {
	var b strings.Builder
	for _, name := range CommandNames() {
		cmd, ok := LookupCommand(name)
		if !ok {
			continue
		}
		usage := strings.TrimRight(cmd.Usage(), "\n")
		if usage == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(usage)
	}
	return b.String()
}
