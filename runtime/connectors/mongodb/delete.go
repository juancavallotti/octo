package mongodb

import (
	"context"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const deleteBlock = connectorType + "-delete"

func registerDelete() {
	core.MustRegisterBlock(deleteBlock, newDelete)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     deleteBlock,
		Label:    "MongoDB Delete",
		Category: core.CategoryProcessor,
		Description: "Remove documents from a MongoDB collection. The deleted count becomes " +
			"the message body, or a variable when one is named.",
		Config: reflect.TypeFor[deleteSettings](),
	})
}

// deleteSettings is the mongodb-delete block's typed configuration.
type deleteSettings struct {
	// Name of the mongodb connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:mongodb"`
	// CEL expression for the database to delete from; empty uses the connector's default.
	Database string `json:"database" octo:"label=Database,type=cel"`
	// CEL expression for the collection to delete from.
	Collection string `json:"collection" octo:"label=Collection,type=cel,required"`
	// CEL expression selecting the documents to remove.
	Filter string `json:"filter" octo:"label=Filter,type=cel,required"`
	// Remove every matching document rather than the first.
	Many bool `json:"many" octo:"label=Delete many,default=false"`
	// Allow a filter that matches everything to empty the collection. Without
	// this, an empty filter with many is refused.
	DeleteAll bool `json:"deleteAll" octo:"label=Allow deleting everything,default=false"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type deleteProcessor struct {
	conn      *Connector
	target    target
	filter    *expr.Program
	many      bool
	deleteAll bool
	resultVar string
	env       map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDelete(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg deleteSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}
	tgt, err := newTarget(deps.Resources, deleteBlock, cfg.Database, cfg.Collection)
	if err != nil {
		return nil, err
	}
	// Required even when deleteAll is set: "match everything" should be
	// something the flow says, not something it omits.
	filter, err := compileRequired(deps.Resources, deleteBlock, "filter", cfg.Filter)
	if err != nil {
		return nil, err
	}

	return &deleteProcessor{
		conn:      conn,
		target:    tgt,
		filter:    filter,
		many:      cfg.Many,
		deleteAll: cfg.DeleteAll,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

func (p *deleteProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	collection, err := p.target.resolve(p.conn, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}
	filter, err := evalDocument("filter", p.filter, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}
	if err := p.guardEmptyFilter(filter); err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}

	result, err := p.remove(ctx, collection, filter)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}
	if err := deliver(msg, p.resultVar, map[string]any{"deletedCount": int(result.DeletedCount)}); err != nil {
		return nil, fmt.Errorf("%s: %w", deleteBlock, err)
	}
	return msg, nil
}

// guardEmptyFilter refuses a delete-many whose filter matches everything, unless
// the flow said deleteAll.
//
// The filter is an expression, so an empty one is rarely written on purpose —
// it is what {"customer": body.customerId} collapses to when the field is
// absent, and CEL happily builds an object out of nothing. That failure mode
// empties a collection, which no error message afterwards can undo. Deleting one
// document is not guarded: the blast radius is one document, and "delete the
// oldest" is a real thing to ask for.
func (p *deleteProcessor) guardEmptyFilter(filter bson.Raw) error {
	if !p.many || p.deleteAll {
		return nil
	}
	elements, err := filter.Elements()
	if err != nil {
		return fmt.Errorf("filter: %w", err)
	}
	if len(elements) == 0 {
		return fmt.Errorf("filter matched every document, which would empty the collection; " +
			"set deleteAll to allow it, or check the filter expression for a field the message did not carry")
	}
	return nil
}

func (p *deleteProcessor) remove(
	ctx context.Context, collection *mongo.Collection, filter bson.Raw,
) (*mongo.DeleteResult, error) {
	var (
		result *mongo.DeleteResult
		err    error
	)
	if p.many {
		result, err = collection.DeleteMany(ctx, filter)
	} else {
		result, err = collection.DeleteOne(ctx, filter)
	}
	if err != nil {
		return nil, fmt.Errorf("delete: %w", err)
	}
	return result, nil
}
