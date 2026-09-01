package parallel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const taskRunResponse = `{"run_id":"r1","status":"queued","is_active":true,"processor":"core"}`

// taskStub answers with a created task run and records the request body.
func taskStub(t *testing.T, got *map[string]any, gotPath *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotPath != nil {
			*gotPath = r.URL.Path
		}
		_ = json.NewDecoder(r.Body).Decode(got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(taskRunResponse))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTaskRunStartsARunAndKeepsTheBody(t *testing.T) {
	var got map[string]any
	var path string
	srv := taskStub(t, &got, &path)

	proc, err := newTaskRun(types.Settings{
		"connector": "parallel",
		"processor": "core",
		"input":     "body.question",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}

	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"question": "what is an octopus arm made of",
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if path != "/v1/tasks/runs" {
		t.Errorf("path = %q, want /v1/tasks/runs", path)
	}
	if got["processor"] != "core" {
		t.Errorf("processor = %v, want core", got["processor"])
	}
	if got["input"] != "what is an octopus arm made of" {
		t.Errorf("input = %v", got["input"])
	}
	// The handle is a receipt, not the answer: it goes in a variable and the
	// caller's own body survives to be responded with.
	run, _ := out.Variables[defaultRunVar].(map[string]any)
	if run["run_id"] != "r1" || run["status"] != "queued" {
		t.Errorf("%s = %v, want the run handle", defaultRunVar, out.Variables[defaultRunVar])
	}
	if body, _ := out.Body.(map[string]any); body["question"] == nil {
		t.Error("the incoming body should survive a task run")
	}
}

func TestTaskRunAcceptsAnObjectInput(t *testing.T) {
	var got map[string]any
	srv := taskStub(t, &got, nil)

	proc, err := newTaskRun(types.Settings{
		"connector": "parallel",
		"processor": "core",
		"input":     "body.spec",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}
	_, err = proc.Process(context.Background(), blockMessage(t, map[string]any{
		"spec": map[string]any{"company": "Acme", "field": "revenue"},
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	input, _ := got["input"].(map[string]any)
	if input["company"] != "Acme" {
		t.Errorf("input = %v, want the object", got["input"])
	}
}

func TestTaskRunSendsWebhookConfig(t *testing.T) {
	t.Run("defaults to the one event type Parallel emits", func(t *testing.T) {
		var got map[string]any
		srv := taskStub(t, &got, nil)

		proc, err := newTaskRun(types.Settings{
			"connector":  "parallel",
			"processor":  "core",
			"input":      `"q"`,
			"webhookURL": "https://octo.test/parallel/events",
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newTaskRun: %v", err)
		}
		if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
			t.Fatalf("Process: %v", err)
		}
		webhook, _ := got["webhook"].(map[string]any)
		if webhook["url"] != "https://octo.test/parallel/events" {
			t.Errorf("webhook url = %v", webhook["url"])
		}
		events, _ := webhook["event_types"].([]any)
		if len(events) != 1 || events[0] != defaultEventType {
			t.Errorf("event_types = %v, want [%s]", webhook["event_types"], defaultEventType)
		}
	})

	t.Run("honours configured event types", func(t *testing.T) {
		var got map[string]any
		srv := taskStub(t, &got, nil)

		proc, err := newTaskRun(types.Settings{
			"connector":  "parallel",
			"processor":  "core",
			"input":      `"q"`,
			"webhookURL": "https://octo.test/parallel/events",
			"eventTypes": []any{"task_run.status"},
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newTaskRun: %v", err)
		}
		if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
			t.Fatalf("Process: %v", err)
		}
		webhook, _ := got["webhook"].(map[string]any)
		if events, _ := webhook["event_types"].([]any); len(events) != 1 {
			t.Errorf("event_types = %v", webhook["event_types"])
		}
	})

	t.Run("omits the webhook entirely when no url is set", func(t *testing.T) {
		var got map[string]any
		srv := taskStub(t, &got, nil)

		proc, err := newTaskRun(types.Settings{
			"connector": "parallel",
			"processor": "core",
			"input":     `"q"`,
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newTaskRun: %v", err)
		}
		if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if _, ok := got["webhook"]; ok {
			t.Errorf("webhook should be omitted without a url, got %v", got["webhook"])
		}
	})
}

func TestTaskRunSendsOutputSchemaAndMetadata(t *testing.T) {
	var got map[string]any
	srv := taskStub(t, &got, nil)

	proc, err := newTaskRun(types.Settings{
		"connector":    "parallel",
		"processor":    "core",
		"input":        `"q"`,
		"outputSchema": `{"type": "object", "properties": {"answer": {"type": "string"}}}`,
		"metadata":     `{"correlationId": body.id}`,
		"resultVar":    "run",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"id": "abc"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// output_schema is a tagged union, so a JSON Schema travels wrapped. Sending
	// the schema bare is rejected: its own "type" is "object", not a union tag.
	spec, _ := got["task_spec"].(map[string]any)
	envelope, _ := spec["output_schema"].(map[string]any)
	if envelope["type"] != "json" {
		t.Errorf("task_spec.output_schema.type = %v, want json", envelope["type"])
	}
	schema, _ := envelope["json_schema"].(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("task_spec.output_schema.json_schema = %v, want the schema itself", envelope["json_schema"])
	}
	metadata, _ := got["metadata"].(map[string]any)
	if metadata["correlationId"] != "abc" {
		t.Errorf("metadata = %v, want the message's id", got["metadata"])
	}
	if out.Variables["run"] == nil {
		t.Error("run should hold the handle when resultVar is set")
	}
}

// A bare string is a text schema — the API documents it, and it is the cheapest
// way to say "answer me in prose".
func TestTaskRunPassesAStringOutputSchemaThrough(t *testing.T) {
	var got map[string]any
	srv := taskStub(t, &got, nil)

	proc, err := newTaskRun(types.Settings{
		"connector":    "parallel",
		"processor":    "core",
		"input":        `"q"`,
		"outputSchema": `"a one-paragraph summary"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	spec, _ := got["task_spec"].(map[string]any)
	if spec["output_schema"] != "a one-paragraph summary" {
		t.Errorf("output_schema = %v, want the string untouched", spec["output_schema"])
	}
}

func TestTaskRunRejectsNonObjectSchemaAndMetadata(t *testing.T) {
	for _, field := range []string{"metadata"} {
		var got map[string]any
		srv := taskStub(t, &got, nil)

		cfg := types.Settings{
			"connector": "parallel",
			"processor": "core",
			"input":     `"q"`,
			field:       `"not an object"`,
		}
		proc, err := newTaskRun(cfg, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newTaskRun: %v", err)
		}
		if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
			t.Errorf("expected an error when %s does not evaluate to an object", field)
		}
	}

	// outputSchema takes an object or a string, so only a third kind is an error.
	var got map[string]any
	srv := taskStub(t, &got, nil)
	proc, err := newTaskRun(types.Settings{
		"connector":    "parallel",
		"processor":    "core",
		"input":        `"q"`,
		"outputSchema": "42",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected an error when outputSchema is neither an object nor a string")
	}
}

func TestTaskRunRequiresProcessorAndInput(t *testing.T) {
	cases := []types.Settings{
		{"connector": "parallel", "input": `"q"`},
		{"connector": "parallel", "processor": "core"},
		{"connector": "parallel", "processor": "core", "input": "body."},
		{"connector": "parallel", "processor": "core", "input": `"q"`, "metadata": "body."},
		{"connector": "parallel", "processor": "core", "input": `"q"`, "outputSchema": "body."},
	}
	for _, cfg := range cases {
		if _, err := newTaskRun(cfg, blockDeps(t, "")); err == nil {
			t.Errorf("expected an error for %v", cfg)
		}
	}
}

func TestTaskRunToleratesErrorWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"detail":{"message":"unknown processor"}}`))
	}))
	defer srv.Close()

	failOnError := false
	proc, err := newTaskRun(types.Settings{
		"connector":   "parallel",
		"processor":   "nope",
		"input":       `"q"`,
		"failOnError": &failOnError,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newTaskRun: %v", err)
	}
	msg := blockMessage(t, map[string]any{"in": true})
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process should tolerate the error: %v", err)
	}
	if out.Variables[defaultRunVar] != nil {
		t.Error("a tolerated error should leave no run handle behind")
	}
}
