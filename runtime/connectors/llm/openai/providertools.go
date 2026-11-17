package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/responses"

	"github.com/juancavallotti/octo/runtime/core"
)

// The server-side tools this connector runs, by the type a block names them with.
const (
	toolWebSearch       = "web_search"
	toolFileSearch      = "file_search"
	toolCodeInterpreter = "code_interpreter"
	toolImageGeneration = "image_generation"
)

// webLocationType is the only kind of location a web search accepts.
const webLocationType = "approximate"

// enumAuto is the image setting value that leaves the choice to the model.
const enumAuto = "auto"

// serverToolItems are the output item types the server ran on the turn's behalf.
// A turn carrying one is kept verbatim, because the portable fields have nowhere
// to put the call or what it found.
var serverToolItems = map[string]bool{
	"web_search_call":       true,
	"file_search_call":      true,
	"code_interpreter_call": true,
}

// webLocation is the approximate location a web search localizes its results to.
type webLocation struct {
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
	// How much context the search retrieves: more is better grounded and slower.
	// Empty leaves it to the provider.
	SearchContextSize string `json:"searchContextSize" octo:"label=Search context size,type=enum,enum=low|medium|high"`
	// Search only these domains.
	AllowedDomains []string `json:"allowedDomains" octo:"label=Allowed domains,type=string-list"`
	// Approximate location to localize results to.
	UserLocation webLocation `json:"userLocation" octo:"label=User location,type=object"`
}

// fileSearchSettings configures the file_search tool.
type fileSearchSettings struct {
	// Vector stores to search. At least one is required.
	VectorStoreIDs []string `json:"vectorStoreIds" octo:"label=Vector store IDs,type=string-list,required"`
	// Most results one search returns. Empty leaves it to the provider.
	MaxNumResults int `json:"maxNumResults" octo:"label=Max results"`
}

// codeInterpreterSettings configures the code_interpreter tool, which runs in a
// container the provider creates for the request.
type codeInterpreterSettings struct {
	// Uploaded files to make available in the container.
	FileIDs []string `json:"fileIds" octo:"label=File IDs,type=string-list"`
}

// imageGenerationSettings configures the image_generation tool. The images it
// produces are written to the block's response media variable.
type imageGenerationSettings struct {
	// Image dimensions. Empty leaves it to the provider.
	Size string `json:"size" octo:"label=Size,type=enum,enum=auto|1024x1024|1024x1536|1536x1024"`
	// Rendering quality. Empty leaves it to the provider.
	Quality string `json:"quality" octo:"label=Quality,type=enum,enum=auto|low|medium|high"`
	// Background treatment. Empty leaves it to the provider.
	Background string `json:"background" octo:"label=Background,type=enum,enum=auto|transparent|opaque"`
	// File format of the produced image. Empty leaves it to the provider.
	OutputFormat string `json:"outputFormat" octo:"label=Output format,type=enum,enum=png|jpeg|webp"`
}

// The values the enum settings accept, checked before the request carries them.
var (
	contextSizes  = []string{"low", "medium", "high"}
	imageSizes    = []string{enumAuto, "1024x1024", "1024x1536", "1536x1024"}
	imageQuality  = []string{enumAuto, "low", "medium", "high"}
	imageBacks    = []string{enumAuto, "transparent", "opaque"}
	imageFormats  = []string{"png", "jpeg", "webp"}
	providerTypes = []string{toolWebSearch, toolFileSearch, toolCodeInterpreter, toolImageGeneration}
)

// providerToolMeta is the catalogue this connector advertises to the editor, in
// the order it offers the tools.
func providerToolMeta() []core.ProviderToolMeta {
	return []core.ProviderToolMeta{
		{
			Type:        toolWebSearch,
			Label:       "Web search",
			Description: "Searches the web and cites what it found.",
			Settings:    reflect.TypeFor[webSearchSettings](),
		},
		{
			Type:        toolFileSearch,
			Label:       "File search",
			Description: "Searches the documents in OpenAI vector stores.",
			Settings:    reflect.TypeFor[fileSearchSettings](),
		},
		{
			Type:        toolCodeInterpreter,
			Label:       "Code interpreter",
			Description: "Runs Python in a sandbox the provider hosts.",
			Settings:    reflect.TypeFor[codeInterpreterSettings](),
		},
		{
			Type:        toolImageGeneration,
			Label:       "Image generation",
			Description: "Generates images, written to the block's response media variable.",
			Settings:    reflect.TypeFor[imageGenerationSettings](),
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
func toServerTools(tools []core.LLMProviderTool) ([]responses.ToolUnionParam, error) {
	out := make([]responses.ToolUnionParam, 0, len(tools))
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
func toServerTool(t core.LLMProviderTool) (responses.ToolUnionParam, error) {
	switch t.Type {
	case toolWebSearch:
		var set webSearchSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return responses.ToolUnionParam{}, err
		}
		return webSearchTool(set)
	case toolFileSearch:
		var set fileSearchSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return responses.ToolUnionParam{}, err
		}
		return fileSearchTool(set)
	case toolCodeInterpreter:
		var set codeInterpreterSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return responses.ToolUnionParam{}, err
		}
		return responses.ToolUnionParam{OfCodeInterpreter: &responses.ToolCodeInterpreterParam{
			Container: responses.ToolCodeInterpreterContainerUnionParam{
				OfCodeInterpreterContainerAuto: &responses.ToolCodeInterpreterContainerCodeInterpreterContainerAutoParam{
					FileIDs: set.FileIDs,
				},
			},
		}}, nil
	case toolImageGeneration:
		var set imageGenerationSettings
		if err := t.Settings.DecodeStrict(&set); err != nil {
			return responses.ToolUnionParam{}, err
		}
		return imageGenerationTool(set)
	default:
		return responses.ToolUnionParam{}, fmt.Errorf("not one of: %s", strings.Join(providerTypes, ", "))
	}
}

// webSearchTool builds the web_search tool.
func webSearchTool(set webSearchSettings) (responses.ToolUnionParam, error) {
	if err := oneOf("searchContextSize", set.SearchContextSize, contextSizes); err != nil {
		return responses.ToolUnionParam{}, err
	}
	tool := responses.WebSearchToolParam{
		Type:              responses.WebSearchToolTypeWebSearch,
		SearchContextSize: responses.WebSearchToolSearchContextSize(set.SearchContextSize),
		Filters:           responses.WebSearchToolFiltersParam{AllowedDomains: set.AllowedDomains},
	}
	if loc := set.UserLocation; loc != (webLocation{}) {
		tool.UserLocation = responses.WebSearchToolUserLocationParam{
			Type:     webLocationType,
			City:     optString(loc.City),
			Region:   optString(loc.Region),
			Country:  optString(loc.Country),
			Timezone: optString(loc.Timezone),
		}
	}
	return responses.ToolUnionParam{OfWebSearch: &tool}, nil
}

// fileSearchTool builds the file_search tool.
func fileSearchTool(set fileSearchSettings) (responses.ToolUnionParam, error) {
	if len(set.VectorStoreIDs) == 0 {
		return responses.ToolUnionParam{}, errors.New("vectorStoreIds requires at least one vector store")
	}
	if set.MaxNumResults < 0 {
		return responses.ToolUnionParam{}, fmt.Errorf("maxNumResults must not be negative, got %d", set.MaxNumResults)
	}
	tool := responses.FileSearchToolParam{VectorStoreIDs: set.VectorStoreIDs}
	if set.MaxNumResults > 0 {
		tool.MaxNumResults = param.NewOpt(int64(set.MaxNumResults))
	}
	return responses.ToolUnionParam{OfFileSearch: &tool}, nil
}

// imageGenerationTool builds the image_generation tool.
func imageGenerationTool(set imageGenerationSettings) (responses.ToolUnionParam, error) {
	for _, check := range []struct {
		name, value string
		allowed     []string
	}{
		{"size", set.Size, imageSizes},
		{"quality", set.Quality, imageQuality},
		{"background", set.Background, imageBacks},
		{"outputFormat", set.OutputFormat, imageFormats},
	} {
		if err := oneOf(check.name, check.value, check.allowed); err != nil {
			return responses.ToolUnionParam{}, err
		}
	}
	return responses.ToolUnionParam{OfImageGeneration: &responses.ToolImageGenerationParam{
		Size:         set.Size,
		Quality:      set.Quality,
		Background:   set.Background,
		OutputFormat: set.OutputFormat,
	}}, nil
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

// optString is a non-empty setting as an SDK option, unset for "".
func optString(v string) param.Opt[string] {
	if v == "" {
		return param.Opt[string]{}
	}
	return param.NewOpt(v)
}

// opaqueTurn returns the turn's output items verbatim, as the server sent them,
// when it carries a server tool call, and nil otherwise. Responses accepts a
// turn's output items back as input unchanged, which is how they are echoed.
//
// Generated images are left out, as they are from the portable fields: no turn
// needs them echoed, and carrying them would re-send the bytes for the rest of the
// conversation.
func opaqueTurn(output []responses.ResponseOutputItemUnion) *core.LLMOpaqueTurn {
	needed := false
	for _, item := range output {
		if serverToolItems[item.Type] {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	items := make([]json.RawMessage, 0, len(output))
	for _, item := range output {
		if item.Type == "image_generation_call" {
			continue
		}
		raw := item.RawJSON()
		if raw == "" {
			slog.Warn("llm-openai cannot echo an output item without its wire form", "type", item.Type)
			return nil
		}
		items = append(items, json.RawMessage(raw))
	}
	raw, err := json.Marshal(items)
	if err != nil {
		slog.Warn("llm-openai cannot encode a server tool turn", "error", err)
		return nil
	}
	return &core.LLMOpaqueTurn{Provider: core.ProviderOpenAI, Content: raw}
}

// opaqueItems decodes an opaque turn this connector wrote back into the input
// items it echoes, each sent exactly as it was received.
func opaqueItems(raw json.RawMessage) ([]responses.ResponseInputItemUnionParam, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("llm-openai: decode opaque assistant turn: %w", err)
	}
	out := make([]responses.ResponseInputItemUnionParam, len(items))
	for i, item := range items {
		out[i] = param.Override[responses.ResponseInputItemUnionParam](item)
	}
	return out, nil
}
