package mongodb

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const findBlock = connectorType + "-find"

func registerFind() {
	core.MustRegisterBlock(findBlock, newFind)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     findBlock,
		Label:    "MongoDB Find",
		Category: core.CategoryProcessor,
		Description: "Read documents from a MongoDB collection. The matching documents become " +
			"the message body, or a variable when one is named.",
		Config: reflect.TypeFor[findSettings](),
	})
}

// findSettings is the mongodb-find block's typed configuration.
type findSettings struct {
	// Name of the mongodb connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:mongodb"`
	// CEL expression for the database to read from; empty uses the connector's default.
	Database string `json:"database" octo:"label=Database,type=cel"`
	// CEL expression for the collection to read from.
	Collection string `json:"collection" octo:"label=Collection,type=cel,required"`
	// CEL expression evaluating to a query filter; empty matches every document.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
	// CEL expression evaluating to a projection, e.g. {"sku": 1, "_id": 0}.
	Projection string `json:"projection" octo:"label=Projection,type=cel"`
	// CEL expression evaluating to a sort. Sorting by more than one key needs the
	// list form, e.g. [{"created": -1}, {"sku": 1}].
	Sort string `json:"sort" octo:"label=Sort,type=cel"`
	// Most documents to return; 0 returns them all.
	Limit int `json:"limit" octo:"label=Limit"`
	// Documents to skip before returning any.
	Skip int `json:"skip" octo:"label=Skip"`
	// Return the first matching document rather than a list, and null when
	// nothing matched.
	Single bool `json:"single" octo:"label=Single document,default=false"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type findProcessor struct {
	conn       *Connector
	target     target
	filter     *expr.Program
	projection *expr.Program
	sort       *expr.Program
	limit      int64
	skip       int64
	single     bool
	resultVar  string
	env        map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newFind(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg findSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	tgt, err := newTarget(deps.Resources, findBlock, cfg.Database, cfg.Collection)
	if err != nil {
		return nil, err
	}
	if cfg.Limit < 0 || cfg.Skip < 0 {
		return nil, fmt.Errorf("%s: limit and skip must not be negative", findBlock)
	}

	filter, err := compileOptional(deps.Resources, findBlock, "filter", cfg.Filter)
	if err != nil {
		return nil, err
	}
	projection, err := compileOptional(deps.Resources, findBlock, "projection", cfg.Projection)
	if err != nil {
		return nil, err
	}
	sort, err := compileOptional(deps.Resources, findBlock, "sort", cfg.Sort)
	if err != nil {
		return nil, err
	}

	return &findProcessor{
		conn:       conn,
		target:     tgt,
		filter:     filter,
		projection: projection,
		sort:       sort,
		limit:      int64(cfg.Limit),
		skip:       int64(cfg.Skip),
		single:     cfg.Single,
		resultVar:  cfg.ResultVar,
		env:        expr.EnvActivation(deps.Env),
	}, nil
}

func (p *findProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	collection, err := p.target.resolve(p.conn, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	query, err := p.evalQuery(activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}

	if p.single {
		return p.one(ctx, collection, query, msg)
	}
	return p.many(ctx, collection, query, msg)
}

// query is one message's evaluated find: what to match, what to return, and in
// what order.
type query struct {
	filter     bson.Raw
	projection bson.Raw
	sort       bson.D
}

func (p *findProcessor) evalQuery(activation map[string]any) (query, error) {
	filter, err := evalOptionalDocument("filter", p.filter, activation)
	if err != nil {
		return query{}, err
	}
	projection, err := evalOptionalDocument("projection", p.projection, activation)
	if err != nil {
		return query{}, err
	}
	sort, err := evalSort(p.sort, activation)
	if err != nil {
		return query{}, err
	}
	return query{filter: filter, projection: projection, sort: sort}, nil
}

// one runs the single-document form. A miss is a null body rather than an
// error, mirroring the sql block's single mode: "no such order" is an ordinary
// answer a flow branches on, not a failure.
func (p *findProcessor) one(
	ctx context.Context, collection *mongo.Collection, q query, msg *types.Message,
) (*types.Message, error) {
	opts := options.FindOne()
	if q.projection != nil {
		opts.SetProjection(q.projection)
	}
	if q.sort != nil {
		opts.SetSort(q.sort)
	}
	if p.skip > 0 {
		opts.SetSkip(p.skip)
	}

	doc, err := collection.FindOne(ctx, orEmpty(q.filter), opts).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		deliverMiss(msg, p.resultVar)
		return msg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	raw, err := encodeDocument(doc, p.conn.Canonical())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	return msg, deliverJSON(msg, p.resultVar, raw)
}

func (p *findProcessor) many(
	ctx context.Context, collection *mongo.Collection, q query, msg *types.Message,
) (*types.Message, error) {
	opts := options.Find()
	if q.projection != nil {
		opts.SetProjection(q.projection)
	}
	if q.sort != nil {
		opts.SetSort(q.sort)
	}
	if p.limit > 0 {
		opts.SetLimit(p.limit)
	}
	if p.skip > 0 {
		opts.SetSkip(p.skip)
	}

	cursor, err := collection.Find(ctx, orEmpty(q.filter), opts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	var docs []bson.Raw
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	raw, err := encodeDocuments(docs, p.conn.Canonical())
	if err != nil {
		return nil, fmt.Errorf("%s: %w", findBlock, err)
	}
	return msg, deliverJSON(msg, p.resultVar, raw)
}

// orEmpty substitutes the empty document for an absent filter: the driver wants
// one either way, and "no filter" and "match everything" are the same query.
func orEmpty(doc bson.Raw) any {
	if doc == nil {
		return bson.D{}
	}
	return doc
}

// deliverMiss records "nothing matched" as a null body, or a null variable when
// the block names one — an ordinary answer a flow branches on, not a failure.
func deliverMiss(msg *types.Message, resultVar string) {
	if resultVar != "" {
		msg.Variables.Set(resultVar, nil)
		return
	}
	msg.SetBody(nil)
}

// evalSort evaluates a sort expression to the ordered bson.D the driver takes.
// decodeSort is shared with the pipeline decoder, since a $sort stage has the
// same key-order problem this setting does.
func evalSort(program *expr.Program, activation map[string]any) (bson.D, error) {
	if program == nil {
		return nil, nil
	}
	value, err := program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("sort: %w", err)
	}
	return decodeSort(value)
}
