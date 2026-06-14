package log

import (
	"context"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

func TestLogIsPassThrough(t *testing.T) {
	proc, err := newLog(map[string]any{
		"message": `"id=" + body.id`,
		"level":   "debug",
	})
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
	proc, err := newLog(nil)
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
			if _, err := newLog(tt.settings); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
