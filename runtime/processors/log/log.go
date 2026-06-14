// Package log provides the "log" leaf block: a pass-through wire tap that logs a
// line for each message and forwards the message unchanged. The logged line is a
// CEL expression evaluated against the message, or the JSON body when no
// expression is configured.
package log

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterBlock("log", newLog)
}

// settings is the log block's typed configuration.
type settings struct {
	// Message is a CEL expression rendered to the log line. When empty the JSON
	// body is logged.
	Message string `json:"message"`
	// Level selects the slog level: debug, info (default), warn, or error.
	Level string `json:"level"`
}

// processor logs each message and passes it through unchanged. message is nil
// when no expression is configured, in which case the JSON body is logged.
type processor struct {
	level   slog.Level
	message *expr.Program
}

// newLog builds a log processor, compiling the optional message expression once
// so a malformed expression fails at startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newLog(raw types.Settings) (core.MessageProcessor, error) {
	var cfg settings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	p := &processor{level: level}
	if cfg.Message != "" {
		program, compileErr := expr.Compile(cfg.Message, "body", "vars", "eventID", "correlationID")
		if compileErr != nil {
			return nil, compileErr
		}
		p.message = program
	}

	return p, nil
}

// Process logs the rendered line at the configured level and returns the message
// unchanged.
func (p *processor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	line, err := p.render(msg)
	if err != nil {
		return nil, fmt.Errorf("render log message: %w", err)
	}
	slog.Default().Log(ctx, p.level, line, "event_id", msg.EventID)
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

// parseLevel maps the configured level name to an slog.Level, defaulting to
// info when empty.
func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(level) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log level %q is not one of debug/info/warn/error", level)
	}
}
