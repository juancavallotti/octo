// This file provides the odoo blocks that change records: "odoo-create",
// "odoo-write" and "odoo-delete". Values are field names to values in Odoo's own
// write format, so a many2one takes an id and an x2many takes Odoo's command
// list, e.g. [[0, 0, {"product_id": 7, "product_uom_qty": 2}]] to add an order
// line.
package odoo

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	createBlock = connectorType + "-create"
	writeBlock  = connectorType + "-write"
	deleteBlock = connectorType + "-delete"
)

func registerCreate() {
	core.MustRegisterBlock(createBlock, newCreate)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     createBlock,
		Label:    "Odoo Create",
		Category: core.CategoryProcessor,
		Description: "Create one record of an Odoo model, or several from a list. The list of " +
			"new record ids becomes the message body, or a variable when one is named.",
		Config: reflect.TypeFor[createSettings](),
	})
}

func registerWrite() {
	core.MustRegisterBlock(writeBlock, newWrite)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     writeBlock,
		Label:    "Odoo Write",
		Category: core.CategoryProcessor,
		Description: "Update records of an Odoo model by id, setting the same values on each. " +
			"The result (true) becomes the message body, or a variable when one is named.",
		Config: reflect.TypeFor[writeSettings](),
	})
}

func registerDelete() {
	core.MustRegisterBlock(deleteBlock, newDelete)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     deleteBlock,
		Label:    "Odoo Delete",
		Category: core.CategoryProcessor,
		Description: "Delete records of an Odoo model by id. The result (true) becomes the " +
			"message body, or a variable when one is named.",
		Config: reflect.TypeFor[deleteSettings](),
	})
}

// createSettings is the odoo-create block's typed configuration.
type createSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "crm.lead".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression producing the new record's values, or a list of them to
	// create several records in one call.
	Values string `json:"values" octo:"label=Values,type=cel,required"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the new ids here and leave the body; when empty, they
	// become the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type createProcessor struct {
	target
	values *expr.Program
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newCreate(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg createSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(createBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	values, err := compileRequired(deps.Resources, createBlock, "values", cfg.Values)
	if err != nil {
		return nil, err
	}
	return &createProcessor{target: tgt, values: values}, nil
}

// Process always sends a list: Odoo answers a list with a list of ids, so one
// record and many come back in the same shape.
func (p *createProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	req, err := p.request(activation, "create")
	if err != nil {
		return nil, err
	}
	value, err := p.values.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("%s values: %w", createBlock, err)
	}
	var valsList []any
	switch v := value.(type) {
	case map[string]any:
		valsList = []any{v}
	case []any:
		valsList = v
	default:
		return nil, fmt.Errorf("%s values: want an object or a list of objects, got %T", createBlock, value)
	}
	req.Params = map[string]any{"vals_list": valsList}
	return p.run(ctx, msg, req)
}

// writeSettings is the odoo-write block's typed configuration.
type writeSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "crm.lead".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression for a record id or a list of them.
	IDs string `json:"ids" octo:"label=Record IDs,type=cel,required"`
	// CEL expression producing the values to set on every record.
	Values string `json:"values" octo:"label=Values,type=cel,required"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type writeProcessor struct {
	target
	ids    *expr.Program
	values *expr.Program
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newWrite(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg writeSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(writeBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	ids, err := compileRequired(deps.Resources, writeBlock, "ids", cfg.IDs)
	if err != nil {
		return nil, err
	}
	values, err := compileRequired(deps.Resources, writeBlock, "values", cfg.Values)
	if err != nil {
		return nil, err
	}
	return &writeProcessor{target: tgt, ids: ids, values: values}, nil
}

func (p *writeProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	req, err := p.request(activation, "write")
	if err != nil {
		return nil, err
	}
	if req.IDs, err = evalIDs(p.ids, activation); err != nil {
		return nil, fmt.Errorf("%s ids: %w", writeBlock, err)
	}
	vals, err := evalMap(p.values, activation)
	if err != nil {
		return nil, fmt.Errorf("%s values: %w", writeBlock, err)
	}
	req.Params = map[string]any{"vals": vals}
	return p.run(ctx, msg, req)
}

// deleteSettings is the odoo-delete block's typed configuration.
type deleteSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "crm.lead".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression for a record id or a list of them.
	IDs string `json:"ids" octo:"label=Record IDs,type=cel,required"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type deleteProcessor struct {
	target
	ids *expr.Program
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDelete(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg deleteSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(deleteBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	ids, err := compileRequired(deps.Resources, deleteBlock, "ids", cfg.IDs)
	if err != nil {
		return nil, err
	}
	return &deleteProcessor{target: tgt, ids: ids}, nil
}

func (p *deleteProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	req, err := p.request(activation, "unlink")
	if err != nil {
		return nil, err
	}
	if req.IDs, err = evalIDs(p.ids, activation); err != nil {
		return nil, fmt.Errorf("%s ids: %w", deleteBlock, err)
	}
	return p.run(ctx, msg, req)
}
