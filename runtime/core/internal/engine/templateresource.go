package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterBlock("template-resource", newTemplateResource)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "template-resource",
		Label:    "Template Resource",
		Category: core.CategoryProcessor,
		Group:    groupData,
		Icon:     "FileText",
		Description: "Render a declared template resource against the current message. Writes the " +
			"rendered text to the message body by default, or to a variable when a target is set. " +
			"The template is declared under resources.templates and embeds {{ CEL }} over body, " +
			"vars, env, now.",
		Config: reflect.TypeFor[templateResourceSettings](),
	})
}

// templateResourceSettings configures the template-resource block.
type templateResourceSettings struct {
	// The template to render: its resources.templates alias (the `as`), or its
	// resource path when unaliased.
	ID string `json:"id" octo:"label=Template,ref=template,required"`
	// Variable to store the rendered text in. Leave empty to replace the message
	// body.
	Target string `json:"target" octo:"label=Target variable"`
	// Write the rendered text as a raw-content body {contentType, rawData} instead
	// of a plain string body, so a connector (e.g. the HTTP source) serves it
	// verbatim. Applies to the body only, so leave Target variable empty.
	RawBody bool `json:"rawBody" octo:"label=Raw body,default=false"`
	// MIME type recorded on the raw body, e.g. text/html. Required when Raw body
	// is on.
	ContentType string `json:"contentType" octo:"label=Content type"`
}

// templateResourceBlock renders a template resource against the message, writing
// the result to a variable (Target) or the message body.
type templateResourceBlock struct {
	id          string
	target      string
	rawBody     bool
	contentType string
	registry    *expr.TemplateRegistry
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newTemplateResource(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg templateResourceSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.ID == "" {
		return nil, errors.New("template-resource requires an id")
	}
	if cfg.RawBody {
		if cfg.ContentType == "" {
			return nil, errors.New("template-resource requires a contentType when rawBody is set")
		}
		if cfg.Target != "" {
			return nil, errors.New("template-resource rawBody applies to the body, so target must be empty")
		}
	}
	return &templateResourceBlock{
		id:          cfg.ID,
		target:      cfg.Target,
		rawBody:     cfg.RawBody,
		contentType: cfg.ContentType,
		registry:    expr.NewTemplateRegistry(deps.Resources),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process renders the template and stores the result in the target variable, or
// replaces the message body when no target is set.
func (p *templateResourceBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	tpl, err := p.registry.Get(ctx, p.id)
	if err != nil {
		return nil, err
	}
	rendered, err := tpl.Render(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("template-resource %q: %w", p.id, err)
	}
	switch {
	case p.target != "":
		msg.Variables.Set(p.target, rendered)
	case p.rawBody:
		msg.SetRawBody(p.contentType, rendered)
	default:
		msg.Body = rendered
	}
	return msg, nil
}
