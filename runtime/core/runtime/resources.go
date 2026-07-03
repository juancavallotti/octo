package runtime

import (
	"context"
	"fmt"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

// withTemplateAliases wraps base so template references resolve declared aliases
// (the "as" of each resources.templates entry) to their underlying resource id
// before loading. When no aliases are declared the base loader is returned
// unchanged, so the wrapper adds no overhead to configs that do not alias.
//
//nolint:ireturn // returns the ResourceLoader interface blocks depend on
func withTemplateAliases(base core.ResourceLoader, templates []types.TemplateResource) core.ResourceLoader {
	aliases := make(map[string]string)
	for _, t := range templates {
		if t.As != "" {
			aliases[t.As] = t.Resource
		}
	}
	if len(aliases) == 0 {
		return base
	}
	return aliasingLoader{base: base, aliases: aliases}
}

// aliasingLoader rewrites template resource ids through an alias map before
// delegating to the base loader. Non-template kinds and ids that are not declared
// aliases pass straight through, so direct-id template references keep working.
type aliasingLoader struct {
	base    core.ResourceLoader
	aliases map[string]string
}

func (a aliasingLoader) Load(ctx context.Context, kind core.ResourceKind, id string) ([]byte, error) {
	if kind == core.ResourceKindTemplate {
		if target, ok := a.aliases[id]; ok {
			id = target
		}
	}
	return a.base.Load(ctx, kind, id)
}

// warmTemplates loads and parses every declared template resource so a missing or
// malformed template fails at config load — the deployment — rather than when a
// message first renders it. loader is the base (unaliased) loader; templates are
// loaded by their id.
func warmTemplates(loader core.ResourceLoader, templates []types.TemplateResource) error {
	ctx := context.Background()
	for _, t := range templates {
		if t.Resource == "" {
			return fmt.Errorf("template resource requires a resource id")
		}
		data, err := loader.Load(ctx, core.ResourceKindTemplate, t.Resource)
		if err != nil {
			return fmt.Errorf("template resource %q: %w", t.Resource, err)
		}
		if _, err := expr.ParseTemplate(string(data)); err != nil {
			return fmt.Errorf("template resource %q: %w", t.Resource, err)
		}
	}
	return nil
}
