package aiblocks

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// fakeLLM is a core.Connector + core.LLMClient that returns a canned response
// and records the request it received.
type fakeLLM struct {
	resp   *core.LLMResponse
	err    error
	gotReq core.LLMRequest
}

func (f *fakeLLM) Start(context.Context, types.ConnectorConfig) error { return nil }
func (f *fakeLLM) Stop(context.Context) error                         { return nil }
func (f *fakeLLM) Complete(_ context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

// depsWith wires a connector under the name "claude", which every test uses.
func depsWith(_ string, conn core.Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == "claude" {
			return conn, true
		}
		return nil, false
	}}
}

func textResponse(s string) *core.LLMResponse {
	return &core.LLMResponse{Text: s, StopReason: core.LLMStopEndTurn}
}

func newMessageWith(t *testing.T, bodyJSON string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(bodyJSON)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}

func TestNewAIMappingValidation(t *testing.T) {
	fake := &fakeLLM{}
	deps := depsWith("claude", fake)

	if _, err := newAIMapping(types.Settings{"connector": "claude"}, deps); err == nil {
		t.Error("expected error when prompt is missing")
	}
	if _, err := newAIMapping(types.Settings{"prompt": "x"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newAIMapping(types.Settings{"connector": "nope", "prompt": "x"}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newAIMapping(types.Settings{
		"connector":    "claude",
		"prompt":       "x",
		"outputSchema": `{"type": "object", "properties": {`, // malformed
	}, deps); err == nil {
		t.Error("expected error compiling a malformed output schema")
	}
}

func TestAIMappingTransformsBody(t *testing.T) {
	fake := &fakeLLM{resp: textResponse(`{"firstName":"Ada","lastName":"Lovelace"}`)}
	proc, err := newAIMapping(types.Settings{
		"connector": "claude",
		"prompt":    "split the name",
	}, depsWith("claude", fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	msg := newMessageWith(t, `{"name":"Ada Lovelace"}`)
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	body, ok := out.Body.(map[string]any)
	if !ok || body["firstName"] != "Ada" || body["lastName"] != "Lovelace" {
		t.Errorf("body = %#v", out.Body)
	}
	// The input body should have been sent to the model.
	if !strings.Contains(fake.gotReq.Messages[0].Text, "Ada Lovelace") {
		t.Errorf("request did not carry the input body: %q", fake.gotReq.Messages[0].Text)
	}
}

func TestAIMappingValidatesAgainstSchema(t *testing.T) {
	schema := `{"type":"object","required":["amount"],"properties":{"amount":{"type":"integer"}}}`

	t.Run("valid output passes and sets BodySchema", func(t *testing.T) {
		fake := &fakeLLM{resp: textResponse(`{"amount": 42}`)}
		proc, err := newAIMapping(types.Settings{
			"connector": "claude", "prompt": "build charge", "outputSchema": schema,
		}, depsWith("claude", fake))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		out, err := proc.Process(context.Background(), newMessageWith(t, `{}`))
		if err != nil {
			t.Fatalf("process: %v", err)
		}
		if len(out.BodySchema) == 0 {
			t.Error("expected BodySchema to be set on success")
		}
	})

	t.Run("invalid output errors", func(t *testing.T) {
		fake := &fakeLLM{resp: textResponse(`{"amount": "not-a-number"}`)}
		proc, err := newAIMapping(types.Settings{
			"connector": "claude", "prompt": "build charge", "outputSchema": schema,
		}, depsWith("claude", fake))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if _, err := proc.Process(context.Background(), newMessageWith(t, `{}`)); err == nil {
			t.Error("expected a schema validation error")
		}
	})
}

func TestAIMappingHandlesNonJSONAndFences(t *testing.T) {
	t.Run("non-JSON errors", func(t *testing.T) {
		fake := &fakeLLM{resp: textResponse("sorry, I cannot help")}
		proc, _ := newAIMapping(types.Settings{"connector": "claude", "prompt": "x"}, depsWith("claude", fake))
		if _, err := proc.Process(context.Background(), newMessageWith(t, `{}`)); err == nil {
			t.Error("expected error for non-JSON response")
		}
	})

	t.Run("markdown fence is stripped", func(t *testing.T) {
		fake := &fakeLLM{resp: textResponse("```json\n{\"ok\":true}\n```")}
		proc, _ := newAIMapping(types.Settings{"connector": "claude", "prompt": "x"}, depsWith("claude", fake))
		out, err := proc.Process(context.Background(), newMessageWith(t, `{}`))
		if err != nil {
			t.Fatalf("process: %v", err)
		}
		if body, ok := out.Body.(map[string]any); !ok || body["ok"] != true {
			t.Errorf("body = %#v", out.Body)
		}
	})
}

func TestAsJSONDocument(t *testing.T) {
	// String form (JSON written as an inline YAML string).
	got, err := asJSONDocument([]byte(`"{\"type\":\"object\"}"`))
	if err != nil || string(got) != `{"type":"object"}` {
		t.Errorf("string form = %q, err=%v", got, err)
	}
	// Native map form.
	got, err = asJSONDocument([]byte(`{"type":"object"}`))
	if err != nil || string(got) != `{"type":"object"}` {
		t.Errorf("map form = %q, err=%v", got, err)
	}
	// Empty / null.
	if got, _ := asJSONDocument([]byte(`null`)); got != nil {
		t.Errorf("null should normalize to nil, got %q", got)
	}
}

// buildSystemPrompt should include each supplied contract.
func TestBuildSystemPrompt(t *testing.T) {
	sys := buildSystemPrompt("do the thing",
		[]byte(`{"in":1}`), []byte(`{"out":2}`), []byte(`{"type":"object"}`))
	for _, want := range []string{"do the thing", `{"in":1}`, `{"out":2}`, "JSON Schema"} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q:\n%s", want, sys)
		}
	}
}
