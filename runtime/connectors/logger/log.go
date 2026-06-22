// This file provides the "log" leaf block: a pass-through wire tap that logs a
// line for each message and forwards the message unchanged. The logged line is a
// CEL expression evaluated against the message, or a JSON object with the body
// and variables when no expression is configured. Setting "full" additionally
// attaches the whole
// message (correlation id, variables, body, schema) as structured attributes for
// debugging.
//
// The block lives in the logger connector's package: importing the connector
// registers the block too. A named logger binds to the connector by concrete
// type; with no name the process default logger is used.
package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("log", newLog)
}

// bodyKey is the attribute/field name the body is logged under.
const bodyKey = "body"

// logSettings is the log block's typed configuration.
type logSettings struct {
	// Message is a CEL expression rendered to the log line. When empty the JSON
	// body is logged.
	Message string `json:"message"`
	// Level is the level this block emits each line at: debug, info (default),
	// warn, or error. The logger's own minimum level still filters it.
	Level string `json:"level"`
	// Logger names a logger connector to write through. When empty the process
	// default logger is used.
	Logger string `json:"logger"`
	// Full, when true, attaches the entire message (correlation id, variables,
	// body, and schema) as structured log attributes, for debugging. The line
	// text still comes from Message, defaulting to "message" when none is set.
	Full bool `json:"full"`
}

// processor logs each message and passes it through unchanged. message is nil
// when no expression is configured, in which case the JSON body is logged. When
// full is set, the whole message is attached as structured attributes.
type processor struct {
	level   slog.Level
	message *expr.Program
	logger  *slog.Logger
	full    bool
}

// newLog builds a log processor, resolving its logger and compiling the optional
// message expression once so a bad logger reference or expression fails at
// startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newLog(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg logSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	level, err := core.ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	logger, err := resolveLogger(cfg.Logger, deps)
	if err != nil {
		return nil, err
	}

	p := &processor{level: level, logger: logger, full: cfg.Full}
	if cfg.Message != "" {
		program, compileErr := expr.Compile(cfg.Message, "body", "vars", "eventID", "correlationID")
		if compileErr != nil {
			return nil, compileErr
		}
		p.message = program
	}

	return p, nil
}

// resolveLogger binds the block to its logger: a named logger connector, or the
// process default logger when no name is given.
func resolveLogger(name string, deps core.BlockDeps) (*slog.Logger, error) {
	if name == "" {
		return slog.Default(), nil
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("logger %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("logger connector %q is not configured", name)
	}
	provider, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q does not provide a logger", name)
	}
	return provider.Logger()
}

// Process logs the rendered line at the configured level and returns the message
// unchanged.
func (p *processor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	line, err := p.render(msg)
	if err != nil {
		return nil, fmt.Errorf("render log message: %w", err)
	}
	attrs := []any{"event_id", msg.EventID}
	if p.full {
		attrs = append(attrs, messageAttrs(msg)...)
	}
	p.logger.Log(ctx, p.level, line, attrs...)
	return msg, nil
}

// render evaluates the message expression, or falls back to a JSON object with
// the message body and variables when no expression is configured (so a bare log
// block surfaces both, not just the body). In full mode without an expression the
// line is a fixed label, since the whole message is already attached as a
// structured attribute.
func (p *processor) render(msg *types.Message) (string, error) {
	if p.message != nil {
		return p.message.EvalString(activation(msg))
	}
	if p.full {
		return "message", nil
	}
	raw, err := json.Marshal(map[string]any{
		bodyKey: msg.Body,
		"vars":  map[string]any(msg.Variables),
	})
	if err != nil {
		return "", fmt.Errorf("marshal log message: %w", err)
	}
	return string(raw), nil
}

// messageAttrs renders the whole message as structured slog attributes. They
// serialize cleanly through a JSON logger and read well in text mode.
func messageAttrs(msg *types.Message) []any {
	attrs := []any{
		"correlation_id", msg.CorrelationID,
		"variables", map[string]any(msg.Variables),
		bodyKey, msg.Body,
	}
	if len(msg.BodySchema) > 0 {
		attrs = append(attrs, "body_schema", string(msg.BodySchema))
	}
	return attrs
}

// activation maps a message onto the variables a log expression can reference.
func activation(msg *types.Message) map[string]any {
	return map[string]any{
		bodyKey:         msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
	}
}
