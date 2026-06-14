package cron

import (
	"context"
	"testing"
	"time"

	"github.com/juancavallotti/eip-go/types"
)

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
