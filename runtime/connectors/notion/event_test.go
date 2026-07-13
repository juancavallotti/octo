package notion

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestEventNormalizesAndFilters(t *testing.T) {
	raw := `{"type":"page.content_updated","workspace_id":"W1","timestamp":"2024-01-01T00:00:00Z",` +
		`"entity":{"id":"p1","type":"page"}}`

	proc, err := newEvent(types.Settings{"eventTypes": []any{"page.content_updated"}}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}

	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected the page.content_updated event to pass the filter")
	}
	body := out.Body.(map[string]any)
	if body["type"] != "page.content_updated" || body["entityId"] != "p1" || body["entityType"] != "page" {
		t.Errorf("normalized body = %v", body)
	}
	if body["workspaceId"] != "W1" {
		t.Errorf("normalized body = %v", body)
	}
	if _, ok := body["raw"].(map[string]any); !ok {
		t.Errorf("expected raw event under body.raw, got %T", body["raw"])
	}
}

func TestEventDropsDisallowedType(t *testing.T) {
	raw := `{"type":"comment.created","entity":{"id":"c1","type":"comment"}}`
	proc, err := newEvent(types.Settings{"eventTypes": []any{"page.created"}}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Errorf("expected a disallowed event type to be dropped, got %v", out.Body)
	}
}

func TestEventFilterPredicate(t *testing.T) {
	raw := `{"type":"page.content_updated","entity":{"id":"p1","type":"page"}}`
	proc, err := newEvent(types.Settings{
		"eventTypes": []any{"page.content_updated"},
		"filter":     `body.entityType == "database"`, // keep only database entities
	}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Errorf("expected the page entity to be filtered out, got %v", out.Body)
	}
}
