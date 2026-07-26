package main

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/services"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeHosted is a hosted service that records its lifecycle calls into a shared
// log, so a test can assert the order across several of them.
type fakeHosted struct {
	name     string
	usage    string
	startErr error
	log      *[]string
}

func (f *fakeHosted) Name() string           { return f.name }
func (f *fakeHosted) Flags(fs *flag.FlagSet) { fs.String(f.name+"-flag", "", "a flag") }
func (f *fakeHosted) Usage() string          { return f.usage }

func (f *fakeHosted) Start(context.Context, *services.Health) error {
	*f.log = append(*f.log, "start:"+f.name)
	return f.startErr
}

func (f *fakeHosted) Stop(context.Context) error {
	*f.log = append(*f.log, "stop:"+f.name)
	return nil
}

// configAware also observes each config generation.
type configAware struct {
	fakeHosted
	seen []string
}

func (c *configAware) Configured(config types.Config) {
	c.seen = append(c.seen, config.Service.Name)
}

// A hosted service's flags have no YAML to document them, so its section has to
// reach the help page — and the "one or two dashes" note is about every flag on
// the page, so it must stay last.
func TestUsageTextIncludesHostedSections(t *testing.T) {
	services.RegisterHosted(&fakeHosted{name: "fake-usage", usage: "Fake flags:\n  --fake-usage-flag", log: new([]string)})

	got := usageText()
	if !strings.Contains(got, "Fake flags:\n  --fake-usage-flag") {
		t.Error("usageText() is missing the hosted service's section")
	}
	if !strings.HasSuffix(got, dashNote) {
		t.Errorf("usageText() does not end with the dash note; got tail %q", got[max(0, len(got)-80):])
	}
	if !strings.Contains(got, "octo — run and invoke octo integration flows") {
		t.Error("usageText() is missing the static help page")
	}
}

func TestStartHostedStartsInOrderAndStopsInReverse(t *testing.T) {
	var log []string
	hosted := []services.HostedService{
		&fakeHosted{name: "first", log: &log},
		&fakeHosted{name: "second", log: &log},
	}

	stop, err := startHosted(context.Background(), hosted, services.NewHealth())
	if err != nil {
		t.Fatalf("startHosted: %v", err)
	}
	stop()

	want := []string{"start:first", "start:second", "stop:second", "stop:first"}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle = %v, want %v", log, want)
	}
}

// A service that cannot start fails the run, and the ones already up are stopped
// rather than left running behind a returning error.
func TestStartHostedUnwindsOnFailure(t *testing.T) {
	var log []string
	wantErr := errors.New("bind: address already in use")
	hosted := []services.HostedService{
		&fakeHosted{name: "first", log: &log},
		&fakeHosted{name: "second", startErr: wantErr, log: &log},
		&fakeHosted{name: "third", log: &log},
	}

	stop, err := startHosted(context.Background(), hosted, services.NewHealth())
	if !errors.Is(err, wantErr) {
		t.Fatalf("startHosted error = %v, want %v", err, wantErr)
	}
	if stop != nil {
		t.Error("startHosted returned a stop func alongside an error")
	}
	if !strings.Contains(err.Error(), "second") {
		t.Errorf("error %q does not name the service that failed", err)
	}

	// third never started; first was started and must have been stopped.
	want := []string{"start:first", "start:second", "stop:first"}
	if strings.Join(log, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle = %v, want %v", log, want)
	}
}

// The stop context outlives cancellation of the run, so a graceful drain has
// somewhere to happen.
func TestStartHostedStopUsesLiveContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var stopCtx context.Context
	hosted := []services.HostedService{&contextCapturingHosted{onStop: func(c context.Context) { stopCtx = c }}}

	stop, err := startHosted(ctx, hosted, services.NewHealth())
	if err != nil {
		t.Fatalf("startHosted: %v", err)
	}
	cancel()
	stop()

	if stopCtx == nil {
		t.Fatal("Stop was not called")
	}
	if stopCtx.Err() != nil {
		t.Fatalf("Stop context is already done (%v); a drain has nowhere to happen", stopCtx.Err())
	}
}

type contextCapturingHosted struct {
	onStop func(context.Context)
}

func (c *contextCapturingHosted) Name() string                                  { return "capturing" }
func (c *contextCapturingHosted) Flags(*flag.FlagSet)                           {}
func (c *contextCapturingHosted) Usage() string                                 { return "" }
func (c *contextCapturingHosted) Start(context.Context, *services.Health) error { return nil }
func (c *contextCapturingHosted) Stop(ctx context.Context) error                { c.onStop(ctx); return nil }

func TestAnnounceConfigReachesOnlyConfigAware(t *testing.T) {
	var log []string
	plain := &fakeHosted{name: "plain", log: &log}
	aware := &configAware{fakeHosted: fakeHosted{name: "aware", log: &log}}

	hosted := []services.HostedService{plain, aware}
	announceConfig(hosted, types.Config{Service: types.ServiceConfig{Name: "gen-1"}})
	announceConfig(hosted, types.Config{Service: types.ServiceConfig{Name: "gen-2"}})

	if len(aware.seen) != 2 || aware.seen[0] != "gen-1" || aware.seen[1] != "gen-2" {
		t.Fatalf("ConfigAware saw %v, want [gen-1 gen-2] — one call per generation", aware.seen)
	}
}
