package cron

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// fakeServices is a RuntimeServices whose leadership is fixed, to drive the cron
// source's leader gating in tests.
type fakeServices struct{ leader bool }

//nolint:ireturn // implements core.RuntimeServices, which returns interfaces
func (f fakeServices) LeaderElection() core.LeaderElection { return fakeLeaderElection(f) }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) KV() core.KV { return nil }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Secrets() core.SecretStore { return nil }
func (fakeServices) Close() error              { return nil }

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
	}, out)
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
	}, out)
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
			if _, err := (&Connector{}).NewSource(types.SourceConfig{Settings: tt.settings}, out); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
