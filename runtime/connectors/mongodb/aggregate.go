package mongodb

import (
	"context"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const aggregateBlock = connectorType + "-aggregate"

func registerAggregate() {
	core.MustRegisterBlock(aggregateBlock, newAggregate)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     aggregateBlock,
		Label:    "MongoDB Aggregate",
		Category: core.CategoryProcessor,
		Description: "Run an aggregation pipeline over a MongoDB collection. The resulting " +
			"documents become the message body, or a variable when one is named.",
		Config: reflect.TypeFor[aggregateSettings](),
	})
}

// aggregateSettings is the mongodb-aggregate block's typed configuration.
type aggregateSettings struct {
	// Name of the mongodb connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:mongodb"`
	// CEL expression for the database to read from; empty uses the connector's default.
	Database string `json:"database" octo:"label=Database,type=cel"`
	// CEL expression for the collection to aggregate over.
	Collection string `json:"collection" octo:"label=Collection,type=cel,required"`
	// CEL expression evaluating to a list of pipeline stages, e.g.
	// [{"$match": {...}}, {"$group": {...}}].
	Pipeline string `json:"pipeline" octo:"label=Pipeline,type=cel,required"`
	// When set, store the documents here and leave the body; when empty, they
	// become the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type aggregateProcessor struct {
	conn      *Connector
	target    target
	pipeline  *expr.Program
	resultVar string
	env       map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newAggregate(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg aggregateSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", aggregateBlock, err)
	}
	tgt, err := newTarget(deps.Resources, aggregateBlock, cfg.Database, cfg.Collection)
	if err != nil {
		return nil, err
	}
	pipeline, err := compileRequired(deps.Resources, aggregateBlock, "pipeline", cfg.Pipeline)
	if err != nil {
		return nil, err
	}

	return &aggregateProcessor{
		conn:      conn,
		target:    tgt,
		pipeline:  pipeline,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process runs the pipeline and delivers its documents.
//
// A pipeline is JSON, which is why it belongs in an Octo definition at all: the
// stages an author would type into mongosh are the stages they write here, with
// body and vars reachable inside them.
func (p *aggregateProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	collection, err := p.target.resolve(p.conn, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", aggregateBlock, err)
	}
	value, err := p.pipeline.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("%s: pipeline: %w", aggregateBlock, err)
	}
	stages, err := decodePipeline("pipeline", value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", aggregateBlock, err)
	}

	cursor, err := collection.Aggregate(ctx, stages)
	if err != nil {
		return nil, fmt.Errorf("%s: aggregate: %w", aggregateBlock, err)
	}
	var docs []bson.Raw
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("%s: aggregate: %w", aggregateBlock, err)
	}

	raw, err := encodeDocuments(docs, p.conn.Canonical())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", aggregateBlock, err)
	}
	if err := deliverJSON(msg, p.resultVar, raw); err != nil {
		return nil, fmt.Errorf("%s: %w", aggregateBlock, err)
	}
	return msg, nil
}
