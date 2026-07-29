// This file provides the "pinecone-delete" block: it deletes vectors from a
// Pinecone index by id, by metadata filter, or the whole namespace — exactly
// one of the three, chosen at build time.
package pinecone

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterBlock("pinecone-delete", newDelete)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "pinecone-delete",
		Label:    "Pinecone Delete",
		Category: core.CategoryProcessor,
		Description: "Delete vectors from a Pinecone index: by id, by metadata filter, or the whole " +
			"namespace. Set exactly one of ids, filter, or deleteAll.",
		Config: reflect.TypeFor[deleteSettings](),
	})
}

// deleteSettings is the pinecone-delete block's typed configuration. Exactly
// one of IDs, Filter, or DeleteAll must be set.
type deleteSettings struct {
	// Name of the pinecone connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pinecone"`
	// CEL expression evaluating to a list of vector ids to delete.
	IDs string `json:"ids" octo:"label=IDs,type=cel"`
	// CEL expression evaluating to a metadata filter selecting vectors to delete.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
	// CEL expression for the namespace to delete from; empty uses the connector default.
	Namespace string `json:"namespace" octo:"label=Namespace,type=cel"`
	// Delete every vector in the namespace.
	DeleteAll bool `json:"deleteAll" octo:"label=Delete all,default=false"`
}

// deleteMode selects which of Pinecone's three delete operations a
// deleteProcessor runs.
type deleteMode int

const (
	deleteByIDs deleteMode = iota
	deleteByFilter
	deleteAllInNamespace
)

// deleteProcessor runs one of the three delete operations, chosen by mode.
// Only the expression for the chosen mode is compiled.
type deleteProcessor struct {
	conn      *Connector
	mode      deleteMode
	ids       *expr.Program
	filter    *expr.Program
	namespace *expr.Program
	env       map[string]any
}

// newDelete builds a delete processor, resolving its pinecone connector,
// picking exactly one delete mode from the settings, and compiling that mode's
// expression once so a bad reference, an ambiguous mode, or a bad expression
// fails at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDelete(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg deleteSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("pinecone-delete: %w", err)
	}
	mode, err := resolveDeleteMode(cfg)
	if err != nil {
		return nil, fmt.Errorf("pinecone-delete: %w", err)
	}
	namespace, err := compileOptional(deps.Resources, cfg.Namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-delete: compile namespace: %w", err)
	}

	p := &deleteProcessor{conn: conn, mode: mode, namespace: namespace, env: expr.EnvActivation(deps.Env)}
	switch mode {
	case deleteByIDs:
		p.ids, err = compileRequired(deps.Resources, "pinecone-delete", "ids", cfg.IDs)
	case deleteByFilter:
		p.filter, err = compileRequired(deps.Resources, "pinecone-delete", "filter", cfg.Filter)
	case deleteAllInNamespace:
		// No expression to compile.
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

// resolveDeleteMode picks exactly one of ids/filter/deleteAll, erroring when
// zero or more than one is configured — an ambiguous combination fails at
// build time rather than silently picking one at runtime.
func resolveDeleteMode(cfg deleteSettings) (deleteMode, error) {
	hasIDs := strings.TrimSpace(cfg.IDs) != ""
	hasFilter := strings.TrimSpace(cfg.Filter) != ""

	set := 0
	for _, b := range []bool{hasIDs, hasFilter, cfg.DeleteAll} {
		if b {
			set++
		}
	}
	switch {
	case set == 0:
		return 0, fmt.Errorf(`set exactly one of "ids", "filter", or "deleteAll"`)
	case set > 1:
		return 0, fmt.Errorf(`set only one of "ids", "filter", or "deleteAll"`)
	case hasIDs:
		return deleteByIDs, nil
	case hasFilter:
		return deleteByFilter, nil
	default:
		return deleteAllInNamespace, nil
	}
}

// Process runs the configured delete operation against the resolved namespace.
func (p *deleteProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	namespace, err := evalNamespace(p.namespace, activation)
	if err != nil {
		return nil, fmt.Errorf("pinecone-delete: namespace: %w", err)
	}
	idxConn, err := p.conn.IndexConnection(namespace)
	if err != nil {
		return nil, fmt.Errorf("pinecone-delete: %w", err)
	}

	switch p.mode {
	case deleteByIDs:
		idsRaw, err := p.ids.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("pinecone-delete: ids: %w", err)
		}
		ids, err := toStringSlice(idsRaw)
		if err != nil {
			return nil, fmt.Errorf("pinecone-delete: ids %w", err)
		}
		if err := idxConn.DeleteVectorsById(ctx, ids); err != nil {
			return nil, fmt.Errorf("pinecone-delete: %w", err)
		}
	case deleteByFilter:
		filter, err := evalMetadataFilter(p.filter, activation)
		if err != nil {
			return nil, fmt.Errorf("pinecone-delete: %w", err)
		}
		if err := idxConn.DeleteVectorsByFilter(ctx, filter); err != nil {
			return nil, fmt.Errorf("pinecone-delete: %w", err)
		}
	case deleteAllInNamespace:
		if err := idxConn.DeleteAllVectorsInNamespace(ctx); err != nil {
			return nil, fmt.Errorf("pinecone-delete: %w", err)
		}
	}
	return msg, nil
}
