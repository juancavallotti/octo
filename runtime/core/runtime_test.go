package core

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/eip-go/types"
)

const (
	e2eWaitTimeout = 2 * time.Second
	e2ePollEvery   = 5 * time.Millisecond
)

// countingSource emits a fixed number of messages then idles until stopped.
type countingSource struct {
	out   chan<- *types.Message
	count int
	done  chan struct{}
}

func (s *countingSource) Start(ctx context.Context) error {
	go func() {
		for i := 0; i < s.count; i++ {
			msg, err := types.NewMessage("")
			if err != nil {
				return
			}
			select {
			case s.out <- msg:
			case <-ctx.Done():
				return
			case <-s.done:
				return
			}
		}
	}()
	return nil
}

func (s *countingSource) Stop(context.Context) error {
	close(s.done)
	return nil
}

// fakeConnector is a Connector that provides a countingSource, standing in for a
// real connector (core cannot import the noop connector without a cycle).
type fakeConnector struct{ count int }

func (c *fakeConnector) Start(context.Context, types.ConnectorConfig) error { return nil }
func (c *fakeConnector) Stop(context.Context) error                         { return nil }

//nolint:ireturn // satisfies the SourceProvider interface
func (c *fakeConnector) NewSource(_ types.SourceConfig, out chan<- *types.Message) (MessageSource, error) {
	return &countingSource{out: out, count: c.count, done: make(chan struct{})}, nil
}

func waitForCompleted(t *testing.T, rec *recorder, want int) {
	t.Helper()
	deadline := time.Now().Add(e2eWaitTimeout)
	for time.Now().Before(deadline) {
		if countKind(rec.kinds(), types.FlowEventCompleted) >= want {
			return
		}
		time.Sleep(e2ePollEvery)
	}
	got := countKind(rec.kinds(), types.FlowEventCompleted)
	t.Fatalf("timed out waiting for %d completed events; got %d", want, got)
}

// TestServiceRunsFlowWithFork exercises the whole path: Service builds the flow
// (creating the shared pool and threading it into the fork), starts the source,
// runs messages through a concurrent fork, and publishes lifecycle events.
func TestServiceRunsFlowWithFork(t *testing.T) {
	const count = 12

	MustRegisterBlock("e2e.pass", func(types.Settings, BlockDeps) (MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})

	reg := NewRegistry()
	reg.MustRegister("fake", func() Connector { return &fakeConnector{count: count} })

	cfg := types.Config{
		Connectors: []types.ConnectorConfig{{Name: "fake-conn", Type: "fake"}},
		Flows: []types.FlowConfig{{
			Name:    "e2e",
			Workers: 4,
			Source:  &types.SourceConfig{Connector: "fake-conn", Type: "any"},
			Process: []types.BlockConfig{{
				Type: "fork",
				Name: "scatter",
				Branches: []types.FlowConfig{
					{Name: "a", Process: []types.BlockConfig{{Type: "e2e.pass"}}},
					{Name: "b", Process: []types.BlockConfig{{Type: "e2e.pass"}}},
				},
			}},
		}},
	}

	rec := &recorder{}
	DefaultEventBus().Subscribe(rec.handle)

	svc := NewService(cfg, reg)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- svc.Run(ctx) }()

	waitForCompleted(t, rec, count)
	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Service.Run: %v", err)
	}

	if got := countKind(rec.kinds(), types.FlowEventStarted); got != count {
		t.Errorf("started events = %d, want %d", got, count)
	}
	if got := countKind(rec.kinds(), types.FlowEventCompleted); got != count {
		t.Errorf("completed events = %d, want %d", got, count)
	}
}
