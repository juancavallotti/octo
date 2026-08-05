// This file holds helpers shared by the mongodb blocks: binding a block to its
// mongodb connector, the CEL compile plumbing that mirrors the other CEL-driven
// blocks (pinecone, notion, slack, rest), and the per-message database and
// collection address every block resolves before it acts.
package mongodb

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// resolveConnector binds a block to its mongodb connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("mongodb connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a mongodb connector", name)
	}
	return conn, nil
}

// compileOptional compiles a message CEL expression, returning a nil program
// for an empty source so callers can treat "unset" as "skip".
func compileOptional(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	program, err := expr.CompileMessage(res, src)
	if err != nil {
		return nil, fmt.Errorf("%s: compile %s: %w", block, field, err)
	}
	return program, nil
}

// compileRequired compiles a required message CEL expression, erroring with a
// block- and field-labelled message when it is empty or malformed.
func compileRequired(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("%s requires a %q expression", block, field)
	}
	return compileOptional(res, block, field, src)
}

// target is a block's per-message address. Both halves are expressions rather
// than constants for the reason pinecone makes its namespace one: a flow
// serving several tenants routes body.tenantId (or similar) to a different
// database or collection per message, and a constant would force one flow per
// tenant.
type target struct {
	database   *expr.Program
	collection *expr.Program
}

// newTarget compiles the address fields every mongodb block carries. The
// collection is required; the database is optional and falls back to the
// connector's default.
func newTarget(res core.ResourceLoader, block, database, collection string) (target, error) {
	databaseProgram, err := compileOptional(res, block, "database", database)
	if err != nil {
		return target{}, err
	}
	collectionProgram, err := compileRequired(res, block, "collection", collection)
	if err != nil {
		return target{}, err
	}
	return target{database: databaseProgram, collection: collectionProgram}, nil
}

// resolve evaluates the address and asks the connector for the handle. A
// connector that is not started reports so here, which is where a message still
// in flight during shutdown fails.
func (t target) resolve(conn *Connector, activation map[string]any) (*mongo.Collection, error) {
	database := ""
	if t.database != nil {
		var err error
		if database, err = t.database.EvalString(activation); err != nil {
			return nil, fmt.Errorf("database: %w", err)
		}
	}
	collection, err := t.collection.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("collection: %w", err)
	}
	return conn.Collection(database, collection)
}

// evalDocument evaluates a compiled expression to a BSON document, naming the
// field in any failure so the author knows which expression to look at.
func evalDocument(label string, program *expr.Program, activation map[string]any) (bson.Raw, error) {
	raw, err := program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return decodeDocument(label, raw)
}

// evalOptionalDocument is evalDocument for a field that may be absent, in which
// case it yields a nil document the caller passes straight to the driver as
// "no filter" / "no projection".
func evalOptionalDocument(label string, program *expr.Program, activation map[string]any) (bson.Raw, error) {
	if program == nil {
		return nil, nil
	}
	return evalDocument(label, program, activation)
}

// deliver hands a block's result back to the message: into resultVar when the
// block names one, otherwise as the body.
//
// The body is the default on purpose. A find's documents, an insert's ids —
// these are the payload the next block works on, and an HTTP flow that ends
// there should answer with them. Naming a variable is how you say "keep the
// body I came in with", which is the exception, not the rule.
//
// Results go out as JSON bytes and come back through SetBodyJSON so the message
// only ever holds the decoded-JSON kinds its contract promises. That matters
// more here than in most connectors: a bson.ObjectID left in a body would take
// the expensive round-trip on every scope copy, and CEL would compare it as
// whatever it happened to convert to.
func deliver(msg *types.Message, resultVar string, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return deliverJSON(msg, resultVar, raw)
}

// deliverJSON is deliver for a result that is already extended JSON, so the
// documents a cursor produced are not marshaled a second time.
func deliverJSON(msg *types.Message, resultVar string, raw []byte) error {
	if resultVar == "" {
		return msg.SetBodyJSON(raw)
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	msg.Variables.Set(resultVar, value)
	return nil
}
