package api

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
)

// Services ships logs when the platform accepts them, so the runtime tees its
// loggers through it.
var _ core.LogShipper = (*Services)(nil)

// LogSink returns the handler that ships log records to the platform API, or nil
// when the platform does not accept them — which is how a module says it ships no
// logs, and what teeDefaultLoggerToSink already checks for.
//
//nolint:ireturn // satisfies core.LogShipper
func (s *Services) LogSink() slog.Handler { return s.logSink }

// newLogSink builds an slog.Handler that ships every record to the platform as
// one JSON object per record, tagged with the deployment and instance it came
// from.
//
// It is a plain JSON handler over a shipping writer, so slog does the
// level/attr/group formatting and the identity rides along as base attributes.
// The threshold is debug so it never filters more than the destination it is teed
// with: the console handler keeps applying its own level, while the platform
// captures full fidelity.
//
//nolint:ireturn // returns the slog.Handler interface intentionally
func newLogSink(c *client, cfg Config) slog.Handler {
	h := slog.NewJSONHandler(logWriter{c: c}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return h.WithAttrs([]slog.Attr{
		slog.String("deploymentId", cfg.DeploymentID),
		slog.String("instance", cfg.InstanceID),
	})
}

// logRecordBatch is what goes on the wire. Each record is the raw JSON slog
// produced, so the platform sees the same object it would have read from a file.
type logRecordBatch struct {
	Records []json.RawMessage `json:"records"`
}

// logWriter ships each Write — one slog JSON record — to the platform.
//
// Shipping is fire-and-forget and a publish error is deliberately dropped: a
// platform hiccup must never block or fail a caller's log call. That is also why
// it does not batch. A batching writer would need a flush goroutine and a
// shutdown ordering, and the thing it would protect — the request rate of a log
// stream — is under the platform's own control through the response it returns.
type logWriter struct{ c *client }

// Write ships a copy of p. slog reuses its formatting buffer after Write returns,
// so the bytes are copied to keep them stable for the request.
func (w logWriter) Write(p []byte) (int, error) {
	record := make([]byte, len(p))
	copy(record, p)

	ctx, cancel := context.WithTimeout(context.Background(), shipTimeout)
	defer cancel()
	//nolint:errcheck // deliberately dropped: see the type comment
	_ = w.c.json(ctx, routeLogs, w.c.url(routeLogs),
		logRecordBatch{Records: []json.RawMessage{record}}, nil, shipTimeout)
	return len(p), nil
}
