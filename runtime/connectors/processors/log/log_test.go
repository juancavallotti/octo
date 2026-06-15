package log

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
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

// fakeLogger is a stand-in logger connector that satisfies the logProvider
// capability with a logger writing to an in-memory buffer.
type fakeLogger struct {
	buf *bytes.Buffer
}

func (f *fakeLogger) Start(context.Context, types.ConnectorConfig) error { return nil }
func (f *fakeLogger) Stop(context.Context) error                         { return nil }
func (f *fakeLogger) Logger() (*slog.Logger, error) {
	return slog.New(slog.NewTextHandler(f.buf, nil)), nil
}

func TestLogBindsNamedLogger(t *testing.T) {
	fake := &fakeLogger{buf: &bytes.Buffer{}}
	deps := core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "audit" {
			return fake, true
		}
		return nil, false
	}}

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
	if got := fake.buf.String(); !strings.Contains(got, "hi world") {
		t.Errorf("expected the bound logger to capture the line, got %q", got)
	}
}

func TestLogUnknownLoggerErrors(t *testing.T) {
	deps := core.BlockDeps{Connector: func(string) (core.Connector, bool) { return nil, false }}
	if _, err := newLog(map[string]any{"logger": "missing"}, deps); err == nil {
		t.Fatal("expected an error for an unknown logger reference")
	}
}
