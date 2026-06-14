// Package log provides the "log" leaf block: a pass-through wire tap that logs a
// line for each message and forwards the message unchanged. The logged line is a
// CEL expression evaluated against the message, or the JSON body when no
// expression is configured.
package log

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterBlock("log", newLog)
}

// logProvider is the capability a log block needs from a logger connector: a
// configured slog.Logger to write through. It is matched structurally, so the
// log block stays decoupled from any specific connector implementation.
type logProvider interface {
	Logger() (*slog.Logger, error)
}

// settings is the log block's typed configuration.
type settings struct {
	// Message is a CEL expression rendered to the log line. When empty the JSON
	// body is logged.
	Message string `json:"message"`
	// Level is the level this block emits each line at: debug, info (default),
	// warn, or error. The logger's own minimum level still filters it.
	Level string `json:"level"`
	// Logger names a logger connector to write through. When empty the process
	// default logger is used.
	Logger string `json:"logger"`
}

// processor logs each message and passes it through unchanged. message is nil
// when no expression is configured, in which case the JSON body is logged.
type processor struct {
	level   slog.Level
	message *expr.Program
	logger  *slog.Logger
}

// newLog builds a log processor, resolving its logger and compiling the optional
// message expression once so a bad logger reference or expression fails at
// startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newLog(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg settings
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

	p := &processor{level: level, logger: logger}
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
	provider, ok := connector.(logProvider)
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
	p.logger.Log(ctx, p.level, line, "event_id", msg.EventID)
	return msg, nil
}

// render evaluates the message expression, or falls back to the JSON body when
// no expression is configured.
func (p *processor) render(msg *types.Message) (string, error) {
	if p.message == nil {
		raw, err := msg.BodyJSON()
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return p.message.EvalString(activation(msg))
}

// activation maps a message onto the variables a log expression can reference.
func activation(msg *types.Message) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
	}
}
