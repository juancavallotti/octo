package mongodb

import (
	"context"
	"fmt"
	"reflect"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const updateBlock = connectorType + "-update"

func registerUpdate() {
	core.MustRegisterBlock(updateBlock, newUpdate)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     updateBlock,
		Label:    "MongoDB Update",
		Category: core.CategoryProcessor,
		Description: "Modify documents in a MongoDB collection, optionally inserting one when " +
			"the filter matches nothing. The counts become the message body, or a variable " +
			"when one is named.",
		Config: reflect.TypeFor[updateSettings](),
	})
}

// updateSettings is the mongodb-update block's typed configuration.
type updateSettings struct {
	// Name of the mongodb connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:mongodb"`
	// CEL expression for the database to write to; empty uses the connector's default.
	Database string `json:"database" octo:"label=Database,type=cel"`
	// CEL expression for the collection to write to.
	Collection string `json:"collection" octo:"label=Collection,type=cel,required"`
	// CEL expression selecting the documents to modify.
	Filter string `json:"filter" octo:"label=Filter,type=cel,required"`
	// CEL expression for the update operators to apply, e.g. {"$set": {...}}.
	// Set this or replacement, not both.
	Update string `json:"update" octo:"label=Update,type=cel"`
	// CEL expression for a whole document to put in place of the matched one,
	// keeping only its _id. Set this or update, not both.
	Replacement string `json:"replacement" octo:"label=Replacement,type=cel"`
	// Insert a document when the filter matches nothing.
	Upsert bool `json:"upsert" octo:"label=Upsert,default=false"`
	// Modify every matching document rather than the first. Not available with
	// replacement, which replaces exactly one.
	Many bool `json:"many" octo:"label=Update many,default=false"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
}

type updateProcessor struct {
	conn   *Connector
	target target
	filter *expr.Program
	// change is the update document or the replacement, and replace says which.
	// They are one field because exactly one of them exists.
	change    *expr.Program
	replace   bool
	upsert    bool
	many      bool
	resultVar string
	env       map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newUpdate(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg updateSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	tgt, err := newTarget(deps.Resources, updateBlock, cfg.Database, cfg.Collection)
	if err != nil {
		return nil, err
	}
	filter, err := compileRequired(deps.Resources, updateBlock, "filter", cfg.Filter)
	if err != nil {
		return nil, err
	}
	change, replace, err := resolveChange(deps.Resources, cfg)
	if err != nil {
		return nil, err
	}

	return &updateProcessor{
		conn:      conn,
		target:    tgt,
		filter:    filter,
		change:    change,
		replace:   replace,
		upsert:    cfg.Upsert,
		many:      cfg.Many,
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// resolveChange picks the one of update/replacement that is set, and compiles
// it. They are different operations wearing similar clothes — an update applies
// operators to the document that is there, a replacement discards it — so
// configuring both, or neither, is settled at build time rather than by picking
// one per message.
//
// A replacement with many is refused for the same reason the driver has no
// ReplaceMany: writing the same document over every match, _id apart, is
// something nobody means.
func resolveChange(res core.ResourceLoader, cfg updateSettings) (*expr.Program, bool, error) {
	hasUpdate, hasReplacement := cfg.Update != "", cfg.Replacement != ""
	switch {
	case !hasUpdate && !hasReplacement:
		return nil, false, fmt.Errorf(`%s: set either "update" or "replacement"`, updateBlock)
	case hasUpdate && hasReplacement:
		return nil, false, fmt.Errorf(`%s: set only one of "update" or "replacement"`, updateBlock)
	case hasReplacement && cfg.Many:
		return nil, false, fmt.Errorf(`%s: "replacement" replaces one document; `+
			`use "update" with operators to change many`, updateBlock)
	}

	if hasReplacement {
		program, err := compileRequired(res, updateBlock, "replacement", cfg.Replacement)
		return program, true, err
	}
	program, err := compileRequired(res, updateBlock, "update", cfg.Update)
	return program, false, err
}

func (p *updateProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	collection, err := p.target.resolve(p.conn, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	filter, err := evalDocument("filter", p.filter, activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	change, err := p.evalChange(activation)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}

	result, err := p.apply(ctx, collection, filter, change)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	rendered, err := p.result(result)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	if err := deliver(msg, p.resultVar, rendered); err != nil {
		return nil, fmt.Errorf("%s: %w", updateBlock, err)
	}
	return msg, nil
}

// evalChange evaluates the update or replacement.
//
// An update takes either operators or an aggregation pipeline, which MongoDB
// has accepted since 4.2 and which a list expression is the natural spelling of.
// The operator form is checked here rather than left to the server: a plain
// object passed as an update is refused by MongoDB with a message about the
// first field name, which says nothing about the mistake actually made.
func (p *updateProcessor) evalChange(activation map[string]any) (any, error) {
	if p.replace {
		return evalDocument("replacement", p.change, activation)
	}
	value, err := p.change.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	if _, isPipeline := value.([]any); isPipeline {
		stages, pipelineErr := decodePipeline("update", value)
		if pipelineErr != nil {
			return nil, pipelineErr
		}
		// Widened to []any so the driver sees a plain slice of documents rather
		// than having to reason about []bson.Raw, which is a slice of byte slices.
		out := make([]any, len(stages))
		for i, stage := range stages {
			out[i] = stage
		}
		return out, nil
	}
	doc, err := decodeDocument("update", value)
	if err != nil {
		return nil, err
	}
	if err := requireOperators(doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// requireOperators rejects an update document that is a plain object, naming
// what it should have been. An update says how to change a document; the
// whole-document form is what replacement is for.
func requireOperators(doc bson.Raw) error {
	elements, err := doc.Elements()
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if len(elements) == 0 {
		return fmt.Errorf(`update: is empty; it needs an operator such as {"$set": {...}}`)
	}
	for _, element := range elements {
		if key := element.Key(); len(key) == 0 || key[0] != '$' {
			return fmt.Errorf(`update: %q is not an operator; wrap the fields you want to `+
				`change in {"$set": {...}}, or use "replacement" to replace the whole document`, key)
		}
	}
	return nil
}

func (p *updateProcessor) apply(
	ctx context.Context, collection *mongo.Collection, filter bson.Raw, change any,
) (*mongo.UpdateResult, error) {
	var (
		result *mongo.UpdateResult
		err    error
	)
	switch {
	case p.replace:
		result, err = collection.ReplaceOne(ctx, filter, change, options.Replace().SetUpsert(p.upsert))
	case p.many:
		result, err = collection.UpdateMany(ctx, filter, change, options.UpdateMany().SetUpsert(p.upsert))
	default:
		result, err = collection.UpdateOne(ctx, filter, change, options.UpdateOne().SetUpsert(p.upsert))
	}
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	return result, nil
}

// result renders the driver's counts. matchedCount and modifiedCount differ when
// an update sets a field to the value it already held, which is worth being able
// to tell apart — "nothing matched" and "nothing changed" are different bugs.
//
// The counts are int, not the driver's int64: a CEL comparison against a typed
// number needs a literal suffix no flow author expects to write.
func (p *updateProcessor) result(result *mongo.UpdateResult) (map[string]any, error) {
	rendered := map[string]any{
		"matchedCount":  int(result.MatchedCount),
		"modifiedCount": int(result.ModifiedCount),
		"upsertedCount": int(result.UpsertedCount),
		"upsertedId":    nil,
	}
	// Present and null when no upsert happened, rather than absent, so a flow
	// reading it does not have to test for the field before testing its value.
	if result.UpsertedID != nil {
		id, err := encodeValue(result.UpsertedID, p.conn.Canonical())
		if err != nil {
			return nil, err
		}
		rendered["upsertedId"] = id
	}
	return rendered, nil
}
