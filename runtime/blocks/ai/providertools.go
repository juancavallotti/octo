package ai

import (
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// providerToolSettings is one server-side tool a block enables: Type names it in
// the connector's catalogue, and Settings configures it in that tool's own terms.
type providerToolSettings struct {
	Type     string         `json:"type"`
	Settings types.Settings `json:"settings,omitempty"`
}

// providerTools resolves a block's provider tools against the connector behind
// the caller, so a tool the connector does not run, or settings it cannot
// decode, fail the build rather than the first turn.
//
// tools is the block's function tools: a provider tool and a function tool the
// model sees under one name are a request no provider accepts.
func (c *llmCaller) providerTools(
	kind, connector string, list []providerToolSettings, tools []core.LLMTool,
) ([]core.LLMProviderTool, error) {
	if len(list) == 0 {
		return nil, nil
	}
	if c.serverTools == nil {
		return nil, fmt.Errorf("%s block: connector %q runs no provider tools", kind, connector)
	}
	taken := make(map[string]bool, len(tools)+len(list))
	for _, t := range tools {
		taken[t.Name] = true
	}
	out := make([]core.LLMProviderTool, 0, len(list))
	for i, t := range list {
		if t.Type == "" {
			return nil, fmt.Errorf("%s block: provider tool %d requires a type", kind, i)
		}
		if taken[t.Type] {
			return nil, fmt.Errorf("%s block: provider tool %q collides with another tool of that name", kind, t.Type)
		}
		taken[t.Type] = true
		out = append(out, core.LLMProviderTool{Type: t.Type, Settings: t.Settings})
	}
	if err := c.serverTools.ValidateProviderTools(out); err != nil {
		return nil, fmt.Errorf("%s block: connector %q: %w", kind, connector, err)
	}
	return out, nil
}

// configureAgentProviderTools resolves the agent's provider tools against its
// connector and its complete function tool list, implicit tools included.
func configureAgentProviderTools(block *aiAgent, cfg agentSettings) error {
	serverTools, err := block.caller.providerTools(blockKindAIAgent, cfg.Connector, cfg.ProviderTools, block.tools)
	if err != nil {
		return err
	}
	block.serverTools = serverTools
	return nil
}
