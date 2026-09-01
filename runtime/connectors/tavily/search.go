// This file provides the "tavily-search" block: it runs an agentic web search
// through a tavily connector (POST /search) and hands the whole response back —
// the ranked results, and Tavily's synthesized answer when one was asked for.
package tavily

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerSearch() {
	core.MustRegisterBlock("tavily-search", newSearch)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "tavily-search",
		Label:    "Tavily Search",
		Category: core.CategoryProcessor,
		Description: "Search the live web through a tavily connector and return the ranked " +
			"results, optionally with a synthesized answer.",
		Config: reflect.TypeFor[searchSettings](),
	})
}

// optionNone is the enum value every "how much extra do you want" setting uses to
// mean "leave the field out of the request".
//
// None of those enums carries a default= clause: an unset one is omitted from the
// request so Tavily's own default applies, rather than this connector pinning a
// value it does not own.
const optionNone = "none"

// searchSettings is the tavily-search block's typed configuration.
type searchSettings struct {
	// Name of the tavily connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:tavily"`
	// CEL expression for the search query.
	Query string `json:"query" octo:"label=Query,type=cel,required"`
	// Trades latency for relevance. advanced costs two credits; the rest cost one.
	SearchDepth string `json:"searchDepth" octo:"label=Search depth,type=enum,enum=basic|advanced|fast|ultra-fast"`
	// Search category: general web, or the news index.
	Topic string `json:"topic" octo:"label=Topic,type=enum,enum=general|news,default=general"`
	// Number of results to return (0-20).
	MaxResults int `json:"maxResults" octo:"label=Max results,default=5"`
	// Maximum content chunks taken from each source (1-3).
	ChunksPerSource int `json:"chunksPerSource" octo:"label=Chunks per source"`
	// Ask Tavily to synthesize an answer from the results and return it alongside them.
	IncludeAnswer string `json:"includeAnswer" octo:"label=Include answer,type=enum,enum=none|basic|advanced"`
	// Include each result's full cleaned page content, in the chosen format.
	IncludeRawContent string `json:"includeRawContent" octo:"label=Include raw content,type=enum,enum=none|markdown|text"`
	// CEL expression for a list of domains to restrict the search to.
	IncludeDomains string `json:"includeDomains" octo:"label=Include domains,type=cel"`
	// CEL expression for a list of domains to exclude from the search.
	ExcludeDomains string `json:"excludeDomains" octo:"label=Exclude domains,type=cel"`
	// Country to boost results from, as Tavily's lowercase English name — "united
	// states", "uruguay" — not an ISO code. General topic only.
	Country string `json:"country" octo:"label=Country"`
	// Language to boost results in, as an ISO 639-1 code ("en", "fr", "zh-cn").
	Language string `json:"language" octo:"label=Language"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Tavily API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// searchProcessor evaluates the query (and optional domain filters) and searches.
type searchProcessor struct {
	conn           *Connector
	query          *expr.Program
	includeDomains *expr.Program
	excludeDomains *expr.Program
	fixed          map[string]any
	resultVar      string
	failOnError    bool
	env            map[string]any
}

// newSearch builds a search processor, resolving its tavily connector and
// compiling the CEL expressions once so a bad reference or expression fails at
// startup rather than on the first message.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSearch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg searchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("tavily-search: %w", err)
	}
	query, err := compileRequired(deps.Resources, "tavily-search", "query", cfg.Query)
	if err != nil {
		return nil, err
	}
	includeDomains, err := compileList(deps.Resources, "tavily-search", "includeDomains", cfg.IncludeDomains)
	if err != nil {
		return nil, err
	}
	excludeDomains, err := compileList(deps.Resources, "tavily-search", "excludeDomains", cfg.ExcludeDomains)
	if err != nil {
		return nil, err
	}
	return &searchProcessor{
		conn:           conn,
		query:          query,
		includeDomains: includeDomains,
		excludeDomains: excludeDomains,
		fixed:          searchPayload(cfg),
		resultVar:      cfg.ResultVar,
		failOnError:    failOnErrorDefault(cfg.FailOnError),
		env:            expr.EnvActivation(deps.Env),
	}, nil
}

// searchPayload folds the message-independent settings into the request fields
// they map to, once at build time. Every field is omitted when unset so Tavily's
// own default applies rather than Go's zero value.
func searchPayload(cfg searchSettings) map[string]any {
	payload := map[string]any{}
	putOptional(payload, "search_depth", cfg.SearchDepth)
	putOptional(payload, "topic", cfg.Topic)
	putOptional(payload, "max_results", cfg.MaxResults)
	putOptional(payload, "chunks_per_source", cfg.ChunksPerSource)
	putOptional(payload, "country", cfg.Country)
	putOptional(payload, "language", cfg.Language)
	if cfg.IncludeAnswer != "" && cfg.IncludeAnswer != optionNone {
		payload["include_answer"] = cfg.IncludeAnswer
	}
	if cfg.IncludeRawContent != "" && cfg.IncludeRawContent != optionNone {
		payload["include_raw_content"] = cfg.IncludeRawContent
	}
	return payload
}

// Process evaluates the query and domain filters and returns Tavily's response.
func (p *searchProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	query, err := p.query.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("tavily-search query: %w", err)
	}

	payload := maps.Clone(p.fixed)
	payload["query"] = query
	if err := putList(payload, "include_domains", p.includeDomains, activation); err != nil {
		return nil, fmt.Errorf("tavily-search %w", err)
	}
	if err := putList(payload, "exclude_domains", p.excludeDomains, activation); err != nil {
		return nil, fmt.Errorf("tavily-search %w", err)
	}

	resp, err := p.conn.Call(ctx, "search", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}
