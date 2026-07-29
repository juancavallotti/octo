// This file provides the "pinecone-query" block: it queries a Pinecone index by
// vector similarity, with an optional metadata filter, and folds the matches
// into a variable.
package pinecone

import (
	"context"
	"fmt"
	"reflect"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterBlock("pinecone-query", newQuery)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "pinecone-query",
		Label:    "Pinecone Query",
		Category: core.CategoryProcessor,
		Description: "Query a Pinecone index for the vectors most similar to a query vector, " +
			"with an optional metadata filter, and store the matches.",
		Config: reflect.TypeFor[querySettings](),
	})
}

// defaultQueryResultVar names the variable the query matches are stored in.
const defaultQueryResultVar = "pineconeMatches"

// defaultTopK is used when the topK setting is left unset or non-positive.
const defaultTopK = 10

// querySettings is the pinecone-query block's typed configuration.
type querySettings struct {
	// Name of the pinecone connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pinecone"`
	// CEL expression evaluating to the query vector (a list of numbers).
	Vector string `json:"vector" octo:"label=Vector,required,type=cel"`
	// Number of matches to return.
	TopK int `json:"topK" octo:"label=Top K,default=10"`
	// CEL expression evaluating to a metadata filter object.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
	// CEL expression for the namespace to query; empty uses the connector default.
	Namespace string `json:"namespace" octo:"label=Namespace,type=cel"`
	// Include each match's vector values in the response.
	IncludeValues bool `json:"includeValues" octo:"label=Include values"`
	// Include each match's metadata in the response.
	IncludeMetadata *bool `json:"includeMetadata" octo:"label=Include metadata,default=true"`
	// Variable the matches are stored in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=pineconeMatches"`
}

// queryProcessor evaluates the vector (and optional filter) expressions and
// queries the index.
type queryProcessor struct {
	conn            *Connector
	vector          *expr.Program
	topK            int
	filter          *expr.Program
	namespace       *expr.Program
	includeValues   bool
	includeMetadata bool
	resultVar       string
	env             map[string]any
}

// newQuery builds a query processor, resolving its pinecone connector and
// compiling the vector/filter/namespace expressions once so a bad reference or
// expression fails at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newQuery(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg querySettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: %w", err)
	}
	vector, err := compileRequired(deps.Resources, "pinecone-query", "vector", cfg.Vector)
	if err != nil {
		return nil, err
	}
	filter, err := compileOptional(deps.Resources, cfg.Filter)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: compile filter: %w", err)
	}
	namespace, err := compileOptional(deps.Resources, cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: compile namespace: %w", err)
	}

	topK := cfg.TopK
	if topK <= 0 {
		topK = defaultTopK
	}
	includeMetadata := true
	if cfg.IncludeMetadata != nil {
		includeMetadata = *cfg.IncludeMetadata
	}

	return &queryProcessor{
		conn:            conn,
		vector:          vector,
		topK:            topK,
		filter:          filter,
		namespace:       namespace,
		includeValues:   cfg.IncludeValues,
		includeMetadata: includeMetadata,
		resultVar:       orDefault(cfg.ResultVar, defaultQueryResultVar),
		env:             expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the query vector (and optional filter), queries the index,
// and stores the matches in the result variable.
func (p *queryProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	vecRaw, err := p.vector.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: vector: %w", err)
	}
	vector, err := toFloat32Slice(vecRaw)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: vector %w", err)
	}

	req := &sdk.QueryByVectorValuesRequest{
		//nolint:gosec // topK is validated positive and small; overflow to uint32 is not reachable in practice
		TopK:            uint32(p.topK),
		Vector:          vector,
		IncludeValues:   p.includeValues,
		IncludeMetadata: p.includeMetadata,
	}

	if p.filter != nil {
		filter, err := evalMetadataFilter(p.filter, activation)
		if err != nil {
			return nil, fmt.Errorf("pinecone-query: %w", err)
		}
		req.MetadataFilter = filter
	}

	namespace, err := evalNamespace(p.namespace, activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: namespace: %w", err)
	}

	resp, err := p.conn.IndexConnection(namespace).QueryByVectorValues(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("pinecone-query: %w", err)
	}

	matches := make([]map[string]any, len(resp.Matches))
	for i, m := range resp.Matches {
		matches[i] = toMatch(m)
	}
	msg.Variables.Set(p.resultVar, matches)
	return msg, nil
}

// toMatch folds one scored match into a plain map: id and score always,
// values/metadata when the vector carries them.
func toMatch(m *sdk.ScoredVector) map[string]any {
	match := map[string]any{"score": m.Score}
	if m.Vector == nil {
		return match
	}
	match["id"] = m.Vector.Id
	for k, v := range vectorFields(m.Vector) {
		match[k] = v
	}
	return match
}
