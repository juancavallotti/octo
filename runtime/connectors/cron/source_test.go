package cron

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeServices is a RuntimeServices whose leadership is fixed, to drive the cron
// source's leader gating in tests.
type fakeServices struct{ leader bool }

//nolint:ireturn // implements core.RuntimeServices, which returns interfaces
func (f fakeServices) LeaderElection() core.LeaderElection { return fakeLeaderElection(f) }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Leases() core.Leases { return core.NoopLeases() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (fakeServices) KV() core.KV { return nil }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Secrets() core.SecretStore { return nil }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Traces() core.TracePublisher { return core.NoopTracer() }
func (fakeServices) Close() error                { return nil }

type fakeLeaderElection struct{ leader bool }

//nolint:ireturn // implements core.LeaderElection
func (f fakeLeaderElection) Acquire(context.Context, string) (core.Leadership, error) {
	return fakeLeadership(f), nil
}

type fakeLeadership struct{ leader bool }

func (f fakeLeadership) IsLeader() bool { return f.leader }
func (fakeLeadership) Close() error     { return nil }

// newCronSource builds a source for leader tests, returning the concrete type so a
// test can drive emit directly.
func newCronSource(t *testing.T, out chan<- *types.Message) *source {
	t.Helper()
	src, err := (&Connector{}).NewSource(types.SourceConfig{
		Connector: "daily-report",
		Settings:  map[string]any{"schedule": "@every 1h", "payload": `{"kind":"tick"}`},
	}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	return src.(*source)
}

func TestCronEmitsWhenLeader(t *testing.T) {
	out := make(chan *types.Message, 1)
	s := newCronSource(t, out)
	ctx := core.ContextWithRuntimeServices(context.Background(), fakeServices{leader: true})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	s.emit()
	select {
	case <-out:
	default:
		t.Fatal("leader should have emitted a tick")
	}
}

func TestCronSkipsWhenNotLeader(t *testing.T) {
	out := make(chan *types.Message, 1)
	s := newCronSource(t, out)
	ctx := core.ContextWithRuntimeServices(context.Background(), fakeServices{leader: false})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	s.emit()
	select {
	case <-out:
		t.Fatal("non-leader should not emit a tick")
	default:
	}
}

func TestLeaderKeyFromConnectorName(t *testing.T) {
	// The key is the connector kind prefix plus the connector instance name.
	if got := leaderKey(types.SourceConfig{Connector: "daily-report", Type: "cron"}); got != "cron_daily-report" {
		t.Fatalf("key = %q, want \"cron_daily-report\"", got)
	}
	// An implicitly-resolved connector (no name) falls back to the type.
	if got := leaderKey(types.SourceConfig{Type: "cron"}); got != "cron_cron" {
		t.Fatalf("fallback key = %q, want \"cron_cron\"", got)
	}
}

func TestCronSourceEmitsPayload(t *testing.T) {
	out := make(chan *types.Message, 1)
	src, err := (&Connector{}).NewSource(types.SourceConfig{
		Type: "cron",
		Settings: map[string]any{
			"schedule": "@every 1s",
			"payload":  `{"kind": "tick"}`,
		},
	}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	select {
	case msg := <-out:
		body, ok := msg.Body.(map[string]any)
		if !ok {
			t.Fatalf("body type = %T, want map", msg.Body)
		}
		if body["kind"] != "tick" {
			t.Errorf("body kind = %v, want tick", body["kind"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a cron tick")
	}
}

// stubLoader serves one template resource by id, standing in for the runtime's
// resource loader so a payload can render templateResource(id).
type stubLoader struct{ text string }

func (s stubLoader) Load(context.Context, core.ResourceKind, string) ([]byte, error) {
	return []byte(s.text), nil
}

// A payload reaches the registered CEL extensions, templateResource among them,
// rendered against the source's own scope (now, settings).
func TestCronSourcePayloadRendersTemplateResource(t *testing.T) {
	out := make(chan *types.Message, 1)
	src, err := (&Connector{}).NewSource(types.SourceConfig{
		Type: "cron",
		Settings: map[string]any{
			"schedule": "@every 1s",
			"payload":  `{"test": templateResource("in")}`,
		},
	}, out, core.SourceDeps{Resources: stubLoader{text: "runs {{ settings.schedule }}"}})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := src.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	select {
	case msg := <-out:
		body, ok := msg.Body.(map[string]any)
		if !ok {
			t.Fatalf("body type = %T, want map", msg.Body)
		}
		if body["test"] != "runs @every 1s" {
			t.Errorf("body test = %v, want %q", body["test"], "runs @every 1s")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a cron tick")
	}
}

func TestCronSourceRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
	}{
		{name: "missing schedule", settings: map[string]any{}},
		{name: "bad schedule", settings: map[string]any{"schedule": "not a cron"}},
		{name: "bad payload", settings: map[string]any{"schedule": "@every 1s", "payload": "{"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := make(chan *types.Message)
			if _, err := (&Connector{}).NewSource(types.SourceConfig{Settings: tt.settings}, out, core.SourceDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

// cronTraceCollector keeps what the source published.
type cronTraceCollector struct {
	mu      sync.Mutex
	records []types.TraceEvent
}

func (c *cronTraceCollector) Enabled() bool { return true }

func (c *cronTraceCollector) Publish(event types.TraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, event)
}

func (c *cronTraceCollector) all() []types.TraceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]types.TraceEvent(nil), c.records...)
}

func tracingCron(t *testing.T) *cronTraceCollector {
	t.Helper()
	c := &cronTraceCollector{}
	previous := core.Tracer()
	core.SetTracer(c)
	t.Cleanup(func() { core.SetTracer(previous) })
	return c
}

// A cron tick has no caller to answer, so what its record adds over the flow's own
// events is the schedule — the reason the message exists at all.
func TestCronTraceNamesTheSchedule(t *testing.T) {
	c := tracingCron(t)
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}

	s := &source{schedule: "@every 2s"}
	s.trace(msg)

	records := c.all()
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	if records[0].Kind != types.TraceSourceEmit {
		t.Errorf("Kind = %s, want %s", records[0].Kind, types.TraceSourceEmit)
	}
	if got := records[0].Attrs["schedule"]; got != "@every 2s" {
		t.Errorf("schedule = %v, want the expression as authored", got)
	}
	if msg.TraceID() == "" || records[0].TraceID != msg.TraceID() {
		t.Errorf("the record and the message disagree on the trace id")
	}
}

func TestCronTraceIsInertWhenTracingIsOff(t *testing.T) {
	c := tracingCron(t)
	core.SetTracer(nil)

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	(&source{schedule: "@every 2s"}).trace(msg)

	if got := len(c.all()); got != 0 {
		t.Errorf("records = %d with tracing off, want 0", got)
	}
	if msg.TraceID() != "" {
		t.Errorf("an untraced tick minted a trace id: %q", msg.TraceID())
	}
}
