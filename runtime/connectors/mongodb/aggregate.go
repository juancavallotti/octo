package mongodb

import (
	"context"
	"errors"
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
	// Most documents to return; 0 returns them all. Appended to the pipeline as a
	// final $limit stage.
	Limit int `json:"limit" octo:"label=Limit"`
	// When set, store the documents here and leave the body; when empty, they
	// become the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type aggregateProcessor struct {
	conn      *Connector
	target    target
	pipeline  *expr.Program
	limit     int
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
	if cfg.Limit < 0 {
		return nil, fmt.Errorf("%s: limit must not be negative", aggregateBlock)
	}

	return &aggregateProcessor{
		conn:      conn,
		target:    tgt,
		pipeline:  pipeline,
		limit:     cfg.Limit,
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
	stages, err = p.bound(stages)
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

// limitStage bounds a pipeline's output, the way the find block's limit bounds a
// query's.
const limitStage = "$limit"

// bound appends a $limit stage when the block sets a limit, so the server stops
// producing documents rather than the cursor reading everything the pipeline
// made — the whole result is materialised as a list and then again as extended
// JSON, so an unbounded pipeline costs memory in proportion to what it matched.
//
// The stage goes last, which is both where it has to be to bound the pipeline as
// a whole and where a reader of one expects to find it. A pipeline that already
// ends in $limit keeps working: the smaller of the two wins, which is what
// either of them alone would have done.
func (p *aggregateProcessor) bound(stages []bson.Raw) ([]bson.Raw, error) {
	if p.limit <= 0 {
		return stages, nil
	}
	if len(stages) > 0 && writesToCollection(stages[len(stages)-1]) {
		return nil, errors.New("limit cannot be applied to a pipeline ending in $out or $merge: " +
			"those stages must be last, and return no documents to limit")
	}
	limit, err := bson.Marshal(bson.D{{Key: limitStage, Value: p.limit}})
	if err != nil {
		return nil, fmt.Errorf("limit: %w", err)
	}
	return append(stages, bson.Raw(limit)), nil
}

// writesToCollection reports whether a stage sends the pipeline's output to a
// collection instead of returning it. Both such stages must themselves be last,
// so nothing can be appended after one.
func writesToCollection(stage bson.Raw) bool {
	element, err := stage.IndexErr(0)
	if err != nil {
		return false
	}
	switch element.Key() {
	case "$out", "$merge":
		return true
	default:
		return false
	}
}
