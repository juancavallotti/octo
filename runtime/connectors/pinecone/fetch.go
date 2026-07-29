// This file provides the "pinecone-fetch" block: it fetches vectors from a
// Pinecone index by id and folds them into a variable.
package pinecone

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterBlock("pinecone-fetch", newFetch)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "pinecone-fetch",
		Label:       "Pinecone Fetch",
		Category:    core.CategoryProcessor,
		Description: "Fetch vectors from a Pinecone index by id and store them, keyed by id.",
		Config:      reflect.TypeFor[fetchSettings](),
	})
}

// defaultFetchResultVar names the variable the fetched vectors are stored in.
const defaultFetchResultVar = "pineconeVectors"

// fetchSettings is the pinecone-fetch block's typed configuration.
type fetchSettings struct {
	// Name of the pinecone connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pinecone"`
	// CEL expression evaluating to a list of vector ids.
	IDs string `json:"ids" octo:"label=IDs,required,type=cel"`
	// CEL expression for the namespace to fetch from; empty uses the connector default.
	Namespace string `json:"namespace" octo:"label=Namespace,type=cel"`
	// Variable the fetched vectors are stored in, keyed by id.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=pineconeVectors"`
}

// fetchProcessor evaluates the ids expression and fetches the result.
type fetchProcessor struct {
	conn      *Connector
	ids       *expr.Program
	namespace *expr.Program
	resultVar string
	env       map[string]any
}

// newFetch builds a fetch processor, resolving its pinecone connector and
// compiling the ids/namespace expressions once so a bad reference or
// expression fails at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newFetch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg fetchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: %w", err)
	}
	ids, err := compileRequired(deps.Resources, "pinecone-fetch", "ids", cfg.IDs)
	if err != nil {
		return nil, err
	}
	namespace, err := compileOptional(deps.Resources, cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: compile namespace: %w", err)
	}
	return &fetchProcessor{
		conn:      conn,
		ids:       ids,
		namespace: namespace,
		resultVar: orDefault(cfg.ResultVar, defaultFetchResultVar),
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the ids expression, fetches the vectors, and stores them
// in the result variable, keyed by id.
func (p *fetchProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	idsRaw, err := p.ids.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: ids: %w", err)
	}
	ids, err := toStringSlice(idsRaw)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: ids %w", err)
	}

	namespace, err := evalNamespace(p.namespace, activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: namespace: %w", err)
	}

	resp, err := p.conn.IndexConnection(namespace).FetchVectors(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: %w", err)
	}

	vectors := make(map[string]any, len(resp.Vectors))
	for id, v := range resp.Vectors {
		vectors[id] = vectorFields(v)
	}
	msg.Variables.Set(p.resultVar, vectors)
	return msg, nil
}
