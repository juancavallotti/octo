package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// serverToolLLM is a provider that runs one server-side tool, web_search, and
// rejects every other type the way a real connector names its catalogue.
type serverToolLLM struct{ scriptedLLM }

func (s *serverToolLLM) ValidateProviderTools(tools []core.LLMProviderTool) error {
	for _, t := range tools {
		if t.Type != "web_search" {
			return errors.New(`provider tool "` + t.Type + `" is not one of: web_search`)
		}
	}
	return nil
}

func searchAgent(settings types.Settings) types.BlockConfig {
	cfg := types.Settings{
		"connector":     "claude",
		"prompt":        "answer with current facts",
		"answer":        "text",
		"providerTools": []map[string]any{{"type": "web_search", "settings": map[string]any{"maxUses": 3}}},
	}
	for k, v := range settings {
		cfg[k] = v
	}
	return types.BlockConfig{Type: "ai-agent", Settings: cfg}
}

func TestAgentSendsProviderToolsOnEveryTurn(t *testing.T) {
	fake := &serverToolLLM{scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("noop", `{}`), endTurnResp("done"),
	}}}
	cfg := searchAgent(types.Settings{"tools": []map[string]any{toolBranch("noop", "does nothing", nil)}})
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), cfg)

	if _, err := proc.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fake.calls))
	}
	for i, call := range fake.calls {
		if len(call.ProviderTools) != 1 || call.ProviderTools[0].Type != "web_search" {
			t.Fatalf("call %d provider tools = %+v, want web_search", i, call.ProviderTools)
		}
		if got, _ := call.ProviderTools[0].Settings.Int("maxUses"); got != 3 {
			t.Errorf("call %d maxUses = %d, want 3", i, got)
		}
	}
}

func TestAgentBuildsWithOnlyProviderTools(t *testing.T) {
	fake := &serverToolLLM{scriptedLLM{responses: []*core.LLMResponse{endTurnResp("grounded")}}}
	proc := mustBuildAI(t, attachmentRegistry(), depsLLM(fake), searchAgent(nil))

	if _, err := proc.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls[0].Tools) != 0 {
		t.Errorf("function tools = %+v, want none", fake.calls[0].Tools)
	}
}

func TestProviderToolBuildRefusals(t *testing.T) {
	cases := []struct {
		name string
		conn core.Connector
		cfg  types.BlockConfig
		want string
	}{
		{
			name: "no tools of either kind",
			conn: &serverToolLLM{},
			cfg:  searchAgent(types.Settings{"providerTools": []map[string]any{}}),
			want: "at least one tool or provider tool",
		},
		{
			name: "connector runs none",
			conn: &scriptedLLM{},
			cfg:  searchAgent(nil),
			want: `connector "claude" runs no provider tools`,
		},
		{
			name: "type outside the catalogue",
			conn: &serverToolLLM{},
			cfg:  searchAgent(types.Settings{"providerTools": []map[string]any{{"type": "code_execution"}}}),
			want: `"code_execution" is not one of: web_search`,
		},
		{
			name: "missing type",
			conn: &serverToolLLM{},
			cfg:  searchAgent(types.Settings{"providerTools": []map[string]any{{"settings": map[string]any{}}}}),
			want: "provider tool 0 requires a type",
		},
		{
			name: "collides with a function tool",
			conn: &serverToolLLM{},
			cfg: searchAgent(types.Settings{
				"tools": []map[string]any{toolBranch("web_search", "my own search", nil)},
			}),
			want: `provider tool "web_search" collides`,
		},
		{
			name: "duplicate provider tool",
			conn: &serverToolLLM{},
			cfg: searchAgent(types.Settings{
				"providerTools": []map[string]any{{"type": "web_search"}, {"type": "web_search"}},
			}),
			want: `provider tool "web_search" collides`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tryBuildBlock(attachmentRegistry(), depsLLM(tc.conn), tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestMappingSendsProviderTools(t *testing.T) {
	fake := &serverToolLLM{scriptedLLM{responses: []*core.LLMResponse{endTurnResp(`{"ok":true}`)}}}
	cfg := types.BlockConfig{Type: "ai-mapping", Settings: types.Settings{
		"connector":     "claude",
		"prompt":        "enrich with current facts",
		"providerTools": []map[string]any{{"type": "web_search"}},
	}}
	proc := mustBuildAI(t, testRegistry(), depsLLM(fake), cfg)

	if _, err := proc.Process(context.Background(), mustMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if got := fake.calls[0].ProviderTools; len(got) != 1 || got[0].Type != "web_search" {
		t.Errorf("provider tools = %+v, want web_search", got)
	}
}

func TestMappingRefusesProviderToolsOnAConnectorWithoutThem(t *testing.T) {
	cfg := types.BlockConfig{Type: "ai-mapping", Settings: types.Settings{
		"connector":     "claude",
		"prompt":        "enrich",
		"providerTools": []map[string]any{{"type": "web_search"}},
	}}
	_, err := tryBuildBlock(testRegistry(), depsLLM(&scriptedLLM{}), cfg)
	if err == nil || !strings.Contains(err.Error(), "runs no provider tools") {
		t.Fatalf("err = %v, want a refusal naming provider tools", err)
	}
}
