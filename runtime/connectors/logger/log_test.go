package logger

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func TestLogIsPassThrough(t *testing.T) {
	proc, err := newLog(map[string]any{
		"message": `"id=" + body.id`,
		"level":   "debug",
	}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newLog: %v", err)
	}

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"id": "7"}

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Error("Process must forward the same message unchanged")
	}
}

func TestLogWithoutMessageUsesBody(t *testing.T) {
	proc, err := newLog(nil, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newLog: %v", err)
	}

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"ok": true}

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestLogRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
	}{
		{name: "bad level", settings: map[string]any{"level": "loud"}},
		{name: "bad expression", settings: map[string]any{"message": "body."}},
		{name: "non-string message", settings: map[string]any{"message": 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newLog(tt.settings, core.BlockDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

// startFileLogger starts a real logger connector writing text records to a fresh
// temp file and returns the connector plus the file path. The block now binds to
// the connector by concrete type, so the test uses the real one rather than a
// stand-in.
func startFileLogger(t *testing.T) (*Connector, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bound.log")
	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "audit", Type: "logger", Settings: types.Settings{
		"output": path,
		"format": "text",
		"level":  "debug",
	}}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("connector Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c, path
}

func depsFor(name string, c *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == name {
			return c, true
		}
		return nil, false
	}}
}

func TestLogBindsNamedLogger(t *testing.T) {
	conn, path := startFileLogger(t)
	deps := depsFor("audit", conn)

	proc, err := newLog(map[string]any{"logger": "audit", "message": `"hi " + body.who`}, deps)
	if err != nil {
		t.Fatalf("newLog: %v", err)
	}

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"who": "world"}

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	// Close the connector to flush the file before reading it.
	if err := conn.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	got := readFile(t, path)
	if !strings.Contains(got, "hi world") {
		t.Errorf("expected the bound logger to capture the line, got %q", got)
	}
}

func TestLogFullDumpsMessage(t *testing.T) {
	conn, path := startFileLogger(t)
	deps := depsFor("audit", conn)

	proc, err := newLog(map[string]any{"logger": "audit", "full": true}, deps)
	if err != nil {
		t.Fatalf("newLog: %v", err)
	}

	msg, err := types.NewMessage("corr-1")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"who": "world"}
	msg.Variables.Set("tenant", "acme")

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if err := conn.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	got := readFile(t, path)
	for _, want := range []string{"correlation_id=corr-1", "tenant", "event_id=" + msg.EventID} {
		if !strings.Contains(got, want) {
			t.Errorf("full dump missing %q, got %q", want, got)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // path is a t.TempDir() file the test just wrote
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func TestLogUnknownLoggerErrors(t *testing.T) {
	deps := core.BlockDeps{Connector: func(string) (core.Connector, bool) { return nil, false }}
	if _, err := newLog(map[string]any{"logger": "missing"}, deps); err == nil {
		t.Fatal("expected an error for an unknown logger reference")
	}
}
