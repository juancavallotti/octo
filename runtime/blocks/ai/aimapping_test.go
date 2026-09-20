package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
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
	deps := depsLLM(fake)

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
	}, depsLLM(fake))
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
		}, depsLLM(fake))
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
		}, depsLLM(fake))
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
		proc, _ := newAIMapping(types.Settings{"connector": "claude", "prompt": "x"}, depsLLM(fake))
		if _, err := proc.Process(context.Background(), newMessageWith(t, `{}`)); err == nil {
			t.Error("expected error for non-JSON response")
		}
	})

	t.Run("markdown fence is stripped", func(t *testing.T) {
		fake := &fakeLLM{resp: textResponse("```json\n{\"ok\":true}\n```")}
		proc, _ := newAIMapping(types.Settings{"connector": "claude", "prompt": "x"}, depsLLM(fake))
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

// AcceptsMedia makes the double a multimodal provider, so a mapping configured
// to send attachments against it builds.
func (f *fakeLLM) AcceptsMedia() []string { return []string{"image/png"} }

// textOnlyMapper is a provider that reads no media at all.
type textOnlyMapper struct{ fakeLLM }

func (t *textOnlyMapper) AcceptsMedia() []string { return nil }

// mappingSettingsWith is an ai-mapping that states its turn and sends whatever
// the body put under `files`.
func mappingSettingsWith(extra types.Settings) types.Settings {
	cfg := types.Settings{
		"connector":   "claude",
		"prompt":      "describe the picture",
		"input":       "body.question",
		"attachments": "body.files",
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

const mappingFileBody = `{"question":"what is this?","files":[` +
	`{"mimeType":"image/png","name":"shot.png","data":"UE5HQllURVM="}]}`

func TestAIMappingSendsAttachments(t *testing.T) {
	fake := &fakeLLM{resp: textResponse(`{"seen":"a screenshot"}`)}
	proc, err := newAIMapping(mappingSettingsWith(nil), depsLLM(fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := proc.Process(context.Background(), newMessageWith(t, mappingFileBody)); err != nil {
		t.Fatalf("process: %v", err)
	}
	sent := fake.gotReq.Messages[0]
	if sent.Text != "what is this?" {
		t.Errorf("turn = %q, want the stated input and not the whole body", sent.Text)
	}
	if len(sent.Attachments) != 1 || sent.Attachments[0].MimeType != "image/png" {
		t.Fatalf("attachments = %+v, want the png", sent.Attachments)
	}
	if got := string(sent.Attachments[0].Data); got != "PNGBYTES" {
		t.Errorf("data = %q, want the decoded bytes", got)
	}
}

// Without input the turn is still the whole body, exactly as it was before this
// block could send anything but text.
func TestAIMappingWithoutInputStillSendsTheBody(t *testing.T) {
	fake := &fakeLLM{resp: textResponse(`{"ok":true}`)}
	proc, err := newAIMapping(types.Settings{
		"connector": "claude", "prompt": "reshape",
	}, depsLLM(fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := proc.Process(context.Background(), newMessageWith(t, `{"name":"Ada"}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if !strings.Contains(fake.gotReq.Messages[0].Text, "Ada") {
		t.Errorf("turn = %q, want the whole body", fake.gotReq.Messages[0].Text)
	}
}

// The default turn is the body, so a flow that put its files there would send
// each of them twice — once as a file, once as base64 in the prompt. Stating the
// turn is how an author says which half of the body is which.
func TestAIMappingRefusesAttachmentsWithoutInput(t *testing.T) {
	cfg := mappingSettingsWith(nil)
	delete(cfg, "input")
	_, err := newAIMapping(cfg, depsLLM(&fakeLLM{}))
	if err == nil {
		t.Fatal("expected a build error")
	}
	for _, want := range []string{"attachments requires input", "base64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestAIMappingRefusesAttachmentsAgainstATextOnlyModel(t *testing.T) {
	_, err := newAIMapping(mappingSettingsWith(nil), depsLLM(&textOnlyMapper{}))
	if err == nil {
		t.Fatal("expected a build error")
	}
	if !strings.Contains(err.Error(), "reads only text") {
		t.Errorf("error = %v, want one naming the text-only connector", err)
	}
}

func TestAIMappingWritesGeneratedMedia(t *testing.T) {
	resp := textResponse(`{"ok":true}`)
	resp.Media = []core.LLMAttachment{{MimeType: "image/png", Data: []byte("CHART"), Name: "chart.png"}}
	fake := &fakeLLM{resp: resp}
	proc, err := newAIMapping(mappingSettingsWith(types.Settings{"responseMedia": "art"}), depsLLM(fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := proc.Process(context.Background(), newMessageWith(t, mappingFileBody))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	files, ok := out.Variables["art"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("vars.art = %#v, want one file", out.Variables["art"])
	}
	file, _ := files[0].(map[string]any)
	if file["mimeType"] != "image/png" || file["name"] != "chart.png" || file["size"] != len("CHART") {
		t.Errorf("file = %#v", file)
	}
}

func TestAIMappingWritesNoMediaVariableWhenThereIsNone(t *testing.T) {
	fake := &fakeLLM{resp: textResponse(`{"ok":true}`)}
	proc, err := newAIMapping(mappingSettingsWith(types.Settings{"responseMedia": "art"}), depsLLM(fake))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out, err := proc.Process(context.Background(), newMessageWith(t, mappingFileBody))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, present := out.Variables["art"]; present {
		t.Errorf("vars.art = %#v, want the variable left unwritten", out.Variables["art"])
	}
}
