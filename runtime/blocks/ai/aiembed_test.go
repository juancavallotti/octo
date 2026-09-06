package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeEmbedder is a core.Connector + core.EmbedClient that returns a canned
// response and records the request it received.
type fakeEmbedder struct {
	resp   *core.EmbedResponse
	err    error
	gotReq core.EmbedRequest
}

func (f *fakeEmbedder) Start(context.Context, types.ConnectorConfig) error { return nil }
func (f *fakeEmbedder) Stop(context.Context) error                         { return nil }
func (f *fakeEmbedder) Embed(_ context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	f.gotReq = req
	return f.resp, f.err
}

func TestNewAIEmbedValidation(t *testing.T) {
	fake := &fakeEmbedder{}
	deps := depsLLM(fake)

	if _, err := newAIEmbed(types.Settings{"connector": "claude", "text": "body.text"}, deps); err == nil {
		t.Error("expected error when model is missing")
	}
	if _, err := newAIEmbed(types.Settings{"model": "text-embedding-3-small", "text": "body.text"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newAIEmbed(types.Settings{"connector": "claude", "model": "text-embedding-3-small"}, deps); err == nil {
		t.Error("expected error when text is missing")
	}
	if _, err := newAIEmbed(types.Settings{"connector": "nope", "model": "m", "text": "body.text"}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newAIEmbed(types.Settings{
		"connector": "claude", "model": "m", "text": "body.(",
	}, deps); err == nil {
		t.Error("expected error compiling a malformed text expression")
	}
}

// TestResolveEmbedRejectsNonEmbedConnector proves ai-embed rejects a connector
// that satisfies core.LLMClient but not core.EmbedClient — the mechanism by
// which pointing ai-embed at an llm-anthropic connector fails at build time.
func TestResolveEmbedRejectsNonEmbedConnector(t *testing.T) {
	fake := &fakeLLM{} // Connector + LLMClient, no Embed method — stands in for llm-anthropic.
	deps := depsLLM(fake)

	_, err := newAIEmbed(types.Settings{"connector": "claude", "model": "m", "text": "body.text"}, deps)
	if err == nil || !strings.Contains(err.Error(), "does not support embeddings") {
		t.Fatalf("expected embeddings-unsupported error, got %v", err)
	}
}

func TestAIEmbedSingleText(t *testing.T) {
	fake := &fakeEmbedder{resp: &core.EmbedResponse{Vectors: [][]float32{{0.1, 0.2}}}}
	proc, err := newAIEmbed(types.Settings{
		"connector": "claude",
		"model":     "text-embedding-3-small",
		"text":      "body.text",
	}, depsLLM(fake))
	if err != nil {
		t.Fatalf("newAIEmbed: %v", err)
	}

	msg := newMessageWith(t, `{"text":"hello"}`)
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if len(fake.gotReq.Input) != 1 || fake.gotReq.Input[0] != "hello" {
		t.Errorf("embed request input = %+v", fake.gotReq.Input)
	}
	vec, ok := out.Variables["embedding"].([]float32)
	if !ok || len(vec) != 2 {
		t.Fatalf("embedding var = %#v", out.Variables["embedding"])
	}
}

func TestAIEmbedBatchText(t *testing.T) {
	fake := &fakeEmbedder{resp: &core.EmbedResponse{Vectors: [][]float32{{0.1}, {0.2}}}}
	proc, err := newAIEmbed(types.Settings{
		"connector": "claude",
		"model":     "text-embedding-3-small",
		"text":      "body.texts",
		"resultVar": "vecs",
	}, depsLLM(fake))
	if err != nil {
		t.Fatalf("newAIEmbed: %v", err)
	}

	msg := newMessageWith(t, `{"texts":["a","b"]}`)
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}

	if len(fake.gotReq.Input) != 2 || fake.gotReq.Input[0] != "a" || fake.gotReq.Input[1] != "b" {
		t.Errorf("embed request input = %+v", fake.gotReq.Input)
	}
	vecs, ok := out.Variables["vecs"].([][]float32)
	if !ok || len(vecs) != 2 {
		t.Fatalf("vecs var = %#v", out.Variables["vecs"])
	}
}

func TestAIEmbedRejectsNonStringElements(t *testing.T) {
	fake := &fakeEmbedder{resp: &core.EmbedResponse{Vectors: [][]float32{{0.1}}}}
	proc, err := newAIEmbed(types.Settings{
		"connector": "claude",
		"model":     "m",
		"text":      "body.texts",
	}, depsLLM(fake))
	if err != nil {
		t.Fatalf("newAIEmbed: %v", err)
	}

	msg := newMessageWith(t, `{"texts":[1,2]}`)
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected error for non-string list elements")
	}
}

// TestAIEmbedRejectsAMisshapenResponse: the provider is a connector somebody
// else wrote, so a nil response or a vector list that does not match the texts
// sent is a flow error, not a panic in the worker or a silently misaligned result.
func TestAIEmbedRejectsAMisshapenResponse(t *testing.T) {
	cases := map[string]struct {
		resp *core.EmbedResponse
		text string
	}{
		"nil response":                    {resp: nil, text: `{"text":"hello"}`},
		"no vectors for one text":         {resp: &core.EmbedResponse{}, text: `{"text":"hello"}`},
		"fewer vectors than batch inputs": {resp: &core.EmbedResponse{Vectors: [][]float32{{0.1}}}, text: `{"text":["a","b"]}`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeEmbedder{resp: tc.resp}
			proc, err := newAIEmbed(types.Settings{
				"connector": "claude",
				"model":     "text-embedding-3-small",
				"text":      "body.text",
			}, depsLLM(fake))
			if err != nil {
				t.Fatalf("newAIEmbed: %v", err)
			}
			if _, err := proc.Process(context.Background(), newMessageWith(t, tc.text)); err == nil {
				t.Fatal("expected a flow error for a response that does not match the request")
			}
		})
	}
}
