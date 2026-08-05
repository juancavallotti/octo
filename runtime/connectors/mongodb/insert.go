package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const insertBlock = connectorType + "-insert"

func registerInsert() {
	core.MustRegisterBlock(insertBlock, newInsert)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     insertBlock,
		Label:    "MongoDB Insert",
		Category: core.CategoryProcessor,
		Description: "Write one document, or a list of them, into a MongoDB collection. " +
			"The inserted ids become the message body, or a variable when one is named.",
		Config: reflect.TypeFor[insertSettings](),
	})
}

// insertSettings is the mongodb-insert block's typed configuration.
type insertSettings struct {
	// Name of the mongodb connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:mongodb"`
	// CEL expression for the database to write to; empty uses the connector's default.
	Database string `json:"database" octo:"label=Database,type=cel"`
	// CEL expression for the collection to write to.
	Collection string `json:"collection" octo:"label=Collection,type=cel,required"`
	// CEL expression for what to insert: an object writes one document, a list of
	// objects writes them all. Often just "body".
	Document string `json:"document" octo:"label=Document,type=cel,required"`
	// Keep inserting after a document fails, rather than stopping at the first
	// one. Only applies to a list.
	Unordered bool `json:"unordered" octo:"label=Unordered,default=false"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type insertProcessor struct {
	conn      *Connector
	target    target
	document  *expr.Program
	unordered bool
	resultVar string
	env       map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newInsert(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg insertSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", insertBlock, err)
	}
	tgt, err := newTarget(deps.Resources, insertBlock, cfg.Database, cfg.Collection)
	if err != nil {
		return nil, err
	}
	document, err := compileRequired(deps.Resources, insertBlock, "document", cfg.Document)
	if err != nil {
		return nil, err
	}

	return &insertProcessor{
		conn:      conn,
		target:    tgt,
		document:  document,
		unordered: cfg.Unordered,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process inserts one document or many, decided by what the expression
// evaluated to rather than by a mode setting. A list is InsertMany and an object
// is InsertOne — the value already says which, so there is nothing for the
// author to keep in sync with it.
func (p *insertProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	collection, err := p.target.resolve(p.conn, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", insertBlock, err)
	}
	value, err := p.document.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("%s: document: %w", insertBlock, err)
	}

	ids, err := p.insert(ctx, collection, value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", insertBlock, err)
	}
	result, err := p.result(ids)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", insertBlock, err)
	}
	if err := deliver(msg, p.resultVar, result); err != nil {
		return nil, fmt.Errorf("%s: %w", insertBlock, err)
	}
	return msg, nil
}

func (p *insertProcessor) insert(ctx context.Context, collection *mongo.Collection, value any) ([]any, error) {
	if list, ok := value.([]any); ok {
		return p.insertMany(ctx, collection, list)
	}
	document, err := decodeDocument("document", value)
	if err != nil {
		return nil, err
	}
	result, err := collection.InsertOne(ctx, document)
	if err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}
	return []any{result.InsertedID}, nil
}

func (p *insertProcessor) insertMany(
	ctx context.Context, collection *mongo.Collection, list []any,
) ([]any, error) {
	// An empty list is a no-op rather than a driver error: a flow inserting
	// whatever it filtered out of a payload should not fail on the day the
	// payload filters down to nothing.
	if len(list) == 0 {
		return nil, nil
	}
	documents := make([]any, 0, len(list))
	for i, item := range list {
		document, err := decodeDocument(fmt.Sprintf("document[%d]", i), item)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	opts := options.InsertMany().SetOrdered(!p.unordered)
	result, err := collection.InsertMany(ctx, documents, opts)
	if err != nil {
		// A failed batch can still have written part of itself — always for an
		// unordered insert, and up to the failing document for an ordered one. The
		// flow fails either way, but the error says how much landed, because
		// "insert failed" and "insert half-failed" call for different recoveries.
		if result != nil && len(result.InsertedIDs) > 0 {
			return nil, fmt.Errorf("insert: %d of %d documents were written: %w",
				len(result.InsertedIDs), len(documents), err)
		}
		return nil, fmt.Errorf("insert: %w", err)
	}
	return result.InsertedIDs, nil
}

// result renders the inserted ids back into the message. The ids go out as
// extended JSON, so an _id the server generated comes back in the {"$oid": ...}
// shape a filter takes — which is what makes "insert it, then read it back"
// work without the flow author converting anything.
func (p *insertProcessor) result(ids []any) (map[string]any, error) {
	encoded := make([]json.RawMessage, 0, len(ids))
	for _, id := range ids {
		raw, err := encodeValue(id, p.conn.Canonical())
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, raw)
	}
	// A bare count would be a valid body, but a naked number is a poor HTTP
	// response and a poor thing to write CEL against, so it goes back named.
	return map[string]any{
		"insertedCount": len(ids),
		"insertedIds":   encoded,
	}, nil
}
