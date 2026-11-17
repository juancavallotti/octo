package gemini

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"google.golang.org/genai"

	"github.com/juancavallotti/octo/runtime/core"
)

// The server-side tools this connector runs, by their catalogue type.
// None takes settings.
const (
	toolGoogleSearch  = "google_search"
	toolCodeExecution = "code_execution"
	toolURLContext    = "url_context"
)

// providerTypes is the catalogue's types, in catalogue order.
var providerTypes = []string{toolGoogleSearch, toolCodeExecution, toolURLContext}

// noSettings is what a tool without settings decodes into, so a stray key fails.
type noSettings struct{}

// providerToolMeta describes this connector's catalogue.
func providerToolMeta() []core.ProviderToolMeta {
	return []core.ProviderToolMeta{
		{
			Type:        toolGoogleSearch,
			Label:       "Google Search",
			Description: "Grounds the answer in Google Search results.",
		},
		{
			Type:        toolCodeExecution,
			Label:       "Code execution",
			Description: "Runs Python in a sandbox the provider hosts.",
		},
		{
			Type:        toolURLContext,
			Label:       "URL context",
			Description: "Reads the URLs mentioned in the conversation.",
		},
	}
}

// ValidateProviderTools satisfies core.LLMProviderTools by translating the tools
// exactly as a request would, so build time and call time cannot disagree.
func (c *Connector) ValidateProviderTools(tools []core.LLMProviderTool) error {
	_, err := toServerTools(tools)
	return err
}

// toServerTools translates the request's provider tools, one genai.Tool each.
func toServerTools(tools []core.LLMProviderTool) ([]*genai.Tool, error) {
	out := make([]*genai.Tool, 0, len(tools))
	for _, t := range tools {
		var set noSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return nil, fmt.Errorf("provider tool %q: %w", t.Type, err)
		}
		switch t.Type {
		case toolGoogleSearch:
			out = append(out, &genai.Tool{GoogleSearch: &genai.GoogleSearch{}})
		case toolCodeExecution:
			out = append(out, &genai.Tool{CodeExecution: &genai.ToolCodeExecution{}})
		case toolURLContext:
			out = append(out, &genai.Tool{URLContext: &genai.URLContext{}})
		default:
			return nil, fmt.Errorf("provider tool %q: not one of: %s", t.Type, strings.Join(providerTypes, ", "))
		}
	}
	return out, nil
}

// serverToolPart reports whether a part records work the server ran on the
// turn's behalf: code it wrote and executed, and what that code printed.
func serverToolPart(p *genai.Part) bool {
	return p.ExecutableCode != nil || p.CodeExecutionResult != nil
}

// opaqueTurn returns the turn's parts verbatim when one of them is a server tool
// part the portable fields cannot express, and nil otherwise.
func opaqueTurn(parts []*genai.Part) *core.LLMOpaqueTurn {
	needed := false
	for _, p := range parts {
		if serverToolPart(p) {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		slog.Warn("llm-gemini cannot encode a server tool turn", "error", err)
		return nil
	}
	return &core.LLMOpaqueTurn{Provider: core.ProviderGoogle, Content: raw}
}

// opaqueParts decodes an opaque turn this connector wrote back into the parts it
// echoes.
func opaqueParts(raw json.RawMessage) ([]*genai.Part, error) {
	var parts []*genai.Part
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("llm-gemini: decode opaque assistant turn: %w", err)
	}
	return parts, nil
}
