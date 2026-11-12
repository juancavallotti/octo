// This file provides the "odoo-call" block: it calls any public method of any
// Odoo model with named arguments. It is the general form the other odoo blocks
// specialize, and the one to reach for when they do not fit — a workflow action
// (action_confirm, action_post), posting to a record's chatter (message_post),
// or a method a custom module adds.
package odoo

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const callBlock = connectorType + "-call"

func registerCall() {
	core.MustRegisterBlock(callBlock, newCall)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     callBlock,
		Label:    "Odoo Call",
		Category: core.CategoryProcessor,
		Description: "Call any method of an Odoo model, on the model or on records by id, with " +
			"named arguments. The method's return value becomes the message body, or a " +
			"variable when one is named.",
		Config: reflect.TypeFor[callSettings](),
	})
}

// callSettings is the odoo-call block's typed configuration.
type callSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "sale.order".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression for the method, e.g. "action_confirm".
	Method string `json:"method" octo:"label=Method,type=cel,required"`
	// CEL expression for a record id or a list of them; the method runs on those
	// records. Empty calls the method on the model itself.
	IDs string `json:"ids" octo:"label=Record IDs,type=cel"`
	// CEL expression producing the method's arguments by name, e.g.
	// {"body": "Shipped", "message_type": "comment"}.
	Params string `json:"params" octo:"label=Arguments,type=cel"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type callProcessor struct {
	target
	method *expr.Program
	ids    *expr.Program
	params *expr.Program
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newCall(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg callSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(callBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	method, err := compileRequired(deps.Resources, callBlock, "method", cfg.Method)
	if err != nil {
		return nil, err
	}
	ids, err := compileOptional(deps.Resources, callBlock, "ids", cfg.IDs)
	if err != nil {
		return nil, err
	}
	params, err := compileOptional(deps.Resources, callBlock, "params", cfg.Params)
	if err != nil {
		return nil, err
	}
	return &callProcessor{target: tgt, method: method, ids: ids, params: params}, nil
}

func (p *callProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	method, err := p.method.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("%s method: %w", callBlock, err)
	}
	req, err := p.request(activation, method)
	if err != nil {
		return nil, err
	}
	if p.ids != nil {
		if req.IDs, err = evalIDs(p.ids, activation); err != nil {
			return nil, fmt.Errorf("%s ids: %w", callBlock, err)
		}
	}
	if req.Params, err = evalMap(p.params, activation); err != nil {
		return nil, fmt.Errorf("%s params: %w", callBlock, err)
	}
	return p.run(ctx, msg, req)
}
