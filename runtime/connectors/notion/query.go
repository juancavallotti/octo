// This file provides the "notion-query-datasource" block: it queries a Notion
// data source (POST /data_sources/{id}/query) with an optional filter and sorts,
// and folds the response into a variable. The data source can be named directly
// (dataSource) or derived from a database id (database) — in the 2025-09-03 API a
// database holds one or more data sources, so deriving costs an extra GET. The
// query API requires Notion-Version 2025-09-03 or later (the connector's default).
package notion

import (
	"context"
	"fmt"
	"net/http"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerQueryDataSource() {
	core.MustRegisterBlock("notion-query-datasource", newQueryDataSource)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-query-datasource",
		Label:    "Notion Query Data Source",
		Category: core.CategoryProcessor,
		Description: "Query a Notion data source (data_sources/{id}/query) with an optional filter " +
			"and sorts, and store the response. Target it by data source id, or by database id " +
			"(the block derives the data source).",
		Config: reflect.TypeFor[querySettings](),
	})
}

// defaultResultsVar names the variable the query response is stored in.
const defaultResultsVar = "notionResults"

// querySettings is the notion-query-datasource block's typed configuration.
type querySettings struct {
	// Name of the notion connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:notion"`
	// CEL expression for the data source id to query. Set exactly one of dataSource
	// or database.
	DataSource string `json:"dataSource" octo:"label=Data source ID,type=cel"`
	// CEL expression for a database id; the block retrieves the database and queries
	// its first data source. Set exactly one of dataSource or database.
	Database string `json:"database" octo:"label=Database ID,type=cel"`
	// CEL expression producing a Notion filter object.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
	// CEL expression producing a list of Notion sort objects.
	Sorts string `json:"sorts" octo:"label=Sorts,type=cel"`
	// Maximum number of results per call (Notion's own max is 100).
	PageSize int `json:"pageSize" octo:"label=Page size"`
	// Variable the query response is stored in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=notionResults"`
	// Turn a Notion API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// queryProcessor queries a data source and stores the response. Exactly one of
// dataSource or database is non-nil.
type queryProcessor struct {
	conn        *Connector
	dataSource  *expr.Program
	database    *expr.Program
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
	dataSource, database, err := compileTarget(deps.Resources, cfg)
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
		database:    database,
		filter:      filter,
		sorts:       sorts,
		pageSize:    cfg.PageSize,
		resultVar:   orDefault(cfg.ResultVar, defaultResultsVar),
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// compileTarget compiles exactly one of the dataSource or database expressions,
// erroring when neither or both are set.
func compileTarget(res core.ResourceLoader, cfg querySettings) (dataSource, database *expr.Program, err error) {
	hasDataSource := cfg.DataSource != ""
	hasDatabase := cfg.Database != ""
	switch {
	case hasDataSource && hasDatabase:
		return nil, nil, fmt.Errorf("notion-query-datasource: set only one of \"dataSource\" or \"database\"")
	case hasDataSource:
		dataSource, err = compileRequired(res, "notion-query-datasource", "dataSource", cfg.DataSource)
		return dataSource, nil, err
	case hasDatabase:
		database, err = compileRequired(res, "notion-query-datasource", "database", cfg.Database)
		return nil, database, err
	default:
		return nil, nil, fmt.Errorf("notion-query-datasource requires a \"dataSource\" or \"database\" expression")
	}
}

// Process resolves the data source id (directly or via a database), builds the
// query body from the optional filter/sorts/pageSize, and folds the response into
// the result variable. CEL evaluation failures are hard errors; Notion API
// failures honor failOnError.
func (p *queryProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	if p.dataSource != nil {
		id, err := p.dataSource.EvalString(activation)
		if err != nil {
			return nil, fmt.Errorf("notion-query-datasource dataSource: %w", err)
		}
		return p.runQuery(ctx, msg, id, activation)
	}

	dbID, err := p.database.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("notion-query-datasource database: %w", err)
	}
	id, err := p.deriveDataSourceID(ctx, dbID)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return p.runQuery(ctx, msg, id, activation)
}

// runQuery builds the request body, queries the data source, and folds the
// response into the result variable. A Notion API failure honors failOnError.
func (p *queryProcessor) runQuery(
	ctx context.Context, msg *types.Message, id string, activation map[string]any,
) (*types.Message, error) {
	body, err := p.buildBody(activation)
	if err != nil {
		return nil, err
	}
	resp, err := p.conn.Call(ctx, http.MethodPost, "data_sources/"+id+"/query", body)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	msg.Variables.Set(p.resultVar, resp)
	return msg, nil
}

// deriveDataSourceID retrieves a database and returns its first data source id.
func (p *queryProcessor) deriveDataSourceID(ctx context.Context, dbID string) (string, error) {
	db, err := p.conn.Call(ctx, http.MethodGet, "databases/"+dbID, nil)
	if err != nil {
		return "", err
	}
	id, err := firstDataSourceID(db)
	if err != nil {
		return "", fmt.Errorf("database %s: %w", dbID, err)
	}
	return id, nil
}

// buildBody assembles the query request body from the optional filter, sorts, and
// page size, omitting any that are unset.
func (p *queryProcessor) buildBody(activation map[string]any) (map[string]any, error) {
	body := map[string]any{}
	if p.filter != nil {
		filter, err := p.filter.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("notion-query-datasource filter: %w", err)
		}
		body["filter"] = filter
	}
	if p.sorts != nil {
		sorts, err := p.sorts.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("notion-query-datasource sorts: %w", err)
		}
		body["sorts"] = sorts
	}
	if p.pageSize > 0 {
		body["page_size"] = p.pageSize
	}
	return body, nil
}

// firstDataSourceID returns the id of a database's first data source, erroring
// when the database exposes none. A database's data sources are under
// "data_sources" as {id, name} objects (Notion-Version 2025-09-03+).
func firstDataSourceID(db map[string]any) (string, error) {
	sources, _ := db["data_sources"].([]any)
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := source["id"].(string); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("database has no data sources")
}
