// This file provides the "parallel-task-run" block: it starts one of Parallel's
// asynchronous research runs (POST /v1/tasks/runs) and hands back the run handle.
//
// The block does not wait. A task run takes far longer than an HTTP request, so
// what comes back is {run_id, status, ...} — the answer arrives later, as a
// webhook Parallel posts to the URL configured here. Point that URL at an http
// source in this same service and authenticate it with parallel-verify-request.
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

func registerTaskRun() {
	core.MustRegisterBlock("parallel-task-run", newTaskRun)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "parallel-task-run",
		Label:    "Parallel Task Run",
		Category: core.CategoryProcessor,
		Description: "Start an asynchronous Parallel task run and return its handle; the result " +
			"arrives later on the configured webhook.",
		Config: reflect.TypeFor[taskRunSettings](),
	})
}

const (
	// defaultRunVar names the variable the run handle is stored in.
	defaultRunVar = "parallelRun"
	// defaultEventType is the only event Parallel's task API emits today, and the
	// one a flow waiting on a run cares about.
	defaultEventType = "task_run.status"
)

// taskRunSettings is the parallel-task-run block's typed configuration.
type taskRunSettings struct {
	// Name of the parallel connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:parallel"`
	// Parallel processor to run the task on; it selects the depth/cost tier.
	Processor string `json:"processor" octo:"label=Processor,required"`
	// CEL expression for the task input: a string, or an object matching the
	// input schema.
	Input string `json:"input" octo:"label=Input,type=cel,required"`
	// CEL expression for the output the task must produce: a JSON Schema object,
	// or a plain-English description of the answer you want.
	OutputSchema string `json:"outputSchema" octo:"label=Output schema,type=cel"`
	// CEL expression for key/value metadata echoed back on the run and its webhook.
	Metadata string `json:"metadata" octo:"label=Metadata,type=cel"`
	// URL Parallel posts the run's result to. Point it at an http source in this
	// service and authenticate the request with parallel-verify-request.
	WebhookURL string `json:"webhookURL" octo:"label=Webhook URL"`
	// Event types to deliver to the webhook.
	EventTypes []string `json:"eventTypes" octo:"label=Event types"`
	// Variable the run handle is stored in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=parallelRun"`
	// Turn a Parallel API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// taskRunProcessor starts a task run and stores its handle.
type taskRunProcessor struct {
	conn         *Connector
	input        *expr.Program
	outputSchema *expr.Program
	metadata     *expr.Program
	fixed        map[string]any
	resultVar    string
	failOnError  bool
	env          map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newTaskRun(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg taskRunSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("parallel-task-run: %w", err)
	}
	if cfg.Processor == "" {
		return nil, fmt.Errorf("parallel-task-run requires a %q setting", "processor")
	}
	input, err := compileRequired(deps.Resources, "parallel-task-run", "input", cfg.Input)
	if err != nil {
		return nil, err
	}
	outputSchema, err := compileOptional(deps.Resources, cfg.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("parallel-task-run: compile outputSchema: %w", err)
	}
	metadata, err := compileOptional(deps.Resources, cfg.Metadata)
	if err != nil {
		return nil, fmt.Errorf("parallel-task-run: compile metadata: %w", err)
	}

	return &taskRunProcessor{
		conn:         conn,
		input:        input,
		outputSchema: outputSchema,
		metadata:     metadata,
		fixed:        taskRunPayload(cfg),
		resultVar:    orDefault(cfg.ResultVar, defaultRunVar),
		failOnError:  failOnErrorDefault(cfg.FailOnError),
		env:          expr.EnvActivation(deps.Env),
	}, nil
}

// taskRunPayload folds the message-independent settings into the request fields
// they map to, once at build time.
func taskRunPayload(cfg taskRunSettings) map[string]any {
	payload := map[string]any{"processor": cfg.Processor}
	if cfg.WebhookURL != "" {
		webhook := map[string]any{"url": cfg.WebhookURL}
		types := cfg.EventTypes
		if len(types) == 0 {
			types = []string{defaultEventType}
		}
		webhook["event_types"] = types
		payload["webhook"] = webhook
	}
	return payload
}

// Process starts the run and stores its handle. The handle is put in a variable
// rather than the body — unlike every other block here — because it is not the
// answer: it is a receipt, and the flow that asked usually still wants its own
// body to respond with.
func (p *taskRunProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	input, err := p.input.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("parallel-task-run input: %w", err)
	}
	payload := maps.Clone(p.fixed)
	payload["input"] = input

	if err := p.applySchemaAndMetadata(payload, activation); err != nil {
		return nil, err
	}

	resp, err := p.conn.Call(ctx, "v1/tasks/runs", payload)
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	msg.Variables.Set(p.resultVar, resp)
	return msg, nil
}

// applySchemaAndMetadata evaluates the two optional object expressions onto the
// payload, each of which must yield an object.
func (p *taskRunProcessor) applySchemaAndMetadata(payload, activation map[string]any) error {
	schema, err := evalOutputSchema(p.outputSchema, activation)
	if err != nil {
		return fmt.Errorf("parallel-task-run outputSchema: %w", err)
	}
	if schema != nil {
		payload["task_spec"] = map[string]any{"output_schema": schema}
	}
	metadata, err := evalObject(p.metadata, activation)
	if err != nil {
		return fmt.Errorf("parallel-task-run metadata: %w", err)
	}
	if metadata != nil {
		payload["metadata"] = metadata
	}
	return nil
}

// evalOutputSchema evaluates the optional output-schema expression into the
// envelope the Task API expects.
//
// output_schema is not a bare JSON Schema: it is a tagged union, and a JSON
// Schema has to arrive wrapped as {type: "json", json_schema: {...}}. Passing the
// schema through unwrapped is rejected, because the object's own "type" (usually
// "object") is not one of the union's tags.
//
// A string is passed through untouched — the API documents a bare string as a
// text schema, which is the cheapest way to say "answer me in prose".
func evalOutputSchema(program *expr.Program, activation map[string]any) (any, error) {
	if program == nil {
		return nil, nil
	}
	raw, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	switch schema := raw.(type) {
	case string:
		if schema == "" {
			return nil, nil
		}
		return schema, nil
	case map[string]any:
		return map[string]any{"type": "json", "json_schema": schema}, nil
	default:
		return nil, fmt.Errorf("must evaluate to a JSON Schema object or a description string, got %T", raw)
	}
}

// evalObject evaluates an optional expression that must yield an object,
// returning nil when the program is unset.
func evalObject(program *expr.Program, activation map[string]any) (map[string]any, error) {
	if program == nil {
		return nil, nil
	}
	raw, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("must evaluate to an object, got %T", raw)
	}
	return obj, nil
}
