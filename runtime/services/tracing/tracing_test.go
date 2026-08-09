package tracing

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/services"
)

// parseFlags builds a service and resolves args through its flagset, the way
// `octo run` does.
func parseFlags(t *testing.T, args ...string) *Service {
	t.Helper()
	s := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	s.Flags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return s
}

// captureLogs redirects the default logger for one test and returns everything
// written to it.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var sb strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&sb, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return sb.String
}

func TestTracingIsOffByDefault(t *testing.T) {
	s := parseFlags(t)
	if s.Options().Enabled {
		t.Fatal("tracing is on without --traces")
	}
}

// Each flag's default is its environment variable, which is the only way to turn
// tracing on in a deployed pod: it passes no arguments, so the image's CMD stands
// and the environment is the whole configuration surface.
func TestFlagDefaultsComeFromTheEnvironment(t *testing.T) {
	t.Setenv(envEnabled, "true")
	t.Setenv(envFile, "/tmp/from-env.jsonl")
	t.Setenv(envBlocks, "orders.charge")
	t.Setenv(envBodies, "off")
	t.Setenv(envVars, "no")
	t.Setenv(envMaxPayload, "1024")
	t.Setenv(envBuffer, "64")

	opts := parseFlags(t).Options()
	if !opts.Enabled {
		t.Fatalf("%s=true did not enable tracing", envEnabled)
	}
	if opts.File != "/tmp/from-env.jsonl" {
		t.Errorf("File = %q, want the environment's", opts.File)
	}
	if opts.Bodies || opts.Vars {
		t.Errorf("Bodies/Vars = %v/%v, want both off", opts.Bodies, opts.Vars)
	}
	if opts.MaxPayload != 1024 {
		t.Errorf("MaxPayload = %d, want 1024", opts.MaxPayload)
	}
	if opts.Buffer != 64 {
		t.Errorf("Buffer = %d, want 64", opts.Buffer)
	}
}

// A flag passed on the command line beats the environment, which falls out of
// making the environment the flag's default rather than writing precedence code.
func TestFlagBeatsEnvironment(t *testing.T) {
	t.Setenv(envEnabled, "false")

	if !parseFlags(t, "--traces").Options().Enabled {
		t.Fatal("--traces did not override OCTO_TRACING=false")
	}
}

// The warning is part of the feature, not a formality: a traced runtime looks
// healthy from the outside, just slower, so the run has to say what it is doing.
func TestEnablingTracingWarnsLoudly(t *testing.T) {
	logs := captureLogs(t)
	s := parseFlags(t, "--traces")
	if err := s.Start(context.Background(), services.NewHealth()); err != nil {
		t.Fatalf("start: %v", err)
	}

	out := logs()
	for _, want := range []string{"development feature", "not recommended", "every block"} {
		if !strings.Contains(out, want) {
			t.Errorf("the warning does not mention %q:\n%s", want, out)
		}
	}
	// Payloads are on by default, so the run must also say that it is writing
	// message contents somewhere.
	if !strings.Contains(out, "payloads") {
		t.Errorf("no payload warning with capture on:\n%s", out)
	}
}

func TestDisabledTracingIsSilentAndInert(t *testing.T) {
	logs := captureLogs(t)
	s := parseFlags(t)
	if err := s.Start(context.Background(), services.NewHealth()); err != nil {
		t.Fatalf("start: %v", err)
	}

	if out := logs(); strings.Contains(out, "tracing") {
		t.Errorf("a disabled service logged about tracing:\n%s", out)
	}
	if core.Tracer().Enabled() {
		t.Fatal("starting a disabled service enabled the process tracer")
	}
}

// Start installs no publisher — the runtime-services module builds that, later in
// startup — so the process tracer is untouched here. What the service registers
// is listeners, and they are guarded on the tracer being enabled rather than on
// having been registered.
func TestStartDoesNotInstallAPublisher(t *testing.T) {
	previous := core.Tracer()
	core.SetTracer(nil)
	t.Cleanup(func() { core.SetTracer(previous) })

	s := parseFlags(t, "--traces")
	if err := s.Start(context.Background(), services.NewHealth()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if core.Tracer().Enabled() {
		t.Fatal("Start installed a publisher; the module owns that")
	}
}

// Neither event stream has an unsubscribe, so a second Start must not register a
// second listener. It would not fail or warn — it would quietly write every
// record twice, and a trace that says a block ran twice when it ran once is
// worse than one that is merely missing.
//
// The assertion is on the delta rather than the total: this package's other
// tests have already registered listeners on the same process-wide dispatcher,
// and there is no way to take them back off.
func TestStartingTwiceRegistersOneListener(t *testing.T) {
	c := collecting(t)

	msg := traceMessage(t, map[string]any{"amount": float64(1)})
	event := blockEvent(msg)

	core.DefaultBlockEvents().Emit(context.Background(), event)
	baseline := c.count()

	s := parseFlags(t, "--traces")
	for range 2 {
		if err := s.Start(context.Background(), services.NewHealth()); err != nil {
			t.Fatalf("start: %v", err)
		}
	}

	// The second emit re-runs the baseline listeners as well, so what this
	// service added is the total less two baselines.
	core.DefaultBlockEvents().Emit(context.Background(), event)
	if added := c.count() - baseline*2; added != 1 {
		t.Fatalf("two Starts produced %d records per block event, want 1", added)
	}
}

// The service registers itself, so a binary importing this package gets it
// without any wiring of its own.
func TestServiceRegistersItself(t *testing.T) {
	for _, registered := range services.Hosted() {
		if registered.Name() == serviceName {
			return
		}
	}
	t.Fatalf("no hosted service named %q is registered", serviceName)
}

// Usage is appended to `octo --help`, so the warning has to be visible before
// someone turns tracing on, not only after.
func TestUsageWarnsBeforeTheFlagIsUsed(t *testing.T) {
	usage := New().Usage()
	if !strings.Contains(usage, "--traces") {
		t.Error("usage does not document --traces")
	}
	if !strings.Contains(usage, "DEVELOPMENT FEATURE") {
		t.Error("usage does not warn that tracing is a development feature")
	}
	if !strings.Contains(usage, envEnabled) {
		t.Errorf("usage does not mention %s, the only way to enable tracing in a pod", envEnabled)
	}
}

func TestParseAddresses(t *testing.T) {
	got := parseAddresses(" orders.charge , orders.fanout[audit].log-it ,, ")
	want := []string{"orders.charge", "orders.fanout[audit].log-it"}
	if len(got) != len(want) {
		t.Fatalf("addresses = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("addresses = %v, want %v", got, want)
		}
	}
	if addresses := parseAddresses(" , "); len(addresses) != 0 {
		t.Fatalf("a list of blanks parsed to %v, want nothing", addresses)
	}
}
