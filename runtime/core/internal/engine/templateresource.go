package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("template-resource", newTemplateResource)
}

// templateResourceSettings configures the template-resource block.
type templateResourceSettings struct {
	// ID is the template resource to load and render.
	ID string `json:"id"`
	// Target names the variable to store the rendered text in (readable later as
	// vars.<target>). When empty, the rendered text replaces the message body.
	Target string `json:"target"`
}

// templateResourceBlock renders a template resource against the message, writing
// the result to a variable (Target) or the message body.
type templateResourceBlock struct {
	id       string
	target   string
	registry *expr.TemplateRegistry
	env      map[string]any
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
	return &templateResourceBlock{
		id:       cfg.ID,
		target:   cfg.Target,
		registry: expr.NewTemplateRegistry(deps.Resources),
		env:      envActivation(deps.Env),
	}, nil
}

// Process renders the template and stores the result in the target variable, or
// replaces the message body when no target is set.
func (p *templateResourceBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	tpl, err := p.registry.Get(ctx, p.id)
	if err != nil {
		return nil, err
	}
	rendered, err := tpl.Render(messageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("template-resource %q: %w", p.id, err)
	}
	if p.target != "" {
		msg.Variables.Set(p.target, rendered)
	} else {
		msg.Body = rendered
	}
	return msg, nil
}
