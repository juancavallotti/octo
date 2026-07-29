// This file provides the "pinecone-fetch" block: it fetches vectors from a
// Pinecone index by id and folds them, keyed by id, into the message — the body
// by default, or a variable when one is named.
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
		Type:     "pinecone-fetch",
		Label:    "Pinecone Fetch",
		Category: core.CategoryProcessor,
		Description: "Fetch vectors from a Pinecone index by id. The vectors, keyed by id, become " +
			"the message body, or a variable when one is named.",
		Config: reflect.TypeFor[fetchSettings](),
	})
}

// fetchSettings is the pinecone-fetch block's typed configuration.
type fetchSettings struct {
	// Name of the pinecone connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pinecone"`
	// CEL expression evaluating to a list of vector ids.
	IDs string `json:"ids" octo:"label=IDs,required,type=cel"`
	// CEL expression for the namespace to fetch from; empty uses the connector default.
	Namespace string `json:"namespace" octo:"label=Namespace,type=cel"`
	// When set, store the fetched vectors here and leave the body; when empty, they
	// become the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
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
		resultVar: cfg.ResultVar,
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the ids expression, fetches the vectors, and hands them
// back keyed by id: as the message body, or in the result variable when one is
// named.
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

	idxConn, err := p.conn.IndexConnection(namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: %w", err)
	}

	resp, err := idxConn.FetchVectors(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("pinecone-fetch: %w", err)
	}

	vectors := make(map[string]any, len(resp.Vectors))
	for id, v := range resp.Vectors {
		vectors[id] = vectorFields(v)
	}
	return deliver(msg, p.resultVar, vectors), nil
}
