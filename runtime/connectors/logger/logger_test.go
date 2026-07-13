package logger

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// shipperServices is a RuntimeServices that also ships logs to a captured handler,
// used to verify the connector tees its output through the central sink.
type shipperServices struct {
	core.RuntimeServices
	sink slog.Handler
}

func (s shipperServices) LogSink() slog.Handler { return s.sink }

// TestLoggerTeesToLogSink checks that when the runtime services in context ship
// logs, the connector's logger writes both to its own output and to the sink.
func TestLoggerTeesToLogSink(t *testing.T) {
	var shipped bytes.Buffer
	ctx := core.ContextWithRuntimeServices(context.Background(), shipperServices{
		RuntimeServices: core.NoopRuntimeServices(),
		sink:            slog.NewJSONHandler(&shipped, nil),
	})

	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "audit", Type: "logger", Settings: types.Settings{"format": "json"}}
	if err := c.Start(ctx, cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	logger, err := c.Logger()
	if err != nil {
		t.Fatalf("Logger: %v", err)
	}
	logger.Info("teed", "n", 1)

	if got := shipped.String(); !strings.Contains(got, `"msg":"teed"`) {
		t.Errorf("sink did not receive the record: %q", got)
	}
}

// TestLoggerNoSinkWhenServicesDoNotShip confirms a non-shipping module leaves the
// connector logger unchanged (no panic, no central sink wired).
func TestLoggerNoSinkWhenServicesDoNotShip(t *testing.T) {
	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "audit", Type: "logger", Settings: types.Settings{}}
	// NoopRuntimeServices does not implement LogShipper.
	ctx := core.ContextWithRuntimeServices(context.Background(), core.NoopRuntimeServices())
	if err := c.Start(ctx, cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := c.Logger(); err != nil {
		t.Fatalf("Logger: %v", err)
	}
}

func TestLoggerWritesToFileAndClosesIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")

	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "audit", Type: "logger", Settings: types.Settings{
		"output": path,
		"format": "json",
		"level":  "info",
	}}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}

	logger, err := c.Logger()
	if err != nil {
		t.Fatalf("Logger: %v", err)
	}
	logger.Info("hello", "n", 1)

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // path is a t.TempDir() file the test just wrote
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := string(data); !strings.Contains(got, `"msg":"hello"`) {
		t.Errorf("log file = %q, want a json record with msg=hello", got)
	}
}

func TestLoggerDefaultsNeedNoSettings(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Type: "logger"}); err != nil {
		t.Fatalf("Start with no settings: %v", err)
	}
	if _, err := c.Logger(); err != nil {
		t.Fatalf("Logger: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestLoggerRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{name: "bad format", settings: types.Settings{"format": "xml"}},
		{name: "bad level", settings: types.Settings{"level": "loud"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connector{}
			if err := c.Start(context.Background(), types.ConnectorConfig{Settings: tt.settings}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
