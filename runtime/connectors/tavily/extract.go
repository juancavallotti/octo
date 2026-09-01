// This file provides the "tavily-extract" block: it pulls clean, LLM-ready
// content out of URLs you already have (POST /extract), which is the half of
// research that search does not cover — following a link the flow was handed.
//
// Tavily answers with results and failed_results side by side, so a partial
// failure is a 200. The block surfaces that rather than letting it pass as a
// clean success.
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

func registerExtract() {
	core.MustRegisterBlock("tavily-extract", newExtract)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "tavily-extract",
		Label:    "Tavily Extract",
		Category: core.CategoryProcessor,
		Description: "Extract clean page content from one or more URLs through a tavily " +
			"connector.",
		Config: reflect.TypeFor[extractSettings](),
	})
}

// extractSettings is the tavily-extract block's typed configuration.
type extractSettings struct {
	// Name of the tavily connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:tavily"`
	// CEL expression for the URLs to extract: a single URL, or a list of them.
	URLs string `json:"urls" octo:"label=URLs,type=cel,required"`
	// CEL expression for the intent used to rerank the extracted chunks.
	Query string `json:"query" octo:"label=Query,type=cel"`
	// How hard to work at extraction; advanced costs more and retrieves more.
	ExtractDepth string `json:"extractDepth" octo:"label=Extract depth,type=enum,enum=basic|advanced"`
	// Format of the extracted content.
	Format string `json:"format" octo:"label=Format,type=enum,enum=markdown|text"`
	// Maximum content chunks taken from each source (1-5).
	ChunksPerSource int `json:"chunksPerSource" octo:"label=Chunks per source"`
	// Include the image URLs found on each page.
	IncludeImages bool `json:"includeImages" octo:"label=Include images"`
	// Fail the flow when Tavily could not extract every URL it was given.
	FailOnPartial bool `json:"failOnPartial" octo:"label=Fail on partial extraction"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Tavily API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// extractProcessor evaluates the URL list (and optional query) and extracts.
type extractProcessor struct {
	conn          *Connector
	urls          *expr.Program
	query         *expr.Program
	fixed         map[string]any
	failOnPartial bool
	resultVar     string
	failOnError   bool
	env           map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newExtract(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg extractSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("tavily-extract: %w", err)
	}
	urls, err := compileList(deps.Resources, "tavily-extract", "urls", cfg.URLs)
	if err != nil {
		return nil, err
	}
	if urls == nil {
		return nil, fmt.Errorf("tavily-extract requires a %q expression", "urls")
	}
	query, err := compileOptional(deps.Resources, cfg.Query)
	if err != nil {
		return nil, fmt.Errorf("tavily-extract: compile query: %w", err)
	}

	fixed := map[string]any{}
	putOptional(fixed, "extract_depth", cfg.ExtractDepth)
	putOptional(fixed, "format", cfg.Format)
	putOptional(fixed, "chunks_per_source", cfg.ChunksPerSource)
	putOptional(fixed, "include_images", cfg.IncludeImages)

	return &extractProcessor{
		conn:          conn,
		urls:          urls,
		query:         query,
		fixed:         fixed,
		failOnPartial: cfg.FailOnPartial,
		resultVar:     cfg.ResultVar,
		failOnError:   failOnErrorDefault(cfg.FailOnError),
		env:           expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the URLs and returns Tavily's extraction response.
func (p *extractProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	payload := maps.Clone(p.fixed)
	if err := putList(payload, "urls", p.urls, activation); err != nil {
		return nil, fmt.Errorf("tavily-extract %w", err)
	}
	if _, ok := payload["urls"]; !ok {
		return nil, fmt.Errorf("tavily-extract urls: evaluated to no URLs")
	}
	if err := putString(payload, "query", p.query, activation); err != nil {
		return nil, fmt.Errorf("tavily-extract %w", err)
	}

	resp, err := p.conn.Call(ctx, "extract", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	if err := p.checkPartial(resp); err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}

// checkPartial turns Tavily's failed_results into an error when the block was
// configured to insist on a complete extraction. Tavily reports per-URL failures
// inside a 200, so without this a flow reading results would silently work from
// fewer pages than it asked for.
func (p *extractProcessor) checkPartial(resp map[string]any) error {
	if !p.failOnPartial {
		return nil
	}
	failed, _ := resp["failed_results"].([]any)
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("tavily-extract: %d of %d URLs could not be extracted",
		len(failed), len(failed)+resultCount(resp))
}

// resultCount reports how many results a Tavily response carried.
func resultCount(resp map[string]any) int {
	results, _ := resp["results"].([]any)
	return len(results)
}
