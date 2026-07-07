// This file provides the "notion-query-datasource" block: it queries a Notion
// data source (POST /data_sources/{id}/query) with an optional filter and sorts,
// and folds the response into a variable. The data-sources query API requires
// Notion-Version 2025-09-03 or later (the connector's default).
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
	core.MustRegisterBlock("notion-query-datasource", newQueryDataSource)
}

// defaultResultsVar names the variable the query response is stored in.
const defaultResultsVar = "notionResults"

// querySettings is the notion-query-datasource block's typed configuration.
type querySettings struct {
	// Connector names the notion connector to query through (required).
	Connector string `json:"connector"`
	// DataSource is a CEL expression producing the data source id to query
	// (required).
	DataSource string `json:"dataSource"`
	// Filter is an optional CEL expression producing a Notion filter object.
	Filter string `json:"filter"`
	// Sorts is an optional CEL expression producing a list of Notion sort objects.
	Sorts string `json:"sorts"`
	// PageSize caps the number of results per call (Notion's own max is 100); 0
	// leaves it to Notion's default.
	PageSize int `json:"pageSize"`
	// ResultVar names the variable the response is stored in (default
	// "notionResults").
	ResultVar string `json:"resultVar"`
	// FailOnError, when true (the default), turns a Notion API error into a flow
	// error.
	FailOnError *bool `json:"failOnError"`
}

// queryProcessor queries a data source and stores the response.
type queryProcessor struct {
	conn        *Connector
	dataSource  *expr.Program
	filter      *expr.Program
	sorts       *expr.Program
	pageSize    int
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newQueryDataSource(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg querySettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("notion-query-datasource: %w", err)
	}
	dataSource, err := compileRequired(deps.Resources, "notion-query-datasource", "dataSource", cfg.DataSource)
	if err != nil {
		return nil, err
	}
	filter, err := compileOptional(deps.Resources, cfg.Filter)
	if err != nil {
		return nil, fmt.Errorf("notion-query-datasource: compile filter: %w", err)
	}
	sorts, err := compileOptional(deps.Resources, cfg.Sorts)
	if err != nil {
		return nil, fmt.Errorf("notion-query-datasource: compile sorts: %w", err)
	}
	return &queryProcessor{
		conn:        conn,
		dataSource:  dataSource,
		filter:      filter,
		sorts:       sorts,
		pageSize:    cfg.PageSize,
		resultVar:   orDefault(cfg.ResultVar, defaultResultsVar),
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the data source id, builds the query body from the optional
// filter/sorts/pageSize, and folds the response into the result variable.
func (p *queryProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	id, err := p.dataSource.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("notion-query-datasource dataSource: %w", err)
	}

	body := map[string]any{}
	if p.filter != nil {
		filter, filterErr := p.filter.Eval(activation)
		if filterErr != nil {
			return nil, fmt.Errorf("notion-query-datasource filter: %w", filterErr)
		}
		body["filter"] = filter
	}
	if p.sorts != nil {
		sorts, sortsErr := p.sorts.Eval(activation)
		if sortsErr != nil {
			return nil, fmt.Errorf("notion-query-datasource sorts: %w", sortsErr)
		}
		body["sorts"] = sorts
	}
	if p.pageSize > 0 {
		body["page_size"] = p.pageSize
	}

	resp, err := p.conn.Call(ctx, http.MethodPost, "data_sources/"+id+"/query", body)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	msg.Variables.Set(p.resultVar, resp)
	return msg, nil
}
