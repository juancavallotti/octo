package anthropic

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/juancavallotti/octo/runtime/core"
)

// The server-side tools this connector runs, by their catalogue type.
const (
	toolWebSearch     = "web_search"
	toolWebFetch      = "web_fetch"
	toolCodeExecution = "code_execution"
)

// The tool variants a web tool's version setting selects between.
const (
	// versionLatest is the dynamic-filtering variant, which provisions code
	// execution itself to filter what it read. It needs a Claude 4.6 or later model.
	versionLatest = "latest"
	// versionBasic is the original variant, for older models and for platforms
	// that offer nothing newer.
	versionBasic = "basic"
)

// userLocation is the approximate location a web search localizes its results to.
type userLocation struct {
	// City name.
	City string `json:"city" octo:"label=City"`
	// Region or state.
	Region string `json:"region" octo:"label=Region"`
	// Two-letter ISO country code.
	Country string `json:"country" octo:"label=Country"`
	// IANA time zone, such as America/New_York.
	Timezone string `json:"timezone" octo:"label=Time zone"`
}

// webSearchSettings configures the web_search tool.
type webSearchSettings struct {
	// Tool variant: latest filters results with code execution and needs a Claude
	// 4.6 or later model; basic suits older models and Vertex AI.
	Version string `json:"version" octo:"label=Version,type=enum,enum=latest|basic,default=latest"`
	// Most searches one turn may run. Empty leaves it to the provider.
	MaxUses int `json:"maxUses" octo:"label=Max uses"`
	// Search only these domains. Cannot be combined with blocked domains.
	AllowedDomains []string `json:"allowedDomains" octo:"label=Allowed domains,type=string-list"`
	// Never search these domains. Cannot be combined with allowed domains.
	BlockedDomains []string `json:"blockedDomains" octo:"label=Blocked domains,type=string-list"`
	// Approximate location to localize results to.
	UserLocation userLocation `json:"userLocation" octo:"label=User location,type=object"`
}

// webFetchSettings configures the web_fetch tool. It fetches only URLs already
// present in the conversation.
type webFetchSettings struct {
	// Tool variant: latest filters fetched content with code execution and needs a
	// Claude 4.6 or later model; basic suits older models.
	Version string `json:"version" octo:"label=Version,type=enum,enum=latest|basic,default=latest"`
	// Most fetches one turn may run. Empty leaves it to the provider.
	MaxUses int `json:"maxUses" octo:"label=Max uses"`
	// Fetch only from these domains. Cannot be combined with blocked domains.
	AllowedDomains []string `json:"allowedDomains" octo:"label=Allowed domains,type=string-list"`
	// Never fetch from these domains. Cannot be combined with allowed domains.
	BlockedDomains []string `json:"blockedDomains" octo:"label=Blocked domains,type=string-list"`
	// Cite the fetched documents in the answer.
	Citations bool `json:"citations" octo:"label=Citations"`
	// Cap on the tokens one fetched document may contribute. Empty leaves it to the provider.
	MaxContentTokens int `json:"maxContentTokens" octo:"label=Max content tokens"`
}

// codeExecutionSettings configures the code_execution tool, which takes no settings.
type codeExecutionSettings struct{}

// providerToolMeta describes this connector's catalogue, in catalogue order.
func providerToolMeta() []core.ProviderToolMeta {
	return []core.ProviderToolMeta{
		{
			Type:        toolWebSearch,
			Label:       "Web search",
			Description: "Searches the web and cites what it found.",
			Settings:    reflect.TypeFor[webSearchSettings](),
		},
		{
			Type:        toolWebFetch,
			Label:       "Web fetch",
			Description: "Reads the full content of a URL already in the conversation.",
			Settings:    reflect.TypeFor[webFetchSettings](),
		},
		{
			Type:        toolCodeExecution,
			Label:       "Code execution",
			Description: "Runs code in a sandbox the provider hosts.",
		},
	}
}

// ValidateProviderTools satisfies core.LLMProviderTools by translating the tools
// exactly as a request would, so build time and call time cannot disagree.
func (c *Connector) ValidateProviderTools(tools []core.LLMProviderTool) error {
	_, err := toServerTools(tools)
	return err
}

// toServerTools translates the request's provider tools to SDK tool params.
func toServerTools(tools []core.LLMProviderTool) ([]sdk.ToolUnionParam, error) {
	out := make([]sdk.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		tool, err := toServerTool(t)
		if err != nil {
			return nil, fmt.Errorf("provider tool %q: %w", t.Type, err)
		}
		out = append(out, tool)
	}
	return out, nil
}

// toServerTool translates one provider tool.
func toServerTool(t core.LLMProviderTool) (sdk.ToolUnionParam, error) {
	switch t.Type {
	case toolWebSearch:
		var set webSearchSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return sdk.ToolUnionParam{}, err
		}
		return webSearchTool(set)
	case toolWebFetch:
		var set webFetchSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return sdk.ToolUnionParam{}, err
		}
		return webFetchTool(set)
	case toolCodeExecution:
		var set codeExecutionSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return sdk.ToolUnionParam{}, err
		}
		return sdk.ToolUnionParam{OfCodeExecutionTool20260120: &sdk.CodeExecutionTool20260120Param{}}, nil
	default:
		return sdk.ToolUnionParam{}, fmt.Errorf("not one of: %s",
			strings.Join([]string{toolWebSearch, toolWebFetch, toolCodeExecution}, ", "))
	}
}

// webSearchTool builds the web_search variant the settings name.
func webSearchTool(set webSearchSettings) (sdk.ToolUnionParam, error) {
	if err := checkWebLimits(set.Version, set.MaxUses, set.AllowedDomains, set.BlockedDomains); err != nil {
		return sdk.ToolUnionParam{}, err
	}
	location := toUserLocation(set.UserLocation)
	if set.Version == versionBasic {
		return sdk.ToolUnionParam{OfWebSearchTool20250305: &sdk.WebSearchTool20250305Param{
			MaxUses:        optInt(set.MaxUses),
			AllowedDomains: set.AllowedDomains,
			BlockedDomains: set.BlockedDomains,
			UserLocation:   location,
		}}, nil
	}
	return sdk.ToolUnionParam{OfWebSearchTool20260209: &sdk.WebSearchTool20260209Param{
		MaxUses:        optInt(set.MaxUses),
		AllowedDomains: set.AllowedDomains,
		BlockedDomains: set.BlockedDomains,
		UserLocation:   location,
	}}, nil
}

// webFetchTool builds the web_fetch variant the settings name.
func webFetchTool(set webFetchSettings) (sdk.ToolUnionParam, error) {
	if err := checkWebLimits(set.Version, set.MaxUses, set.AllowedDomains, set.BlockedDomains); err != nil {
		return sdk.ToolUnionParam{}, err
	}
	if set.MaxContentTokens < 0 {
		return sdk.ToolUnionParam{}, fmt.Errorf("maxContentTokens must not be negative, got %d", set.MaxContentTokens)
	}
	var citations sdk.CitationsConfigParam
	if set.Citations {
		citations.Enabled = param.NewOpt(true)
	}
	if set.Version == versionBasic {
		return sdk.ToolUnionParam{OfWebFetchTool20250910: &sdk.WebFetchTool20250910Param{
			MaxUses:          optInt(set.MaxUses),
			MaxContentTokens: optInt(set.MaxContentTokens),
			AllowedDomains:   set.AllowedDomains,
			BlockedDomains:   set.BlockedDomains,
			Citations:        citations,
		}}, nil
	}
	return sdk.ToolUnionParam{OfWebFetchTool20260209: &sdk.WebFetchTool20260209Param{
		MaxUses:          optInt(set.MaxUses),
		MaxContentTokens: optInt(set.MaxContentTokens),
		AllowedDomains:   set.AllowedDomains,
		BlockedDomains:   set.BlockedDomains,
		Citations:        citations,
	}}, nil
}

// checkWebLimits rejects the settings both web tools share and the API refuses:
// an unknown variant, a negative cap, and both domain lists at once.
func checkWebLimits(version string, maxUses int, allowed, blocked []string) error {
	switch {
	case version != "" && version != versionLatest && version != versionBasic:
		return fmt.Errorf("version must be %q or %q, got %q", versionLatest, versionBasic, version)
	case maxUses < 0:
		return fmt.Errorf("maxUses must not be negative, got %d", maxUses)
	case len(allowed) > 0 && len(blocked) > 0:
		return errors.New("allowedDomains and blockedDomains cannot both be set")
	}
	return nil
}

// toUserLocation converts the location setting, leaving it unset when every
// field is empty so the request carries none.
func toUserLocation(loc userLocation) sdk.UserLocationParam {
	var out sdk.UserLocationParam
	if loc == (userLocation{}) {
		return out
	}
	out.City = optString(loc.City)
	out.Region = optString(loc.Region)
	out.Country = optString(loc.Country)
	out.Timezone = optString(loc.Timezone)
	return out
}

// optInt is a positive setting as an SDK option, unset for zero.
func optInt(v int) param.Opt[int64] {
	if v <= 0 {
		return param.Opt[int64]{}
	}
	return param.NewOpt(int64(v))
}

// optString is a non-empty setting as an SDK option, unset for "".
func optString(v string) param.Opt[string] {
	if v == "" {
		return param.Opt[string]{}
	}
	return param.NewOpt(v)
}
