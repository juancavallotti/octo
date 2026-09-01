// This file provides the "tavily-crawl" block: it walks a site from a root URL
// and returns each page's extracted content (POST /crawl), optionally steered by
// a natural-language instruction.
//
// Crawl runs server-side for up to 150s, well past the tavily connector's 30s
// default timeout, so a flow that uses it must raise that setting.
package tavily

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerCrawl() {
	core.MustRegisterBlock("tavily-crawl", newCrawl)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "tavily-crawl",
		Label:    "Tavily Crawl",
		Category: core.CategoryProcessor,
		Description: "Crawl a site from a root URL through a tavily connector and return each " +
			"page's extracted content.",
		Config: reflect.TypeFor[crawlSettings](),
	})
}

// crawlSettings is the tavily-crawl block's typed configuration. Its traversal
// half is mirrored by mapSettings; the two diverge only in that crawl also
// extracts content.
type crawlSettings struct {
	// Name of the tavily connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:tavily"`
	// CEL expression for the root URL to crawl from.
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
	// How hard to work at extraction on each crawled page.
	ExtractDepth string `json:"extractDepth" octo:"label=Extract depth,type=enum,enum=basic|advanced"`
	// Format of the extracted content.
	Format string `json:"format" octo:"label=Format,type=enum,enum=markdown|text"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Tavily API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// crawlProcessor walks a site and returns the extracted pages.
type crawlProcessor struct {
	conn        *Connector
	traversal   *traversal
	extract     map[string]any
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newCrawl(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg crawlSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("tavily-crawl: %w", err)
	}
	walk, err := compileTraversal(deps.Resources, "tavily-crawl", traversalConfig{
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

	extract := map[string]any{}
	putOptional(extract, "extract_depth", cfg.ExtractDepth)
	putOptional(extract, "format", cfg.Format)

	return &crawlProcessor{
		conn:        conn,
		traversal:   walk,
		extract:     extract,
		resultVar:   cfg.ResultVar,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the traversal expressions and returns Tavily's crawl response.
func (p *crawlProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	payload, err := p.traversal.payload(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("tavily-crawl %w", err)
	}
	for key, value := range p.extract {
		payload[key] = value
	}

	resp, err := p.conn.Call(ctx, "crawl", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}
