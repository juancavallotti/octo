// This file provides the odoo blocks that read records: "odoo-search-read",
// which finds records matching a domain, and "odoo-read", which reads records by
// id. Both return a list of objects holding the requested fields, as Odoo
// produced them: a many2one field is [id, "Name"], an x2many field a list of ids,
// and an empty value false.
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
	searchReadBlock = connectorType + "-search-read"
	readBlock       = connectorType + "-read"
)

func registerSearchRead() {
	core.MustRegisterBlock(searchReadBlock, newSearchRead)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     searchReadBlock,
		Label:    "Odoo Search Read",
		Category: core.CategoryProcessor,
		Description: "Find the records of an Odoo model that match a domain and read their " +
			"fields. The list of records becomes the message body, or a variable when one is named.",
		Config: reflect.TypeFor[searchReadSettings](),
	})
}

func registerRead() {
	core.MustRegisterBlock(readBlock, newRead)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     readBlock,
		Label:    "Odoo Read",
		Category: core.CategoryProcessor,
		Description: "Read records of an Odoo model by id. The list of records becomes the " +
			"message body, or a variable when one is named.",
		Config: reflect.TypeFor[readSettings](),
	})
}

// searchReadSettings is the odoo-search-read block's typed configuration.
type searchReadSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "res.partner".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression producing an Odoo domain, e.g. [["email", "=", body.email]];
	// empty matches every record.
	Domain string `json:"domain" octo:"label=Domain,type=cel"`
	// Fields to read; empty reads every field, which on most models is a lot.
	Fields []string `json:"fields" octo:"label=Fields,type=string-list"`
	// Most records to return; 0 returns them all.
	Limit int `json:"limit" octo:"label=Limit"`
	// Records to skip before returning any.
	Offset int `json:"offset" octo:"label=Offset"`
	// Sort specification, e.g. "create_date desc, id".
	Order string `json:"order" octo:"label=Order"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type searchReadProcessor struct {
	target
	domain *expr.Program
	params map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSearchRead(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg searchReadSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(searchReadBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	if cfg.Limit < 0 || cfg.Offset < 0 {
		return nil, fmt.Errorf("%s: limit and offset must not be negative", searchReadBlock)
	}
	domain, err := compileOptional(deps.Resources, searchReadBlock, "domain", cfg.Domain)
	if err != nil {
		return nil, err
	}
	// The arguments that do not depend on the message are built once.
	params := map[string]any{}
	if len(cfg.Fields) > 0 {
		params["fields"] = cfg.Fields
	}
	if cfg.Limit > 0 {
		params["limit"] = cfg.Limit
	}
	if cfg.Offset > 0 {
		params["offset"] = cfg.Offset
	}
	if cfg.Order != "" {
		params["order"] = cfg.Order
	}
	return &searchReadProcessor{target: tgt, domain: domain, params: params}, nil
}

func (p *searchReadProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	req, err := p.request(activation, "search_read")
	if err != nil {
		return nil, err
	}
	req.Params = make(map[string]any, len(p.params)+1)
	for k, v := range p.params {
		req.Params[k] = v
	}
	domain := []any{}
	if p.domain != nil {
		value, err := p.domain.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("%s domain: %w", searchReadBlock, err)
		}
		list, ok := value.([]any)
		if !ok && value != nil {
			return nil, fmt.Errorf("%s domain: want a list, got %T", searchReadBlock, value)
		}
		if list != nil {
			domain = list
		}
	}
	req.Params["domain"] = domain
	return p.run(ctx, msg, req)
}

// readSettings is the odoo-read block's typed configuration.
type readSettings struct {
	// Name of the odoo connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:odoo"`
	// CEL expression for the model, e.g. "res.partner".
	Model string `json:"model" octo:"label=Model,type=cel,required"`
	// CEL expression for a record id or a list of them.
	IDs string `json:"ids" octo:"label=Record IDs,type=cel,required"`
	// Fields to read; empty reads every field.
	Fields []string `json:"fields" octo:"label=Fields,type=string-list"`
	// CEL expression producing the call context, e.g. {"lang": "es_AR"}.
	Context string `json:"context" octo:"label=Context,type=cel"`
	// When set, store the result here and leave the body; when empty, the result
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn an Odoo error into a flow error; when off, the message passes through
	// unchanged.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

type readProcessor struct {
	target
	ids    *expr.Program
	fields []string
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRead(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg readSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	tgt, err := newTarget(readBlock, deps, cfg.Connector, cfg.Model, cfg.Context, cfg.ResultVar, cfg.FailOnError)
	if err != nil {
		return nil, err
	}
	ids, err := compileRequired(deps.Resources, readBlock, "ids", cfg.IDs)
	if err != nil {
		return nil, err
	}
	return &readProcessor{target: tgt, ids: ids, fields: cfg.Fields}, nil
}

func (p *readProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	req, err := p.request(activation, "read")
	if err != nil {
		return nil, err
	}
	if req.IDs, err = evalIDs(p.ids, activation); err != nil {
		return nil, fmt.Errorf("%s ids: %w", readBlock, err)
	}
	if len(p.fields) > 0 {
		req.Params = map[string]any{"fields": p.fields}
	}
	return p.run(ctx, msg, req)
}
