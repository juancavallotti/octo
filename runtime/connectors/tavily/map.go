// This file provides the "tavily-map" block: it discovers a site's link graph
// from a root URL (POST /map) and returns the URLs it found, without extracting
// any content. It is crawl's cheap sibling — use it to decide what is worth
// crawling or extracting.
//
// Like crawl, map runs server-side for up to 150s, well past the tavily
// connector's 30s default timeout, so a flow that uses it must raise that
// setting.
package tavily

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerMap() {
	core.MustRegisterBlock("tavily-map", newMap)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "tavily-map",
		Label:    "Tavily Map",
		Category: core.CategoryProcessor,
		Description: "Map a site's link graph from a root URL through a tavily connector, " +
			"returning the discovered URLs.",
		Config: reflect.TypeFor[mapSettings](),
	})
}

// mapSettings is the tavily-map block's typed configuration: crawl's traversal
// half, without the extraction knobs, because map returns URLs only.
type mapSettings struct {
	// Name of the tavily connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:tavily"`
	// CEL expression for the root URL to map from.
	URL string `json:"url" octo:"label=URL,type=cel,required"`
	// CEL expression for a natural-language instruction steering the crawler.
	Instructions string `json:"instructions" octo:"label=Instructions,type=cel"`
	// How far from the root URL to explore (1-5).
	MaxDepth int `json:"maxDepth" octo:"label=Max depth"`
	// Links to follow per page level (1-500).
	MaxBreadth int `json:"maxBreadth" octo:"label=Max breadth"`
	// Total links processed before stopping.
	Limit int `json:"limit" octo:"label=Limit"`
	// CEL expression for a list of regexes selecting URL paths to visit.
	SelectPaths string `json:"selectPaths" octo:"label=Select paths,type=cel"`
	// CEL expression for a list of regexes excluding URL paths.
	ExcludePaths string `json:"excludePaths" octo:"label=Exclude paths,type=cel"`
	// CEL expression for a list of regexes selecting domains to visit.
	SelectDomains string `json:"selectDomains" octo:"label=Select domains,type=cel"`
	// CEL expression for a list of regexes excluding domains.
	ExcludeDomains string `json:"excludeDomains" octo:"label=Exclude domains,type=cel"`
	// Follow links that leave the root domain.
	AllowExternal *bool `json:"allowExternal" octo:"label=Allow external,default=true"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Tavily API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// mapProcessor discovers a site's URLs.
type mapProcessor struct {
	conn        *Connector
	traversal   *traversal
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newMap(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg mapSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("tavily-map: %w", err)
	}
	walk, err := compileTraversal(deps.Resources, "tavily-map", traversalConfig{
		URL:            cfg.URL,
		Instructions:   cfg.Instructions,
		SelectPaths:    cfg.SelectPaths,
		ExcludePaths:   cfg.ExcludePaths,
		SelectDomains:  cfg.SelectDomains,
		ExcludeDomains: cfg.ExcludeDomains,
		MaxDepth:       cfg.MaxDepth,
		MaxBreadth:     cfg.MaxBreadth,
		Limit:          cfg.Limit,
		AllowExternal:  cfg.AllowExternal,
	})
	if err != nil {
		return nil, err
	}
	return &mapProcessor{
		conn:        conn,
		traversal:   walk,
		resultVar:   cfg.ResultVar,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the traversal expressions and returns Tavily's map response.
func (p *mapProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	payload, err := p.traversal.payload(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("tavily-map %w", err)
	}

	resp, err := p.conn.Call(ctx, "map", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}
