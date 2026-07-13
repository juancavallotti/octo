// This file provides the "sql" block: a processor that runs a SQL statement
// against this database connector and folds the result into the message body. In
// query mode (the default) the body becomes an array of row objects, or a single
// object when "single" is set; in exec mode the body becomes {"rowsAffected": N}.
// Bind parameters come from CEL expressions evaluated against the message.
//
// Placeholder style is the driver's own: $1, $2 for Postgres and ? for SQLite.
//
// The block lives in the connector's package: importing the connector registers
// the block too, and it binds to the connector by concrete type.
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterBlock("sql", newSQL)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "sql",
		Label:       "SQL",
		Category:    core.CategoryProcessor,
		Description: "Execute a SQL statement against a database connector.",
		Config:      reflect.TypeFor[sqlSettings](),
	})
}

// sqlSettings is the sql block's typed configuration.
type sqlSettings struct {
	// Name of the database connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:database"`
	// SQL statement with placeholders.
	Query string `json:"query" octo:"label=Query,required"`
	// CEL expressions bound to the statement placeholders.
	Args []string `json:"args" octo:"label=Arguments"`
	// Use ExecContext (no result set); result becomes {rowsAffected: N}.
	Exec bool `json:"exec" octo:"label=Exec mode,default=false"`
	// In query mode, unwrap the result to the first row object.
	Single bool `json:"single" octo:"label=Single row,default=false"`
}

// processor runs the statement and writes its result into the message body.
type processor struct {
	db     *sql.DB
	query  string
	args   []*expr.Program
	exec   bool
	single bool
	env    map[string]any
}

// newSQL builds a sql processor, resolving its database connector and compiling
// the argument expressions once so a bad connector reference or expression fails
// at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSQL(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg sqlSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Query == "" {
		return nil, fmt.Errorf("sql block: query is required")
	}

	db, err := resolveDB(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}

	args := make([]*expr.Program, 0, len(cfg.Args))
	for _, e := range cfg.Args {
		program, compileErr := expr.CompileMessage(deps.Resources, e)
		if compileErr != nil {
			return nil, compileErr
		}
		args = append(args, program)
	}

	return &processor{
		db: db, query: cfg.Query, args: args, exec: cfg.Exec, single: cfg.Single,
		env: expr.EnvActivation(deps.Env),
	}, nil
}

// resolveDB binds the block to its database connector by name.
func resolveDB(name string, deps core.BlockDeps) (*sql.DB, error) {
	if name == "" {
		return nil, fmt.Errorf("sql block: connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("sql block: connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("sql block: database connector %q is not configured", name)
	}
	provider, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("sql block: connector %q is not a database", name)
	}
	return provider.DB()
}

// Process evaluates the bind args, runs the statement under the message context,
// folds the result into the body, and returns the message.
func (p *processor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	args, err := p.evalArgs(msg)
	if err != nil {
		return nil, err
	}

	if p.exec {
		result, execErr := p.db.ExecContext(ctx, p.query, args...)
		if execErr != nil {
			return nil, fmt.Errorf("sql exec: %w", execErr)
		}
		affected, _ := result.RowsAffected()
		if setErr := setBody(msg, map[string]any{"rowsAffected": affected}); setErr != nil {
			return nil, setErr
		}
		return msg, nil
	}

	rows, err := p.db.QueryContext(ctx, p.query, args...)
	if err != nil {
		return nil, fmt.Errorf("sql query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records, err := scanRows(rows)
	if err != nil {
		return nil, err
	}

	if p.single {
		var record any
		if len(records) > 0 {
			record = records[0]
		}
		if setErr := setBody(msg, record); setErr != nil {
			return nil, setErr
		}
		return msg, nil
	}

	if setErr := setBody(msg, records); setErr != nil {
		return nil, setErr
	}
	return msg, nil
}

// setBody folds a Go value into the message body, normalizing it to the runtime's
// decoded-JSON kinds (numbers become float64, etc.) via SetBodyJSON so downstream
// stages and CEL expressions see consistent types.
func setBody(msg *types.Message, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode sql result: %w", err)
	}
	return msg.SetBodyJSON(raw)
}

// evalArgs evaluates each compiled argument expression against the message.
func (p *processor) evalArgs(msg *types.Message) ([]any, error) {
	if len(p.args) == 0 {
		return nil, nil
	}
	activation := expr.MessageActivation(msg, p.env)
	args := make([]any, 0, len(p.args))
	for _, program := range p.args {
		value, err := program.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("evaluate sql arg: %w", err)
		}
		args = append(args, value)
	}
	return args, nil
}

// scanRows reads all rows into a slice of column->value maps. []byte values
// (which some drivers return for text/blob columns) are converted to strings so
// the result serializes as JSON text rather than base64.
func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("sql columns: %w", err)
	}

	records := make([]map[string]any, 0)
	for rows.Next() {
		cells := make([]any, len(cols))
		pointers := make([]any, len(cols))
		for i := range cells {
			pointers[i] = &cells[i]
		}
		if scanErr := rows.Scan(pointers...); scanErr != nil {
			return nil, fmt.Errorf("sql scan: %w", scanErr)
		}

		record := make(map[string]any, len(cols))
		for i, col := range cols {
			if b, ok := cells[i].([]byte); ok {
				record[col] = string(b)
				continue
			}
			record[col] = cells[i]
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sql rows: %w", err)
	}
	return records, nil
}
