package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// newFakeSet builds a connectorSet over a registry with "fake" registered and the
// given already-started instances keyed by name, mirroring what startConnectors
// produces for explicitly configured connectors.
func newFakeSet(configs []types.ConnectorConfig, started map[string]*fakeConnector) *connectorSet {
	reg := core.NewRegistry()
	reg.MustRegister("fake", func() core.Connector { return &fakeConnector{} })

	byName := make(map[string]core.Connector, len(started))
	running := make([]core.Connector, 0, len(started))
	for name, c := range started {
		byName[name] = c
		running = append(running, c)
	}
	return &connectorSet{registry: reg, configs: configs, running: running, byName: byName}
}

func TestResolveConnectorExplicitWins(t *testing.T) {
	want := &fakeConnector{}
	set := newFakeSet(
		[]types.ConnectorConfig{{Name: "ticker", Type: "fake"}},
		map[string]*fakeConnector{"ticker": want},
	)

	got, err := set.resolveConnector(context.Background(), types.SourceConfig{Connector: "ticker", Type: "fake"})
	if err != nil {
		t.Fatalf("resolveConnector: %v", err)
	}
	if got != want {
		t.Errorf("resolveConnector returned a different connector than the configured instance")
	}
}

func TestResolveConnectorLoneConfiguredOfType(t *testing.T) {
	want := &fakeConnector{}
	// A lone connector of the type, renamed away from the type name, binds even
	// when the source names it only by type (the editor's fallback).
	set := newFakeSet(
		[]types.ConnectorConfig{{Name: "renamed", Type: "fake"}},
		map[string]*fakeConnector{"renamed": want},
	)

	for _, cfg := range []types.SourceConfig{{Connector: "fake"}, {Type: "fake"}} {
		got, err := set.resolveConnector(context.Background(), cfg)
		if err != nil {
			t.Fatalf("resolveConnector(%+v): %v", cfg, err)
		}
		if got != want {
			t.Errorf("resolveConnector(%+v) did not bind the lone configured connector", cfg)
		}
	}
}

func TestResolveConnectorAmbiguous(t *testing.T) {
	set := newFakeSet(
		[]types.ConnectorConfig{{Name: "a", Type: "fake"}, {Name: "b", Type: "fake"}},
		map[string]*fakeConnector{"a": {}, "b": {}},
	)

	_, err := set.resolveConnector(context.Background(), types.SourceConfig{Type: "fake"})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("resolveConnector with two connectors of the type = %v, want an ambiguity error", err)
	}
}

func TestResolveConnectorStartsDefaultAndShares(t *testing.T) {
	set := newFakeSet(nil, nil)

	first, err := set.resolveConnector(context.Background(), types.SourceConfig{Type: "fake"})
	if err != nil {
		t.Fatalf("resolveConnector: %v", err)
	}
	if first == nil {
		t.Fatal("resolveConnector returned a nil default connector")
	}
	if len(set.running) != 1 {
		t.Errorf("running connectors = %d, want 1 (the default, tracked for teardown)", len(set.running))
	}

	// A second source of the same type discovers and reuses the default instance.
	second, err := set.resolveConnector(context.Background(), types.SourceConfig{Connector: "fake"})
	if err != nil {
		t.Fatalf("resolveConnector (reuse): %v", err)
	}
	if second != first {
		t.Errorf("second resolve started a new connector instead of sharing the default")
	}
	if len(set.running) != 1 {
		t.Errorf("running connectors = %d after reuse, want 1", len(set.running))
	}
}

func TestResolveConnectorUnknown(t *testing.T) {
	set := newFakeSet(nil, nil)

	_, err := set.resolveConnector(context.Background(), types.SourceConfig{Connector: "nope"})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("resolveConnector for an unknown, unregistered connector = %v, want a not-configured error", err)
	}
}

// TestServiceStartsImplicitConnector exercises the full path: a flow whose source
// has no configured connector causes Service.Run to start a default connector on
// demand, run the flow, and tear the connector down.
func TestServiceStartsImplicitConnector(t *testing.T) {
	const count = 8

	core.MustRegisterBlock("e2e.implicit.pass", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			return msg, nil
		}), nil
	})

	reg := core.NewRegistry()
	reg.MustRegister("fake", func() core.Connector { return &fakeConnector{count: count} })

	cfg := types.Config{
		// No connectors configured: the source's type drives an implicit default.
		Flows: []types.FlowConfig{{
			Name:    "implicit-e2e",
			Source:  &types.SourceConfig{Type: "fake"},
			Process: []types.BlockConfig{{Type: "e2e.implicit.pass"}},
		}},
	}

	rec := &recorder{}
	core.DefaultEventBus().Subscribe(rec.handle)

	svc := NewService(cfg, reg)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- svc.Run(ctx) }()

	waitForCompleted(t, rec, count)
	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("Service.Run: %v", err)
	}
}
