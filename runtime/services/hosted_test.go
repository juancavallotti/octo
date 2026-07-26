package services

import (
	"context"
	"errors"
	"flag"
	"io"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// withCleanHostedRegistry empties the process-wide hosted registry for the test
// and restores it afterwards, so tests do not see each other's registrations.
func withCleanHostedRegistry(t *testing.T) {
	t.Helper()
	hostedMu.Lock()
	saved := hosted
	hosted = nil
	hostedMu.Unlock()
	t.Cleanup(func() {
		hostedMu.Lock()
		hosted = saved
		hostedMu.Unlock()
	})
}

// fakeHosted records the lifecycle calls it receives, in order.
type fakeHosted struct {
	name     string
	usage    string
	startErr error
	calls    []string
	flagVal  string
}

func (f *fakeHosted) Name() string { return f.name }

func (f *fakeHosted) Flags(fs *flag.FlagSet) {
	f.calls = append(f.calls, "Flags")
	fs.StringVar(&f.flagVal, f.name+"-flag", "default", "a flag")
}

func (f *fakeHosted) Usage() string { return f.usage }

func (f *fakeHosted) Start(context.Context, *Health) error {
	f.calls = append(f.calls, "Start")
	return f.startErr
}

func (f *fakeHosted) Stop(context.Context) error {
	f.calls = append(f.calls, "Stop")
	return nil
}

// configAwareHosted also observes each config generation.
type configAwareHosted struct {
	fakeHosted
	generations []string
}

func (c *configAwareHosted) Configured(config types.Config) {
	c.calls = append(c.calls, "Configured")
	c.generations = append(c.generations, config.Service.Name)
}

func TestRegisterHostedPreservesOrder(t *testing.T) {
	withCleanHostedRegistry(t)

	first := &fakeHosted{name: "first"}
	second := &fakeHosted{name: "second"}
	RegisterHosted(first)
	RegisterHosted(second)

	got := Hosted()
	if len(got) != 2 {
		t.Fatalf("Hosted() returned %d services, want 2", len(got))
	}
	if got[0].Name() != "first" || got[1].Name() != "second" {
		t.Fatalf("Hosted() = [%s %s], want [first second]", got[0].Name(), got[1].Name())
	}
}

// Unlike Register, RegisterHosted is never a no-op: hosted services are not
// module-selected, so the selected module must not affect them.
func TestRegisterHostedIgnoresSelectedModule(t *testing.T) {
	withCleanHostedRegistry(t)

	t.Setenv(ModuleEnvVar, "some-other-module")
	RegisterHosted(&fakeHosted{name: "always"})

	if got := Hosted(); len(got) != 1 {
		t.Fatalf("Hosted() returned %d services, want 1 regardless of the selected module", len(got))
	}
}

func TestRegisterHostedRejectsNil(t *testing.T) {
	withCleanHostedRegistry(t)

	defer func() {
		if recover() == nil {
			t.Fatal("RegisterHosted(nil) did not panic")
		}
	}()
	RegisterHosted(nil)
}

// The returned slice is a copy: a caller holding it across the run must not be
// able to mutate the registry through it.
func TestHostedReturnsCopy(t *testing.T) {
	withCleanHostedRegistry(t)

	RegisterHosted(&fakeHosted{name: "real"})
	got := Hosted()
	got[0] = &fakeHosted{name: "swapped"}

	if again := Hosted(); again[0].Name() != "real" {
		t.Fatalf("Hosted()[0] = %q after mutating a previous result, want %q", again[0].Name(), "real")
	}
}

func TestHostedUsage(t *testing.T) {
	tests := []struct {
		name     string
		services []*fakeHosted
		want     string
	}{
		{
			name: "no services",
			want: "",
		},
		{
			name:     "one section",
			services: []*fakeHosted{{name: "a", usage: "A flags:\n  --a"}},
			want:     "A flags:\n  --a",
		},
		{
			name: "sections are blank-line separated",
			services: []*fakeHosted{
				{name: "a", usage: "A flags:\n  --a\n"},
				{name: "b", usage: "B flags:\n  --b"},
			},
			want: "A flags:\n  --a\n\nB flags:\n  --b",
		},
		{
			name: "a service documenting nothing is skipped",
			services: []*fakeHosted{
				{name: "a", usage: ""},
				{name: "b", usage: "B flags:\n  --b"},
			},
			want: "B flags:\n  --b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCleanHostedRegistry(t)
			for _, service := range tt.services {
				RegisterHosted(service)
			}
			if got := HostedUsage(); got != tt.want {
				t.Fatalf("HostedUsage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The CLI's contract: flags are registered before parse, Start runs after it, and
// a ConfigAware service sees each generation. This exercises the order a hosted
// service may rely on.
func TestHostedLifecycleOrder(t *testing.T) {
	withCleanHostedRegistry(t)

	service := &configAwareHosted{fakeHosted: fakeHosted{name: "obs"}}
	RegisterHosted(service)

	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	for _, h := range Hosted() {
		h.Flags(fs)
	}
	if err := fs.Parse([]string{"--obs-flag", "passed"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	health := NewHealth()
	for _, h := range Hosted() {
		if err := h.Start(context.Background(), health); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	for _, h := range Hosted() {
		if aware, ok := h.(ConfigAware); ok {
			aware.Configured(types.Config{Service: types.ServiceConfig{Name: "gen-1"}})
			aware.Configured(types.Config{Service: types.ServiceConfig{Name: "gen-2"}})
		}
	}
	for _, h := range Hosted() {
		if err := h.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}

	wantCalls := []string{"Flags", "Start", "Configured", "Configured", "Stop"}
	if len(service.calls) != len(wantCalls) {
		t.Fatalf("calls = %v, want %v", service.calls, wantCalls)
	}
	for i, want := range wantCalls {
		if service.calls[i] != want {
			t.Fatalf("calls = %v, want %v", service.calls, wantCalls)
		}
	}
	if service.flagVal != "passed" {
		t.Fatalf("flag value = %q, want %q — Flags must run before Parse", service.flagVal, "passed")
	}
	if len(service.generations) != 2 || service.generations[0] != "gen-1" || service.generations[1] != "gen-2" {
		t.Fatalf("generations = %v, want [gen-1 gen-2]", service.generations)
	}
}

// A hosted service that cannot start fails the run: binding a port is a startup
// error, not a log line.
func TestHostedStartErrorSurfaces(t *testing.T) {
	withCleanHostedRegistry(t)

	wantErr := errors.New("bind: address already in use")
	RegisterHosted(&fakeHosted{name: "obs", startErr: wantErr})

	for _, h := range Hosted() {
		if err := h.Start(context.Background(), NewHealth()); !errors.Is(err, wantErr) {
			t.Fatalf("Start() error = %v, want %v", err, wantErr)
		}
	}
}
