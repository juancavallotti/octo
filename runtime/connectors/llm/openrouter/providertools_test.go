package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The web search entry rides beside the function tools, in OpenRouter's own
// shape, with only the parameters that were set.
func TestWebSearchRidesTheRequest(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"gen-1","object":"chat.completion","created":1,"model":"openai/gpt-5.4",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"It shipped."}}],
			"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
	}))
	defer srv.Close()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "router", Settings: types.Settings{"apiKey": "sk-or-test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, err := c.Complete(context.Background(), core.LLMRequest{
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "did octo ship?"}},
		Tools:    []core.LLMTool{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch, Settings: types.Settings{
			"engine": "exa", "maxResults": 3, "excludedDomains": []any{"spam.example"},
		}}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "It shipped." {
		t.Errorf("text = %q", resp.Text)
	}
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %v, want the function and the search", gotBody["tools"])
	}
	got, _ := json.Marshal(tools[1])
	want := `{"parameters":{"engine":"exa","excluded_domains":["spam.example"],"max_results":3},` +
		`"type":"openrouter:web_search"}`
	if string(got) != want {
		t.Errorf("search entry =\n%s\nwant\n%s", got, want)
	}
}

func TestBareWebSearchCarriesNoParameters(t *testing.T) {
	tools, err := toServerTools([]core.LLMProviderTool{{Type: toolWebSearch}})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	got, _ := json.Marshal(tools[0])
	if string(got) != `{"type":"openrouter:web_search"}` {
		t.Errorf("entry = %s", got)
	}
}

func TestValidateProviderToolsRefusals(t *testing.T) {
	cases := []struct {
		name     string
		tool     core.LLMProviderTool
		contains string
	}{
		{"unknown type", core.LLMProviderTool{Type: "web_fetch"}, "not one of: web_search"},
		{"unknown setting", core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{"mode": "deep"}}, `"web_search"`},
		{"bad engine", core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{"engine": "bing"}}, "engine must be one of"},
		{"too many results", core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{"maxResults": 30}}, "between 1 and 25"},
	}
	c := &Connector{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateProviderTools([]core.LLMProviderTool{tc.tool})
			if err == nil || !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.contains)
			}
		})
	}
}
