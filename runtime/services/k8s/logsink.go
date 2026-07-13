package k8s

import (
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/nats-io/nats.go"
)

// Services exposes a central log sink, so the runtime tees its loggers through it.
var _ core.LogShipper = (*Services)(nil)

// LogSubject is the shared, deployment-agnostic NATS subject every runtime ships
// its log records to. Unlike the queue subjects (octo.<id>.q.*), it is NOT scoped
// per deployment: a single log-aggregator consumes it as a competing consumer
// across all deployments, and the deployment id travels inside each record so the
// aggregator can attribute it.
const LogSubject = "internal.logs"

// newLogSink builds an slog.Handler that ships every record to LogSubject as one
// JSON line per record, tagged with the deployment id and (when set) its display
// name and version. It is a plain JSON handler over a NATS-publishing writer, so
// slog handles level/attr/group formatting and the deployment identity rides along
// as base attributes the aggregator denormalizes into columns. name and version
// are empty for an unnamed/untagged deployment; they are still emitted so the
// aggregator's parse is uniform.
//
// The sink imposes the lowest threshold (debug) so it never filters more than the
// destination it is teed with: the console/file handler keeps applying its own
// level, while the central store captures full fidelity.
//
//nolint:ireturn // returns the slog.Handler interface intentionally
func newLogSink(conn *nats.Conn, deploymentID, name, version string) slog.Handler {
	h := slog.NewJSONHandler(natsLogWriter{conn: conn}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return h.WithAttrs([]slog.Attr{
		slog.String("deploymentId", deploymentID),
		slog.String("appName", name),
		slog.String("appVersion", version),
	})
}

// natsLogWriter publishes each Write (one slog JSON record) to LogSubject. Shipping
// is fire-and-forget: a broker hiccup must never block or fail the caller's log
// call, so a publish error is intentionally dropped.
type natsLogWriter struct {
	conn *nats.Conn
}

// Write publishes a copy of p. slog reuses its formatting buffer after Write
// returns, and NATS may reference the payload past this call, so the bytes are
// copied to keep them stable.
func (w natsLogWriter) Write(p []byte) (int, error) {
	data := make([]byte, len(p))
	copy(data, p)
	_ = w.conn.Publish(LogSubject, data)
	return len(p), nil
}
