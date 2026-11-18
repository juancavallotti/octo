package openrouter

import (
	"fmt"
	"reflect"
	"strings"

	sdk "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/packages/param"

	"github.com/juancavallotti/octo/runtime/core"
)

const (
	// toolWebSearch is the server-side tool this connector runs, by its catalogue
	// type.
	toolWebSearch = "web_search"
	// wireWebSearch is OpenRouter's own name for it on the wire.
	wireWebSearch = "openrouter:web_search"
	// maxSearchResults is the most results OpenRouter returns per search.
	maxSearchResults = 25
)

// The values the enum settings accept, checked before the request carries them.
var (
	searchEngines = []string{"auto", "native", "exa", "firecrawl", "parallel", "perplexity"}
	contextSizes  = []string{"low", "medium", "high"}
)

// searchLocation is the approximate location a native search localizes its
// results to.
type searchLocation struct {
	// City name.
	City string `json:"city" octo:"label=City"`
	// Region or state.
	Region string `json:"region" octo:"label=Region"`
	// Two-letter ISO country code.
	Country string `json:"country" octo:"label=Country"`
	// IANA time zone, such as America/New_York.
	Timezone string `json:"timezone" octo:"label=Time zone"`
}

// webSearchSettings configures OpenRouter's web search, which runs for any routed
// model: natively where the upstream provider has a search of its own, and
// through a search engine OpenRouter calls otherwise.
type webSearchSettings struct {
	// Search backend: auto uses the model's native search where there is one.
	// Empty leaves it to OpenRouter.
	//nolint:lll // the enum tag lists every engine and is longer than 120 cols
	Engine string `json:"engine" octo:"label=Engine,type=enum,enum=auto|native|exa|firecrawl|parallel|perplexity"`
	// Results per search, 1 to 25. Empty leaves it to OpenRouter.
	MaxResults int `json:"maxResults" octo:"label=Max results"`
	// Most searches one request may run. Empty leaves it to OpenRouter.
	MaxUses int `json:"maxUses" octo:"label=Max uses"`
	// How much of each result reaches the model. Empty leaves it to OpenRouter.
	SearchContextSize string `json:"searchContextSize" octo:"label=Search context size,type=enum,enum=low|medium|high"`
	// Search only these domains.
	AllowedDomains []string `json:"allowedDomains" octo:"label=Allowed domains,type=string-list"`
	// Never search these domains.
	ExcludedDomains []string `json:"excludedDomains" octo:"label=Excluded domains,type=string-list"`
	// Approximate location to localize results to. Native search only.
	UserLocation searchLocation `json:"userLocation" octo:"label=User location,type=object"`
}

// providerToolMeta describes this connector's catalogue.
func providerToolMeta() []core.ProviderToolMeta {
	return []core.ProviderToolMeta{{
		Type:        toolWebSearch,
		Label:       "Web search",
		Description: "Searches the web for any routed model and cites what it found.",
		Settings:    reflect.TypeFor[webSearchSettings](),
	}}
}

// ValidateProviderTools satisfies core.LLMProviderTools by translating the tools
// exactly as a request would, so build time and call time cannot disagree.
func (c *Connector) ValidateProviderTools(tools []core.LLMProviderTool) error {
	_, err := toServerTools(tools)
	return err
}

// toServerTools translates the request's provider tools to tool entries. The SDK
// models only function tools, so each is sent as the JSON OpenRouter expects.
func toServerTools(tools []core.LLMProviderTool) ([]sdk.ChatCompletionToolUnionParam, error) {
	out := make([]sdk.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		if t.Type != toolWebSearch {
			return nil, fmt.Errorf("provider tool %q: not one of: %s", t.Type, toolWebSearch)
		}
		var set webSearchSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return nil, fmt.Errorf("provider tool %q: %w", t.Type, err)
		}
		entry, err := webSearchEntry(set)
		if err != nil {
			return nil, fmt.Errorf("provider tool %q: %w", t.Type, err)
		}
		out = append(out, param.Override[sdk.ChatCompletionToolUnionParam](entry))
	}
	return out, nil
}

// webSearchEntry renders the settings as OpenRouter's tool entry: the type, and
// whichever parameters were set.
func webSearchEntry(set webSearchSettings) (map[string]any, error) {
	if err := checkWebSearch(set); err != nil {
		return nil, err
	}
	params := map[string]any{}
	putString(params, "engine", set.Engine)
	putString(params, "search_context_size", set.SearchContextSize)
	putInt(params, "max_results", set.MaxResults)
	putInt(params, "max_uses", set.MaxUses)
	if len(set.AllowedDomains) > 0 {
		params["allowed_domains"] = set.AllowedDomains
	}
	if len(set.ExcludedDomains) > 0 {
		params["excluded_domains"] = set.ExcludedDomains
	}
	if loc := set.UserLocation; loc != (searchLocation{}) {
		location := map[string]any{"type": "approximate"}
		putString(location, "city", loc.City)
		putString(location, "region", loc.Region)
		putString(location, "country", loc.Country)
		putString(location, "timezone", loc.Timezone)
		params["user_location"] = location
	}
	entry := map[string]any{"type": wireWebSearch}
	if len(params) > 0 {
		entry["parameters"] = params
	}
	return entry, nil
}

// checkWebSearch rejects the settings OpenRouter refuses.
func checkWebSearch(set webSearchSettings) error {
	if err := oneOf("engine", set.Engine, searchEngines); err != nil {
		return err
	}
	if err := oneOf("searchContextSize", set.SearchContextSize, contextSizes); err != nil {
		return err
	}
	switch {
	case set.MaxResults < 0 || set.MaxResults > maxSearchResults:
		return fmt.Errorf("maxResults must be between 1 and %d, got %d", maxSearchResults, set.MaxResults)
	case set.MaxUses < 0:
		return fmt.Errorf("maxUses must not be negative, got %d", set.MaxUses)
	}
	return nil
}

// oneOf rejects a non-empty enum setting outside its allowed values.
func oneOf(name, value string, allowed []string) error {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, got %q", name, strings.Join(allowed, ", "), value)
}

// putString sets key when value is non-empty.
func putString(m map[string]any, key, value string) {
	if value != "" {
		m[key] = value
	}
}

// putInt sets key when value is positive.
func putInt(m map[string]any, key string, value int) {
	if value > 0 {
		m[key] = value
	}
}
