// This file provides the "notion-retrieve-page" block: it retrieves a Notion page
// object by id (GET /pages/{id}) and folds it into a variable, so a flow can read
// the page's properties. A page's content lives in its child blocks, not the page
// object, so this returns properties only.
package notion

import (
	"context"
	"fmt"
	"net/http"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("notion-retrieve-page", newRetrievePage)
}

// defaultPageVar names the variable the retrieved page object is stored in.
const defaultPageVar = "notionPage"

// retrieveSettings is the notion-retrieve-page block's typed configuration.
type retrieveSettings struct {
	// Connector names the notion connector to retrieve through (required).
	Connector string `json:"connector"`
	// Page is a CEL expression producing the page id to retrieve (required).
	Page string `json:"page"`
	// ResultVar names the variable the page object is stored in (default
	// "notionPage").
	ResultVar string `json:"resultVar"`
	// FailOnError, when true (the default), turns a Notion API error (e.g.
	// object_not_found) into a flow error.
	FailOnError *bool `json:"failOnError"`
}

// retrieveProcessor retrieves a page and stores the result.
type retrieveProcessor struct {
	conn        *Connector
	page        *expr.Program
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRetrievePage(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg retrieveSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("notion-retrieve-page: %w", err)
	}
	page, err := compileRequired(deps.Resources, "notion-retrieve-page", "page", cfg.Page)
	if err != nil {
		return nil, err
	}
	return &retrieveProcessor{
		conn:        conn,
		page:        page,
		resultVar:   orDefault(cfg.ResultVar, defaultPageVar),
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the page id and folds the returned page object into the result
// variable.
func (p *retrieveProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	id, err := p.page.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("notion-retrieve-page page: %w", err)
	}
	resp, err := p.conn.Call(ctx, http.MethodGet, "pages/"+id, nil)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	msg.Variables.Set(p.resultVar, resp)
	return msg, nil
}
