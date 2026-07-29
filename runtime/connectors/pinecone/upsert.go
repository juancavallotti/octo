// This file provides the "pinecone-upsert" block: it evaluates a CEL expression
// to a list of {id, values, metadata} objects and upserts them into a Pinecone
// index, chunking automatically so a large batch doesn't exceed the API's
// per-request vector limit.
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
	core.MustRegisterBlock("pinecone-upsert", newUpsert)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "pinecone-upsert",
		Label:    "Pinecone Upsert",
		Category: core.CategoryProcessor,
		Description: "Upsert vectors ({id, values, metadata}) into a Pinecone index, chunking " +
			"automatically to stay under the API's per-request limit.",
		Config: reflect.TypeFor[upsertSettings](),
	})
}

// defaultUpsertResultVar names the variable the upserted-vector count is
// stored in.
const defaultUpsertResultVar = "pineconeUpserted"

// upsertChunkSize caps how many vectors go in one UpsertVectors call, matching
// Pinecone's documented per-request limit.
const upsertChunkSize = 100

// upsertSettings is the pinecone-upsert block's typed configuration.
type upsertSettings struct {
	// Name of the pinecone connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pinecone"`
	// CEL expression evaluating to a list of {id, values, metadata} objects.
	Vectors string `json:"vectors" octo:"label=Vectors,required,type=cel"`
	// CEL expression for the target namespace; empty uses the connector default.
	Namespace string `json:"namespace" octo:"label=Namespace,type=cel"`
	// Variable the total upserted-vector count is stored in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=pineconeUpserted"`
}

// upsertProcessor evaluates the vectors expression and upserts the result.
type upsertProcessor struct {
	conn      *Connector
	vectors   *expr.Program
	namespace *expr.Program
	resultVar string
	env       map[string]any
}

// newUpsert builds an upsert processor, resolving its pinecone connector and
// compiling the vectors/namespace expressions once so a bad reference or
// expression fails at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newUpsert(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg upsertSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("pinecone-upsert: %w", err)
	}
	vectors, err := compileRequired(deps.Resources, "pinecone-upsert", "vectors", cfg.Vectors)
	if err != nil {
		return nil, err
	}
	namespace, err := compileOptional(deps.Resources, cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-upsert: compile namespace: %w", err)
	}
	return &upsertProcessor{
		conn:      conn,
		vectors:   vectors,
		namespace: namespace,
		resultVar: orDefault(cfg.ResultVar, defaultUpsertResultVar),
		env:       expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the vectors expression, upserts the result in chunks of
// upsertChunkSize, and stores the total upserted count in the result variable.
func (p *upsertProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	raw, err := p.vectors.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-upsert: vectors: %w", err)
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("pinecone-upsert: vectors must evaluate to a list, got %T", raw)
	}

	vectors := make([]*sdk.Vector, len(items))
	for i, item := range items {
		vec, err := toVector(item)
		if err != nil {
			return nil, fmt.Errorf("pinecone-upsert: vectors[%d]: %w", i, err)
		}
		vectors[i] = vec
	}

	namespace, err := evalNamespace(p.namespace, activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-upsert: namespace: %w", err)
	}
	idxConn := p.conn.IndexConnection(namespace)

	total, err := upsertInChunks(ctx, idxConn, vectors)
	if err != nil {
		return nil, fmt.Errorf("pinecone-upsert: %w", err)
	}

	msg.Variables.Set(p.resultVar, total)
	return msg, nil
}

// upsertInChunks calls UpsertVectors once per upsertChunkSize-sized slice of
// vectors, summing the reported counts. Split out from Process so the
// chunk-boundary math is testable without a live index connection.
func upsertInChunks(ctx context.Context, idxConn *sdk.IndexConnection, vectors []*sdk.Vector) (uint32, error) {
	var total uint32
	for _, chunk := range chunkVectors(vectors, upsertChunkSize) {
		count, err := idxConn.UpsertVectors(ctx, chunk)
		if err != nil {
			return total, fmt.Errorf("upsert chunk: %w", err)
		}
		total += count
	}
	return total, nil
}

// chunkVectors splits vectors into consecutive slices of at most size
// elements.
func chunkVectors(vectors []*sdk.Vector, size int) [][]*sdk.Vector {
	if len(vectors) == 0 {
		return nil
	}
	chunks := make([][]*sdk.Vector, 0, (len(vectors)+size-1)/size)
	for start := 0; start < len(vectors); start += size {
		end := min(start+size, len(vectors))
		chunks = append(chunks, vectors[start:end])
	}
	return chunks
}

// toVector decodes one evaluated {id, values, metadata} object into a
// *sdk.Vector. id and values are required; metadata is optional.
func toVector(v any) (*sdk.Vector, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must be an object, got %T", v)
	}

	id, _ := m["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("missing required \"id\"")
	}

	values, err := toFloat32Slice(m["values"])
	if err != nil {
		return nil, fmt.Errorf("vector %q: \"values\" %w", id, err)
	}

	vec := &sdk.Vector{Id: id, Values: &values}
	if metaRaw, ok := m["metadata"].(map[string]any); ok {
		meta, err := sdk.NewMetadata(metaRaw)
		if err != nil {
			return nil, fmt.Errorf("vector %q: metadata: %w", id, err)
		}
		vec.Metadata = meta
	}
	return vec, nil
}
