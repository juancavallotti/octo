// This file provides the "parallel-extract" block: it runs Parallel's Extract
// API (POST /v1/extract) and hands back the page contents Parallel read out of
// the URLs it was given.
//
// It is the read half of the web tools: search finds the pages, extract fetches
// what is on them. An objective is optional here — with one, Parallel returns
// excerpts scoped to it; without one, it returns the page. full_content asks for
// the whole page rather than excerpts.
package parallel

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerExtract() {
	core.MustRegisterBlock("parallel-extract", newExtract)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "parallel-extract",
		Label:    "Parallel Extract",
		Category: core.CategoryProcessor,
		Description: "Read the contents of web pages through a parallel connector, returning " +
			"the excerpts that bear on an objective or the full page.",
		Config: reflect.TypeFor[extractSettings](),
	})
}

// extractSettings is the parallel-extract block's typed configuration.
type extractSettings struct {
	// Name of the parallel connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:parallel"`
	// CEL expression for the URLs to read: one string, or a list.
	URLs string `json:"urls" octo:"label=URLs,type=cel,required"`
	// CEL expression describing, in natural language, what to read the pages for.
	// With it, Parallel returns excerpts scoped to it; without it, the page.
	Objective string `json:"objective" octo:"label=Objective,type=cel"`
	// Ask for the whole page rather than the objective-scoped excerpts.
	FullContent bool `json:"fullContent" octo:"label=Full content"`
	// When set, store the response here and leave the body; when empty, the response
	// becomes the body.
	ResultVar string `json:"resultVar" octo:"label=Result variable"`
	// Turn a Parallel API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// extractProcessor evaluates the URLs and optional objective and extracts.
type extractProcessor struct {
	conn        *Connector
	urls        *expr.Program
	objective   *expr.Program // nil when unset
	fixed       map[string]any
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newExtract(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg extractSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("parallel-extract: %w", err)
	}
	urls, err := compileRequired(deps.Resources, "parallel-extract", "urls", cfg.URLs)
	if err != nil {
		return nil, err
	}
	objective, err := compileOptional(deps.Resources, cfg.Objective)
	if err != nil {
		return nil, fmt.Errorf("parallel-extract: compile objective: %w", err)
	}

	return &extractProcessor{
		conn:        conn,
		urls:        urls,
		objective:   objective,
		fixed:       extractPayload(cfg),
		resultVar:   cfg.ResultVar,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// extractPayload folds the message-independent settings into the request fields
// they map to, once at build time.
func extractPayload(cfg extractSettings) map[string]any {
	payload := map[string]any{}
	putOptional(payload, "full_content", cfg.FullContent)
	return payload
}

// Process evaluates the URLs and optional objective and returns Parallel's response.
func (p *extractProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	rawURLs, err := p.urls.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("parallel-extract urls: %w", err)
	}
	urls, err := toStringSlice(rawURLs)
	if err != nil {
		return nil, fmt.Errorf("parallel-extract urls %w", err)
	}

	payload := maps.Clone(p.fixed)
	payload["urls"] = urls
	if p.objective != nil {
		objective, err := p.objective.EvalString(activation)
		if err != nil {
			return nil, fmt.Errorf("parallel-extract objective: %w", err)
		}
		putOptional(payload, "objective", objective)
	}

	resp, err := p.conn.Call(ctx, "v1/extract", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return deliver(msg, p.resultVar, resp), nil
}
