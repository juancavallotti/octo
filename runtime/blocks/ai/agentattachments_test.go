package ai

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// textOnlyLLM is a provider with no media half at all — a connector predating
// core.LLMMedia, or one whose configured model reads only text.
type textOnlyLLM struct{ scriptedLLM }

func (t *textOnlyLLM) AcceptsMedia() []string { return nil }

// attachmentRegistry is the block registry these tests build against: the shared
// one plus a no-op `tool` for the agent's single branch to run.
func attachmentRegistry() *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("tool", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, m *types.Message) (*types.Message, error) {
			return m, nil
		}), nil
	})
	return reg
}

// attachmentAgent builds an ai-agent whose opening turn carries whatever the
// message body put under `files`.
func attachmentAgent(settings types.Settings) types.BlockConfig {
	cfg := types.Settings{
		"connector":   "claude",
		"prompt":      "look at this",
		"answer":      "text",
		"input":       "body.question",
		"attachments": "has(body.files) ? body.files : []",
		"tools":       []map[string]any{toolBranch("noop", "does nothing", nil)},
	}
	for k, v := range settings {
		cfg[k] = v
	}
	return types.BlockConfig{Type: "ai-agent", Settings: cfg}
}

// messageWithFile is an inbound message carrying one attachment, written the way
// a flow would put it on the body.
func messageWithFile(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	body := `{"question":"what is this?","files":[{"mimeType":"image/png","name":"shot.png","data":"` +
		base64.StdEncoding.EncodeToString([]byte("PNGBYTES")) + `"}]}`
	if err := msg.SetBodyJSON([]byte(body)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}

func TestAgentSendsAttachmentsOnItsOpeningTurn(t *testing.T) {
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("a screenshot")}}
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), attachmentAgent(nil))

	if _, err := proc.Process(context.Background(), messageWithFile(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.calls))
	}
	opening := fake.calls[0].Messages[0]
	if opening.Role != core.LLMRoleUser {
		t.Fatalf("opening role = %q, want user", opening.Role)
	}
	if len(opening.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(opening.Attachments))
	}
	got := opening.Attachments[0]
	if got.MimeType != "image/png" || string(got.Data) != "PNGBYTES" || got.Name != "shot.png" {
		t.Errorf("attachment = %+v, want the decoded png", got)
	}
}

// The heart of the one-turn rule: the model reads the file once, and every later
// turn of the same run is text.
func TestAgentShedsAttachmentsAfterTheFirstCall(t *testing.T) {
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("noop", `{}`),
		endTurnResp("done"),
	}}
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), attachmentAgent(nil))

	if _, err := proc.Process(context.Background(), messageWithFile(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fake.calls))
	}
	if len(fake.calls[0].Messages[0].Attachments) != 1 {
		t.Error("the first call did not carry the attachment")
	}
	for _, m := range fake.calls[1].Messages {
		if len(m.Attachments) != 0 {
			t.Errorf("the second call still carries %d attachments", len(m.Attachments))
		}
	}
}

func TestAgentKeepsAttachmentsWhenAsked(t *testing.T) {
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("noop", `{}`),
		endTurnResp("done"),
	}}
	cfg := attachmentAgent(types.Settings{"keepAttachments": true})
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)

	if _, err := proc.Process(context.Background(), messageWithFile(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fake.calls))
	}
	if len(fake.calls[1].Messages[0].Attachments) != 1 {
		t.Error("keepAttachments did not hold the attachment onto the second turn")
	}
}

// The one that matters. Working memory is a JSON transcript of LLMMessage, so a
// field left on it is a field persisted — and a conversation that stored a
// megabyte of image reloads it on every later run and pays for it again.
func TestAgentNeverPersistsAttachments(t *testing.T) {
	for name, responses := range map[string][]*core.LLMResponse{
		"answered":  {endTurnResp("a screenshot")},
		"tool loop": {toolCallResp("noop", `{}`), endTurnResp("done")},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &scriptedLLM{responses: responses}
			ctx, mem, _ := withFakeMemory(context.Background())
			cfg := attachmentAgent(types.Settings{
				"agentId":        "looker",
				"memoryThreadId": `"t1"`,
			})
			proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)
			if _, err := proc.Process(ctx, messageWithFile(t)); err != nil {
				t.Fatalf("process: %v", err)
			}
			assertNoStoredAttachments(t, mem, "looker", "t1")
		})
	}
}

// The shed runs before the failed call is handled, and that ordering is the
// whole reason it sits where it does: callFailed leads to halt, which persists
// the transcript, so a shed that only ran on success would write the bytes into
// working memory on every failed first turn — the one path most likely to be
// retried.
//
// Pinned here as a unit because reaching halt through Process needs a stop
// signal, which would test the signal rather than the ordering.
func TestShedAttachmentsClearsTheTranscriptOnTheFirstTurn(t *testing.T) {
	withFile := func() []core.LLMMessage {
		return []core.LLMMessage{{
			Role:        core.LLMRoleUser,
			Text:        "what is this?",
			Attachments: []core.LLMAttachment{{MimeType: "image/png", Data: []byte("PNGBYTES")}},
		}}
	}
	a := &aiAgent{}
	if got := a.shedAttachments(withFile(), 0); len(got[0].Attachments) != 0 {
		t.Error("the first turn did not shed its attachments")
	}
	// Later turns are left alone: by then there is nothing to shed, and a walk over
	// the whole transcript on every turn would be work for nothing.
	if got := a.shedAttachments(withFile(), 1); len(got[0].Attachments) != 1 {
		t.Error("a later turn should leave the transcript alone")
	}
	keep := &aiAgent{keepAttachments: true}
	if got := keep.shedAttachments(withFile(), 0); len(got[0].Attachments) != 1 {
		t.Error("keepAttachments did not hold the attachment")
	}
}

func assertNoStoredAttachments(t *testing.T, mem *fakeMemory, agentID, thread string) {
	t.Helper()
	wm, ok, err := mem.LoadWorking(context.Background(),
		core.MemoryRef{AgentID: agentID, ThreadKey: thread})
	if err != nil {
		t.Fatalf("load working: %v", err)
	}
	if !ok {
		return // nothing was saved at all, which is also no attachments
	}
	if strings.Contains(string(wm.Payload), base64.StdEncoding.EncodeToString([]byte("PNGBYTES"))) {
		t.Error("the attachment's bytes reached working memory")
	}
	env, err := decodeMemory(wm.Payload)
	if err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	for i, m := range env.Messages {
		if len(m.Attachments) != 0 {
			t.Errorf("stored message %d carries %d attachments", i, len(m.Attachments))
		}
	}
}

// A block that cannot possibly work should not deploy. The editor shows a build
// error while somebody is looking at the block; a turn-time failure reaches them
// as an errored run some time later.
func TestAgentRefusesAttachmentsAgainstATextOnlyModel(t *testing.T) {
	fake := &textOnlyLLM{}
	_, err := tryBuildBlock(attachmentRegistry(), depsLLM(fake), attachmentAgent(nil))
	if err == nil {
		t.Fatal("expected a build error")
	}
	for _, want := range []string{"attachments", "claude", "reads only text"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// Without the setting, a text-only connector is none of this block's business.
func TestAgentWithoutAttachmentsBuildsAgainstATextOnlyModel(t *testing.T) {
	cfg := attachmentAgent(nil)
	delete(cfg.Settings, "attachments")
	if _, err := tryBuildBlock(attachmentRegistry(), depsLLM(&textOnlyLLM{}), cfg); err != nil {
		t.Errorf("build: %v", err)
	}
}

func TestAgentWritesGeneratedMediaToItsVariable(t *testing.T) {
	made := core.LLMAttachment{MimeType: "image/png", Data: []byte("CHART"), Name: "chart.png"}
	resp := endTurnResp("here you go")
	resp.Media = []core.LLMAttachment{made}
	fake := &scriptedLLM{responses: []*core.LLMResponse{resp}}
	cfg := attachmentAgent(types.Settings{"responseMedia": "art"})
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)

	out, err := proc.Process(context.Background(), messageWithFile(t))
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
	if file["data"] != base64.StdEncoding.EncodeToString([]byte("CHART")) {
		t.Errorf("data = %v, want the base64 of the bytes", file["data"])
	}
}

// A model that makes an image and then calls a tool to do something with it made
// that image on a turn that was not the last one.
func TestAgentAccumulatesMediaAcrossTurns(t *testing.T) {
	first := toolCallResp("noop", `{}`)
	first.Media = []core.LLMAttachment{{MimeType: "image/png", Data: []byte("ONE")}}
	last := endTurnResp("done")
	last.Media = []core.LLMAttachment{{MimeType: "image/png", Data: []byte("TWO")}}
	fake := &scriptedLLM{responses: []*core.LLMResponse{first, last}}
	cfg := attachmentAgent(types.Settings{"responseMedia": "art"})
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)

	out, err := proc.Process(context.Background(), messageWithFile(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	files, _ := out.Variables["art"].([]any)
	if len(files) != 2 {
		t.Fatalf("vars.art = %#v, want both turns' files", out.Variables["art"])
	}
}

// An agent that produces no media writes nothing, which is what the empty
// setting means everywhere else in the runtime.
func TestAgentWritesNoMediaVariableWhenThereIsNone(t *testing.T) {
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("just words")}}
	cfg := attachmentAgent(types.Settings{"responseMedia": "art"})
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)

	out, err := proc.Process(context.Background(), messageWithFile(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if _, present := out.Variables["art"]; present {
		t.Errorf("vars.art = %#v, want the variable left unwritten", out.Variables["art"])
	}
}

// The default opening turn is the whole body as JSON, so a flow that put its
// files on the body would send every one of them twice — once as a file, once as
// base64 prose. The prose copy is not an attachment, so the shed does not reach
// it and it persists into working memory to be re-billed on every later turn.
//
// It works and it costs money quietly, which is why this is refused rather than
// documented.
func TestAgentRefusesAttachmentsWithoutAnOpeningTurn(t *testing.T) {
	cfg := attachmentAgent(nil)
	delete(cfg.Settings, "input")
	_, err := tryBuildBlock(attachmentRegistry(), depsLLM(&scriptedLLM{}), cfg)
	if err == nil {
		t.Fatal("expected a build error")
	}
	for _, want := range []string{"attachments requires input", "base64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
