package ingest

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/juancavallotti/octo/logs/internal/cost"
)

// TraceSubject is the shared subject runtimes ship trace records to. Like
// LogSubject it mirrors a constant in the runtime's k8s services module, and the
// two must stay in sync by hand: the two modules do not share a go.mod, so this
// package cannot import the runtime's types and the wire shape below is a copy
// rather than the original.
const TraceSubject = "internal.traces"

// The record kinds the runtime publishes. They are the runtime's own vocabulary,
// spelled here so this package can reason about the handful that mean something
// to it — a model call to price, a terminal that decides a trace's status, an
// entry point to name it by. A kind not listed is still stored: the store's job
// is to keep what a runtime said, not to have an opinion about a version of the
// runtime newer than itself.
const (
	KindFlowStarted   = "flow.started"
	KindFlowCompleted = "flow.completed"
	KindFlowDropped   = "flow.dropped"
	KindFlowFailed    = "flow.failed"

	KindBlockPreInvoke  = "block.pre-invoke"
	KindBlockPostInvoke = "block.post-invoke"

	KindLLMTurn  = "llm.turn"
	KindLLMEmbed = "llm.embed"

	KindSourceReceive = "source.receive"
	KindSourceRespond = "source.respond"
	KindSourceEmit    = "source.emit"

	// KindTraceDropped is the in-band marker saying the publisher could not keep
	// some records. It is the one kind that carries neither a trace id nor a
	// timestamp, because it is about the stream rather than about any one trace.
	KindTraceDropped = "trace.dropped"
)

// TraceRecord is one decoded trace record ready to persist.
type TraceRecord struct {
	// Deployment identity, stamped by the emitting pod. AppVersion is the
	// deployment's tag as it was on that pod, which is why a record keeps the
	// exact integration version that produced it even after a rollout.
	DeploymentID string
	AppName      string
	AppVersion   string

	TraceID       string
	Seq           int64
	Kind          string
	EventID       string
	CorrelationID string
	Flow          string
	Path          string
	BlockType     string

	// Time is when the traced thing happened and DurationNs how long it took, so
	// the interval it occupies is [Time - DurationNs, Time]: the runtime stamps a
	// record at completion. DurationNs is zero for a record marking a point.
	Time       time.Time
	DurationNs int64

	Err       string
	Dropped   bool
	Truncated bool

	// Body, Vars and Attrs are stored as they arrived. Body and Vars are nil when
	// the runtime captured no payload, which is not the same as capturing an
	// empty one — see Truncated.
	Body  json.RawMessage
	Vars  json.RawMessage
	Attrs json.RawMessage

	// Model, Provider and Usage are lifted out of Attrs for the two model-call
	// kinds. Usage is nil when the provider reported none, and nil is not an empty
	// Usage: a provider staying silent and a provider charging nothing are
	// different facts.
	//
	// Provider is the vendor family the runtime stamped on the call. It is empty
	// for a record written before the runtime carried one, and for a third-party
	// connector that reports none — in both cases pricing falls back to the
	// provider of whichever rate matched the model.
	Model    string
	Provider string
	Usage    *cost.Usage
}

// ModelCall reports the call this record describes, and whether it describes one
// at all. Embeddings are flagged rather than merged: they are billed from a
// different rate card and carry no output or cache accounting, so pricing one as
// a completion would be wrong in a way the arithmetic never reveals.
func (r TraceRecord) ModelCall() (cost.Call, bool) {
	switch r.Kind {
	case KindLLMTurn:
		return cost.Call{Model: r.Model, Provider: r.Provider, Usage: r.Usage}, true
	case KindLLMEmbed:
		return cost.Call{Model: r.Model, Provider: r.Provider, Usage: r.Usage, Embedding: true}, true
	default:
		return cost.Call{}, false
	}
}

// TraceRow is one record ready to store: what arrived on the wire, together with
// what ingest worked out about it.
//
// It lives here rather than with the store because ingest is what produces it —
// decoding the record, resolving its integration and pricing its model call are
// all steps on the way in. The store only consumes it.
type TraceRow struct {
	Record TraceRecord
	// IntegrationID is empty when the deployment could not be resolved.
	IntegrationID string
	// Priced is the zero value for records that are not model calls.
	Priced cost.Priced
}

// traceWire is the record as it arrives: the runtime's TraceEvent flattened
// together with the deployment that emitted it, one JSON object per message.
type traceWire struct {
	Seq           uint64          `json:"seq"`
	Kind          string          `json:"kind"`
	TraceID       string          `json:"traceId"`
	EventID       string          `json:"eventId"`
	CorrelationID string          `json:"correlationId"`
	Flow          string          `json:"flow"`
	Path          string          `json:"path"`
	BlockType     string          `json:"blockType"`
	OccurredAt    time.Time       `json:"at"`
	DurationNs    int64           `json:"durationNs"`
	Err           string          `json:"error"`
	Dropped       bool            `json:"dropped"`
	Body          json.RawMessage `json:"body"`
	Vars          json.RawMessage `json:"vars"`
	Truncated     bool            `json:"truncated"`
	Attrs         json.RawMessage `json:"attrs"`

	DeploymentID string `json:"deploymentId"`
	AppName      string `json:"appName"`
	AppVersion   string `json:"appVersion"`
}

// Attribute names the runtime gives a model call. Only the two this package
// prices from are named; the rest travel through in Attrs untouched.
const (
	attrModel    = "model"
	attrProvider = "provider"
	attrUsage    = "usage"
)

// usageWire is the token accounting nested under attrs.usage. The counts are
// pointer-free ints because the runtime omits the whole object rather than
// individual counts when a provider reports nothing.
//
// The cost is the exception, and is a pointer for the opposite reason: the
// runtime writes it only when a provider volunteered one, so absent and zero
// have to stay apart all the way from the wire to the stored row.
type usageWire struct {
	InputTokens      int      `json:"inputTokens"`
	OutputTokens     int      `json:"outputTokens"`
	ThinkingTokens   int      `json:"thinkingTokens"`
	CachedTokens     int      `json:"cachedTokens"`
	CacheWriteTokens int      `json:"cacheWriteTokens"`
	ReportedCostUSD  *float64 `json:"reportedCostUSD"`
}

// parseTraceRecord decodes one shipped record.
//
// Two records are rejected: one without a deployment id, which could not be
// attributed to anything, and one without a kind, which could not be interpreted.
// Everything else is kept, including kinds this package does not know.
func parseTraceRecord(data []byte) (TraceRecord, error) {
	var wire traceWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return TraceRecord{}, fmt.Errorf("ingest: decode trace record: %w", err)
	}
	if wire.DeploymentID == "" {
		return TraceRecord{}, fmt.Errorf("ingest: trace record has no deploymentId")
	}
	if wire.Kind == "" {
		return TraceRecord{}, fmt.Errorf("ingest: trace record has no kind")
	}

	record := TraceRecord{
		DeploymentID:  wire.DeploymentID,
		AppName:       wire.AppName,
		AppVersion:    wire.AppVersion,
		TraceID:       wire.TraceID,
		Seq:           clampSeq(wire.Seq),
		Kind:          wire.Kind,
		EventID:       wire.EventID,
		CorrelationID: wire.CorrelationID,
		Flow:          wire.Flow,
		Path:          wire.Path,
		BlockType:     wire.BlockType,
		Time:          wire.OccurredAt,
		DurationNs:    wire.DurationNs,
		Err:           wire.Err,
		Dropped:       wire.Dropped,
		Truncated:     wire.Truncated,
		Body:          wire.Body,
		Vars:          wire.Vars,
		Attrs:         wire.Attrs,
	}

	// The dropped-records marker is published with no timestamp of its own, so
	// it arrives as the zero time. Stamping it here keeps the column non-null and
	// puts the marker in the stream where it was actually observed, which is the
	// only place it means anything.
	if record.Time.IsZero() {
		record.Time = time.Now()
	}

	if _, isModelCall := record.ModelCall(); isModelCall {
		record.Model, record.Provider, record.Usage = parseModelCall(wire.Attrs)
	}
	return record, nil
}

// parseModelCall lifts the model and token usage out of a record's attributes.
//
// It reads the two keys it needs individually rather than decoding the whole
// object into a type, so an attribute this package has never heard of — or one
// whose shape changed — cannot cost a record its usage. A record whose usage is
// unreadable is priced as though the provider reported none, which is the same
// answer as leaving it out and is honest about what is known.
func parseModelCall(attrs json.RawMessage) (string, string, *cost.Usage) {
	if len(attrs) == 0 {
		return "", "", nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(attrs, &fields); err != nil {
		return "", "", nil
	}

	var model string
	if raw, ok := fields[attrModel]; ok {
		_ = json.Unmarshal(raw, &model)
	}

	var provider string
	if raw, ok := fields[attrProvider]; ok {
		_ = json.Unmarshal(raw, &provider)
	}

	raw, ok := fields[attrUsage]
	if !ok {
		return model, provider, nil
	}
	var usage usageWire
	if err := json.Unmarshal(raw, &usage); err != nil {
		return model, provider, nil
	}
	return model, provider, &cost.Usage{
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		ThinkingTokens:   usage.ThinkingTokens,
		CachedTokens:     usage.CachedTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
		ReportedCostUSD:  usage.ReportedCostUSD,
	}
}

// clampSeq narrows the wire's unsigned counter to the signed column that holds
// it. The counter is per-process and would need to pass nine quintillion for this
// to bite; clamping rather than wrapping means that if it ever did, ordering
// degrades at the ceiling instead of inverting.
func clampSeq(seq uint64) int64 {
	if seq > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(seq)
}
