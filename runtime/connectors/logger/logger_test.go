package logger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/types"
)

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
