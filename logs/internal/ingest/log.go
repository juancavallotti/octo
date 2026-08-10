// Package ingest consumes the telemetry deployed runtimes publish over NATS and
// hands it to a store for persistence: log records on internal.logs, trace
// records on internal.traces. Parsing here is the inverse of the encoding each
// sink applies at the other end.
//
// Both streams live in one package because they arrive the same way — a shared,
// deployment-agnostic subject, consumed by a queue group so each record lands on
// exactly one replica, with the emitting deployment's identity inside the record
// rather than in the subject. Types are named for the stream they belong to,
// since neither is "the" one.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// LogSubject is the shared subject runtimes ship log records to. It mirrors the
// constant in the runtime's k8s services module; the two must stay in sync.
const LogSubject = "internal.logs"

// logQueueGroup makes every aggregator replica a competing consumer of LogSubject, so
// each record is delivered to exactly one replica (point-to-point).
const logQueueGroup = "octo-logs"

// Reserved JSON keys the slog sink emits for the built-in record fields and the
// deployment identity. Everything else in the record is preserved as attrs.
const (
	keyTime       = "time"
	keyLevel      = "level"
	keyMessage    = "msg"
	keyDeployment = "deploymentId"
	keyAppName    = "appName"
	keyAppVersion = "appVersion"
)

// LogEvent is a parsed log record ready to persist. Attrs holds the remaining
// structured fields (everything but the reserved keys) as a JSON object.
type LogEvent struct {
	DeploymentID string
	AppName      string
	AppVersion   string
	Time         time.Time
	Level        string
	Message      string
	Attrs        json.RawMessage
}

// LogStore persists parsed log events. The repo implements it; the consumer depends
// on the interface so it can be tested without a database.
type LogStore interface {
	Insert(ctx context.Context, e LogEvent) error
}

// parseLogEvent decodes a shipped slog JSON record into a LogEvent: it pulls the
// reserved keys into typed fields and keeps every other key in Attrs. A record
// missing the deployment id is rejected, since the row could not be attributed.
func parseLogEvent(data []byte) (LogEvent, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return LogEvent{}, fmt.Errorf("ingest: decode record: %w", err)
	}

	deploymentID := takeString(fields, keyDeployment)
	if deploymentID == "" {
		return LogEvent{}, fmt.Errorf("ingest: record has no %s", keyDeployment)
	}

	ev := LogEvent{
		DeploymentID: deploymentID,
		AppName:      takeString(fields, keyAppName),
		AppVersion:   takeString(fields, keyAppVersion),
		Level:        takeString(fields, keyLevel),
		Message:      takeString(fields, keyMessage),
	}

	if ts := takeString(fields, keyTime); ts != "" {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return LogEvent{}, fmt.Errorf("ingest: parse time %q: %w", ts, err)
		}
		ev.Time = t
	} else {
		// A record without its own timestamp is stamped at parse time so the row is
		// never NULL; received_at still records ingest time separately.
		ev.Time = time.Now()
	}

	// Whatever remains after removing the reserved keys is the structured payload.
	attrs, err := json.Marshal(fields)
	if err != nil {
		return LogEvent{}, fmt.Errorf("ingest: re-encode attrs: %w", err)
	}
	ev.Attrs = attrs
	return ev, nil
}

// takeString returns the string at key and deletes it from fields, so the caller
// is left with only the non-reserved keys. A non-string or absent value yields "".
func takeString(fields map[string]json.RawMessage, key string) string {
	raw, ok := fields[key]
	if !ok {
		return ""
	}
	delete(fields, key)
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
