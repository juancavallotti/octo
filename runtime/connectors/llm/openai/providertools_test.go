package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v2/responses"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// webSearchResponse is a turn that searched the web before answering.
const webSearchResponse = `{
	"id": "resp_1", "object": "response", "created_at": 1, "model": "gpt-5.4", "status": "completed",
	"output": [
		{"type": "web_search_call", "id": "ws_1", "status": "completed",
		 "action": {"type": "search", "query": "octo release"}},
		{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed",
		 "content": [{"type": "output_text", "text": "It shipped.", "annotations": [
			{"type": "url_citation", "url": "https://example.com", "title": "Example", "start_index": 0, "end_index": 11}
		 ]}]}
	],
	"usage": {"input_tokens": 10, "output_tokens": 4, "total_tokens": 14,
		"input_tokens_details": {"cached_tokens": 0}, "output_tokens_details": {"reasoning_tokens": 0}}
}`

func TestServerToolTranslation(t *testing.T) {
	cases := []struct {
		name string
		tool core.LLMProviderTool
		want string
	}{
		{
			name: "web search",
			tool: core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{
				"searchContextSize": "high", "allowedDomains": []any{"example.com"},
				"userLocation": map[string]any{"country": "US"},
			}},
			want: `{"type":"web_search","filters":{"allowed_domains":["example.com"]},` +
				`"user_location":{"country":"US","type":"approximate"},"search_context_size":"high"}`,
		},
		{
			name: "file search",
			tool: core.LLMProviderTool{Type: toolFileSearch, Settings: types.Settings{
				"vectorStoreIds": []any{"vs_1"}, "maxNumResults": 5,
			}},
			want: `{"vector_store_ids":["vs_1"],"max_num_results":5,"type":"file_search"}`,
		},
		{
			name: "code interpreter",
			tool: core.LLMProviderTool{Type: toolCodeInterpreter, Settings: types.Settings{"fileIds": []any{"file_1"}}},
			want: `{"container":{"file_ids":["file_1"],"type":"auto"},"type":"code_interpreter"}`,
		},
		{
			name: "image generation",
			tool: core.LLMProviderTool{Type: toolImageGeneration, Settings: types.Settings{
				"size": "1024x1024", "outputFormat": "webp",
			}},
			want: `{"output_format":"webp","size":"1024x1024","type":"image_generation"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools, err := toServerTools([]core.LLMProviderTool{tc.tool})
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			got, err := json.Marshal(tools[0])
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("tool =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestValidateProviderToolsRefusals(t *testing.T) {
	cases := []struct {
		name string
		tool core.LLMProviderTool
		want string
	}{
		{"unknown type", core.LLMProviderTool{Type: "web_fetch"}, "not one of: web_search, file_search"},
		{"unknown setting", core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{"maxUses": 1}}, `"web_search"`},
		{"bad context size", core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{
			"searchContextSize": "huge",
		}}, "searchContextSize must be one of"},
		{"no vector store", core.LLMProviderTool{Type: toolFileSearch}, "requires at least one vector store"},
		{"bad image format", core.LLMProviderTool{Type: toolImageGeneration, Settings: types.Settings{
			"outputFormat": "gif",
		}}, "outputFormat must be one of"},
	}
	c := &Connector{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateProviderTools([]core.LLMProviderTool{tc.tool})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// A web search turn is kept opaque and echoed item for item, citations included.
func TestServerToolTurnRoundTripsOpaque(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, webSearchResponse)
	}))
	defer srv.Close()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	req := core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "did octo ship?"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch}},
	}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "It shipped." || resp.Raw.Opaque == nil {
		t.Fatalf("text = %q, opaque = %v", resp.Text, resp.Raw.Opaque)
	}
	tools, _ := bodies[0]["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search" {
		t.Errorf("request tools = %v, want web_search", bodies[0]["tools"])
	}

	req.Messages = append(req.Messages, resp.Raw, core.LLMMessage{Role: core.LLMRoleUser, Text: "source?"})
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	input, _ := bodies[1]["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("input items = %d, want user, web_search_call, message, user: %v", len(input), input)
	}
	search := input[1].(map[string]any)
	if search["type"] != "web_search_call" || search["id"] != "ws_1" {
		t.Errorf("echoed search item = %v", search)
	}
	message := input[2].(map[string]any)
	content := message["content"].([]any)[0].(map[string]any)
	if annotations, _ := content["annotations"].([]any); len(annotations) != 1 {
		t.Errorf("citation not echoed: %v", content)
	}
}

func TestPortableTurnStaysTransparent(t *testing.T) {
	resp, err := translateResponse(&responses.Response{
		Model: "gpt-5",
		Output: []responses.ResponseOutputItemUnion{
			{Type: "image_generation_call", ID: "ig_1", Result: base64.StdEncoding.EncodeToString([]byte("x"))},
		},
	}, "gpt-5")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if resp.Raw.Opaque != nil {
		t.Errorf("an image-only turn should not be kept opaque: %s", resp.Raw.Opaque.Content)
	}
}

func TestGeneratedImageTypeIsSniffed(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	resp, err := translateResponse(&responses.Response{
		Model: "gpt-5",
		Output: []responses.ResponseOutputItemUnion{
			{Type: "image_generation_call", ID: "ig_1", Result: base64.StdEncoding.EncodeToString(jpeg)},
		},
	}, "gpt-5")
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	if got := resp.Media[0]; got.MimeType != "image/jpeg" || got.Name != "ig_1.jpg" {
		t.Errorf("media = %s %s, want image/jpeg ig_1.jpg", got.MimeType, got.Name)
	}
}

func TestForeignOpaqueTurnFallsBackToPortableFields(t *testing.T) {
	items, err := assistantItems(core.LLMMessage{
		Role:   core.LLMRoleAssistant,
		Text:   "hello",
		Opaque: &core.LLMOpaqueTurn{Provider: core.ProviderAnthropic, Content: json.RawMessage(`[]`)},
	})
	if err != nil {
		t.Fatalf("assistant items: %v", err)
	}
	if len(items) != 1 || items[0].OfMessage == nil {
		t.Errorf("items = %+v, want the portable text message", items)
	}
}
