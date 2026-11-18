package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startLog records which connector instances were started, by name.
type startLog struct {
	mu      sync.Mutex
	started []string
}

func (l *startLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.started...)
}

// loggedConnector records its Start, and fails it when its name is "broken".
type loggedConnector struct{ log *startLog }

func (c *loggedConnector) Start(_ context.Context, cfg types.ConnectorConfig) error {
	if cfg.Name == "broken" {
		return errors.New("bad settings")
	}
	c.log.mu.Lock()
	defer c.log.mu.Unlock()
	c.log.started = append(c.log.started, cfg.Name)
	return nil
}

func (c *loggedConnector) Stop(context.Context) error { return nil }

// invokeSet starts the connectors of a config whose flows name "src-only",
// "shared" and "broken" as sources, in invoke mode.
func invokeSet(t *testing.T) (*connectorSet, *startLog) {
	t.Helper()
	log := &startLog{}
	reg := core.NewRegistry()
	reg.MustRegister("logged", func() core.Connector { return &loggedConnector{log: log} })

	cfg := types.Config{
		Connectors: []types.ConnectorConfig{
			{Name: "src-only", Type: "logged"},
			{Name: "shared", Type: "logged"},
			{Name: "broken", Type: "logged"},
			{Name: "plain", Type: "logged"},
		},
		Flows: []types.FlowConfig{
			{Name: "a", Source: &types.SourceConfig{Connector: "src-only", Type: "logged"}},
			{Name: "b", Source: &types.SourceConfig{Connector: "shared", Type: "logged"}},
			{Name: "c", Source: &types.SourceConfig{Connector: "broken", Type: "logged"}},
		},
	}
	set, err := NewService(cfg, reg, WithInvokeMode()).startConnectors(context.Background())
	if err != nil {
		t.Fatalf("startConnectors: %v", err)
	}
	return set, log
}

func TestInvokeModeDefersSourceConnectorsUntilABlockAsks(t *testing.T) {
	set, log := invokeSet(t)
	if got := log.names(); len(got) != 1 || got[0] != "plain" {
		t.Fatalf("started at boot = %v, want only the connector no flow uses as its source", got)
	}

	// A block asking for a source connector starts it, once.
	first, ok := set.lookup("shared")
	if !ok {
		t.Fatal("lookup(shared) should start the deferred connector")
	}
	again, _ := set.lookup("shared")
	if first != again {
		t.Error("a second lookup started another instance")
	}
	if got := log.names(); len(got) != 2 || got[1] != "shared" {
		t.Errorf("started = %v, want shared started once on lookup", got)
	}
	if len(set.running) != 2 {
		t.Errorf("running = %d, want the deferred start recorded for teardown", len(set.running))
	}

	// A source-only connector nothing asks for never starts.
	for _, name := range log.names() {
		if name == "src-only" {
			t.Error("a source-only connector was started in invoke mode")
		}
	}
}

func TestInvokeModeDeferredStartFailureReadsAsMissing(t *testing.T) {
	set, _ := invokeSet(t)
	if _, ok := set.lookup("broken"); ok {
		t.Fatal("a connector whose Start fails should not be handed to the block")
	}
	if _, ok := set.lookup("nope"); ok {
		t.Fatal("an unconfigured name should not resolve")
	}
}
