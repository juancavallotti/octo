package core

import (
	"context"
	"errors"
	"log/slog"
)

// LogShipper is an optional capability a RuntimeServices implementation may
// provide to ship log records to a central destination in addition to the
// process's normal output. Callers type-assert the active RuntimeServices to
// LogShipper and, when the assertion holds and LogSink returns a non-nil handler,
// tee their slog handler through it. LogSink may return nil, so callers MUST
// nil-check before wiring it.
type LogShipper interface {
	// LogSink returns a handler that forwards records to the central sink, or nil
	// when no logs are shipped.
	LogSink() slog.Handler
}

// TeeHandler returns an slog.Handler that fans every record out to each of
// handlers, so one logger can write to several destinations at once (e.g. the
// process's stderr plus a central log sink). Nil handlers are dropped, so a caller
// can pass an optional sink without a prior nil-check. A record is delivered to a
// child only when that child's Enabled reports the level, so children may apply
// independent level thresholds. Each child receives its own clone of the record,
// so a handler that mutates it cannot disturb the others.
//
//nolint:ireturn // returns the slog.Handler interface intentionally
func TeeHandler(handlers ...slog.Handler) slog.Handler {
	live := make([]slog.Handler, 0, len(handlers))
	for _, h := range handlers {
		if h != nil {
			live = append(live, h)
		}
	}
	return &teeHandler{handlers: live}
}

// teeHandler forwards records to a fixed set of child handlers.
type teeHandler struct {
	handlers []slog.Handler
}

// Enabled reports whether any child handles the level: if none would, the record
// can be skipped entirely.
func (t *teeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle delivers a clone of the record to every child whose level is enabled,
// joining any errors so one failing child does not hide another's success.
func (t *teeHandler) Handle(ctx context.Context, record slog.Record) error {
	var errs []error
	for _, h := range t.handlers {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		if err := h.Handle(ctx, record.Clone()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// WithAttrs returns a tee whose children each carry the attrs.
//
//nolint:ireturn // satisfies slog.Handler
func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		next[i] = h.WithAttrs(attrs)
	}
	return &teeHandler{handlers: next}
}

// WithGroup returns a tee whose children each open the group.
//
//nolint:ireturn // satisfies slog.Handler
func (t *teeHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return t
	}
	next := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		next[i] = h.WithGroup(name)
	}
	return &teeHandler{handlers: next}
}
