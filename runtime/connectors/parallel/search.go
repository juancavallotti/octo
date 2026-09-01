// This file provides the "parallel-search" block: it runs Parallel's synchronous
// search (POST /v1/search) and hands back the ranked pages with the LLM-optimized
// excerpts Parallel picked out of them.
//
// The shape is unlike a keyword search API: an objective says what the caller is
// actually after, and the queries are the searches to run toward it. Parallel
// ranks against the objective, which is why both are required.
package parallel

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
	core.MustRegisterBlock("parallel-search", newSearch)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "parallel-search",
		Label:    "Parallel Search",
		Category: core.CategoryProcessor,
		Description: "Search the web through a parallel connector, returning ranked pages with " +
			"LLM-optimized excerpts.",
		Config: reflect.TypeFor[searchSettings](),
	})
}

// searchSettings is the parallel-search block's typed configuration.
type searchSettings struct {
	// Name of the parallel connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:parallel"`
	// CEL expression describing, in natural language, what the search is for.
	// Parallel ranks results against this, not against the queries.
	Objective string `json:"objective" octo:"label=Objective,type=cel,required"`
	// CEL expression for the search queries to run: one string, or a list.
	SearchQueries string `json:"searchQueries" octo:"label=Search queries,type=cel,required"`
	// Latency/quality tradeoff: advanced ~3s, basic and fast are quicker, turbo
	// ~250ms.
	Mode string `json:"mode" octo:"label=Mode,type=enum,enum=advanced|basic|fast|turbo"`
	// Upper bound on the number of results (1-20; Parallel defaults to 10).
	MaxResults int `json:"maxResults" octo:"label=Max results"`
	// Cap on the characters returned per result.
	MaxCharsPerResult int `json:"maxCharsPerResult" octo:"label=Max chars per result"`
	// Cap on the characters returned across every result together. This is the
	// budget that matters when the results are headed for a model's context.
	MaxCharsTotal int `json:"maxCharsTotal" octo:"label=Max chars total"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Parallel API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// searchProcessor evaluates the objective and queries and searches.
type searchProcessor struct {
	conn        *Connector
	objective   *expr.Program
	queries     *expr.Program
	fixed       map[string]any
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSearch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg searchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("parallel-search: %w", err)
	}
	objective, err := compileRequired(deps.Resources, "parallel-search", "objective", cfg.Objective)
	if err != nil {
		return nil, err
	}
	queries, err := compileRequired(deps.Resources, "parallel-search", "searchQueries", cfg.SearchQueries)
	if err != nil {
		return nil, err
	}

	return &searchProcessor{
		conn:        conn,
		objective:   objective,
		queries:     queries,
		fixed:       searchPayload(cfg),
		resultVar:   cfg.ResultVar,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// searchPayload folds the message-independent settings into the request fields
// they map to, once at build time.
//
// Only mode and max_chars_total are top-level. The two result-shaping knobs live
// under advanced_settings, and the request schema is additionalProperties:false —
// so sending them at the top level is not merely ignored, it fails the whole call
// with extra_forbidden.
func searchPayload(cfg searchSettings) map[string]any {
	payload := map[string]any{}
	putOptional(payload, "mode", cfg.Mode)
	putOptional(payload, "max_chars_total", cfg.MaxCharsTotal)

	advanced := map[string]any{}
	putOptional(advanced, "max_results", cfg.MaxResults)
	if cfg.MaxCharsPerResult > 0 {
		advanced["excerpt_settings"] = map[string]any{"max_chars_per_result": cfg.MaxCharsPerResult}
	}
	if len(advanced) > 0 {
		payload["advanced_settings"] = advanced
	}
	return payload
}

// Process evaluates the objective and queries and returns Parallel's response.
func (p *searchProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	objective, err := p.objective.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("parallel-search objective: %w", err)
	}
	rawQueries, err := p.queries.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("parallel-search searchQueries: %w", err)
	}
	queries, err := toStringSlice(rawQueries)
	if err != nil {
		return nil, fmt.Errorf("parallel-search searchQueries %w", err)
	}

	payload := maps.Clone(p.fixed)
	payload["objective"] = objective
	payload["search_queries"] = queries

	resp, err := p.conn.Call(ctx, "v1/search", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}
