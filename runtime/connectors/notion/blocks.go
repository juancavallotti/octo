// This file provides the "notion-retrieve-blocks" block: it retrieves a page's
// (or any block's) child blocks (GET /blocks/{id}/children), following pagination,
// and stores them as a {results: [...]} object. That shape matches the Notion API
// and feeds notion-page-to-markdown directly (its default source is body.results),
// so a flow can fetch a page's content and render it to markdown.
package notion

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("notion-retrieve-blocks", newRetrieveBlocks)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-retrieve-blocks",
		Label:    "Notion Retrieve Blocks",
		Category: "processor",
		Description: "Retrieve a page's (or block's) child blocks (GET /blocks/{id}/children, " +
			"paginated) as a {results: [...]} object, ready for Notion Page to Markdown.",
		Config: reflect.TypeFor[retrieveBlocksSettings](),
	})
}

const (
	// blocksPageSize is the per-request page size for listing children (Notion's
	// own max is 100).
	blocksPageSize = 100
	// maxBlockPages bounds pagination so a pathological response cannot loop
	// forever; 100 pages is 10k blocks.
	maxBlockPages = 100
)

// retrieveBlocksSettings is the notion-retrieve-blocks block's typed configuration.
type retrieveBlocksSettings struct {
	// Name of the notion connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:notion"`
	// CEL expression for the block or page id whose children to retrieve.
	Block string `json:"block" octo:"label=Block/Page ID,type=cel,required"`
	// When set, store the {results: [...]} object here and leave the body; when
	// empty, replace the body with it.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Notion API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// retrieveBlocksProcessor retrieves a block's children and stores them.
type retrieveBlocksProcessor struct {
	conn        *Connector
	block       *expr.Program
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRetrieveBlocks(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg retrieveBlocksSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("notion-retrieve-blocks: %w", err)
	}
	block, err := compileRequired(deps.Resources, "notion-retrieve-blocks", "block", cfg.Block)
	if err != nil {
		return nil, err
	}
	return &retrieveBlocksProcessor{
		conn:        conn,
		block:       block,
		resultVar:   cfg.ResultVar,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the block id, fetches all its children, and either stores the
// {results: [...]} object in the result variable or replaces the body with it.
func (p *retrieveBlocksProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	id, err := p.block.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("notion-retrieve-blocks block: %w", err)
	}
	results, err := p.fetchChildren(ctx, id)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}

	out := map[string]any{"results": results}
	if p.resultVar != "" {
		msg.Variables.Set(p.resultVar, out)
		return msg, nil
	}
	msg.Body = out
	return msg, nil
}

// fetchChildren lists all of a block's children, following next_cursor until the
// listing is exhausted (or the page cap is hit).
func (p *retrieveBlocksProcessor) fetchChildren(ctx context.Context, id string) ([]any, error) {
	var all []any
	cursor := ""
	for range maxBlockPages {
		path := fmt.Sprintf("blocks/%s/children?page_size=%d", id, blocksPageSize)
		if cursor != "" {
			path += "&start_cursor=" + url.QueryEscape(cursor)
		}
		resp, err := p.conn.Call(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if results, ok := resp["results"].([]any); ok {
			all = append(all, results...)
		}
		if hasMore, _ := resp["has_more"].(bool); !hasMore {
			break
		}
		next, _ := resp["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	return all, nil
}
