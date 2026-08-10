package repo

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/juancavallotti/octo/logs/internal/cost"
	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// The statuses a trace can end in, worst-first when two batches disagree. A trace
// that failed anywhere failed, whatever else it also did.
const (
	StatusOK      = "ok"
	StatusDropped = "dropped"
	StatusFailed  = "failed"
)

// How good a record is at naming what started a trace, lowest being best.
//
// This is a rank rather than a flag because the entry record does not arrive
// first, or even in the same batch as the records that need it: a trace's
// flow.started may be stored before its source.receive is. Comparing ranks lets a
// later batch correct an earlier guess while stopping a worse later one from
// overwriting a good earlier one — neither of which a first-writer-wins or
// last-writer-wins rule can do in both directions.
const (
	entryRankSourceReceive = 0
	entryRankSourceEmit    = 1
	entryRankFlowStarted   = 2
	// entryRankFallback marks a record that names no entry point. The identity
	// columns are NOT NULL, so the lowest-sequence record stands in for them
	// until something better arrives.
	entryRankFallback = 99
)

// Attribute names the runtime gives a source record, used to render what started
// a trace.
const (
	attrMethod   = "method"
	attrRoute    = "route"
	attrSchedule = "schedule"
)

// TraceRow is one record ready to store: what arrived on the wire, together with
// what ingest worked out about it.
type TraceRow struct {
	Record ingest.TraceRecord
	// IntegrationID is empty when the deployment could not be resolved.
	IntegrationID string
	// Priced is the zero value for records that are not model calls.
	Priced cost.Priced
}

// TraceDelta is what one batch of records contributes to a trace's summary.
//
// Every field is an aggregate that can be merged with another delta for the same
// trace without knowing which came first. That is not a nicety: records arrive
// out of sequence order and spread across batches, so any field depending on
// arrival order would give different answers on different runs of the same data.
type TraceDelta struct {
	TraceID string

	// Identity, taken from the best entry record this batch saw. EntryRank and
	// EntrySeq travel with them so a merge can tell whose identity is better.
	DeploymentID  string
	IntegrationID string
	AppName       string
	AppVersion    string
	EntryKind     string
	EntryLabel    string
	EntryRank     int
	EntrySeq      int64

	// DeploymentIDs is every deployment seen. A trace crosses process
	// boundaries: its id travels on the message and survives a queue hop.
	DeploymentIDs []string

	// RootFlow is the flow of the lowest-sequence record carrying one, and
	// RootFlowSeq is that record's sequence. It is chosen separately from the
	// entry identity because the record naming the entry point is usually not the
	// one naming the flow: source records carry no flow at all.
	RootFlow    string
	RootFlowSeq int64

	StartedAt time.Time
	EndedAt   time.Time

	Status         string
	RootDurationNs int64

	Records       int
	LLMCalls      int
	InputTokens   int64
	OutputTokens  int64
	CachedTokens  int64
	CostUSD       float64
	UnpricedCalls int

	Models []string

	// Bookkeeping for the "best so far" comparisons, not stored.
	hasIdentity bool
	hasRootFlow bool
}

// FoldTraces groups records into one delta per trace, in trace-id order.
//
// Records belonging to no trace are skipped rather than grouped under an empty
// id: the dropped-records marker describes the stream a publisher produced, not
// any one trace, and giving it a summary row would invent a trace that never ran.
//
// The output is sorted so a batch always upserts in the same order, which keeps
// two writers touching overlapping traces from deadlocking on each other.
func FoldTraces(rows []TraceRow) []TraceDelta {
	byTrace := make(map[string]*TraceDelta)
	for _, row := range rows {
		if row.Record.TraceID == "" {
			continue
		}
		delta, known := byTrace[row.Record.TraceID]
		if !known {
			delta = &TraceDelta{TraceID: row.Record.TraceID, EntryRank: entryRankFallback, Status: StatusOK}
			byTrace[row.Record.TraceID] = delta
		}
		delta.absorb(row)
	}

	out := make([]TraceDelta, 0, len(byTrace))
	for _, delta := range byTrace {
		sort.Strings(delta.Models)
		sort.Strings(delta.DeploymentIDs)
		out = append(out, *delta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TraceID < out[j].TraceID })
	return out
}

// absorb merges one record into the delta.
func (d *TraceDelta) absorb(row TraceRow) {
	d.Records++
	d.extendInterval(row.Record)
	d.considerIdentity(row)
	d.considerRootFlow(row.Record)
	d.considerOutcome(row.Record)
	d.considerModelCall(row)
	d.DeploymentIDs = appendUnique(d.DeploymentIDs, row.Record.DeploymentID)
}

// extendInterval widens the trace to contain this record.
//
// A record is stamped when the thing it describes finished, so it occupies
// [Time - DurationNs, Time]. Using that interval rather than the timestamp alone
// is what makes the summary's duration and a waterfall's span the same number by
// construction, instead of by two implementations agreeing.
func (d *TraceDelta) extendInterval(record ingest.TraceRecord) {
	began := record.Time.Add(-time.Duration(record.DurationNs))
	if d.StartedAt.IsZero() || began.Before(d.StartedAt) {
		d.StartedAt = began
	}
	if record.Time.After(d.EndedAt) {
		d.EndedAt = record.Time
	}
}

// considerIdentity keeps the identity of the best entry record seen so far.
func (d *TraceDelta) considerIdentity(row TraceRow) {
	record := row.Record
	rank := entryRankOf(record.Kind)
	if d.hasIdentity && !betterEntry(rank, record.Seq, d.EntryRank, d.EntrySeq) {
		return
	}

	d.hasIdentity = true
	d.EntryRank, d.EntrySeq = rank, record.Seq
	d.DeploymentID = record.DeploymentID
	d.IntegrationID = row.IntegrationID
	d.AppName = record.AppName
	d.AppVersion = record.AppVersion

	if rank == entryRankFallback {
		// This record names no entry point. Leave the label empty rather than
		// inventing one from whichever block happened to sort first.
		d.EntryKind, d.EntryLabel = "", ""
		return
	}
	d.EntryKind = record.Kind
	d.EntryLabel = entryLabel(record)
}

// betterEntry compares two candidates by rank, then by sequence.
func betterEntry(rank int, seq int64, bestRank int, bestSeq int64) bool {
	if rank != bestRank {
		return rank < bestRank
	}
	return seq < bestSeq
}

// considerRootFlow keeps the flow of the lowest-sequence record carrying one.
//
// Sequence is only comparable within one publisher, so for a trace that crossed
// deployments this takes the earliest flow of whichever process numbered it
// lowest. It names the trace in a list; nothing is computed from it.
func (d *TraceDelta) considerRootFlow(record ingest.TraceRecord) {
	if record.Flow == "" {
		return
	}
	if d.hasRootFlow && record.Seq >= d.RootFlowSeq {
		return
	}
	d.hasRootFlow = true
	d.RootFlowSeq = record.Seq
	d.RootFlow = record.Flow
}

// considerOutcome folds in how a flow ended. Worse outcomes win, so a trace that
// failed anywhere reads as failed however many of its flows completed.
func (d *TraceDelta) considerOutcome(record ingest.TraceRecord) {
	switch record.Kind {
	case ingest.KindFlowFailed:
		d.Status = StatusFailed
	case ingest.KindFlowDropped:
		if d.Status != StatusFailed {
			d.Status = StatusDropped
		}
	case ingest.KindFlowCompleted:
		// Deliberately leaves the status alone: completing is the default, and a
		// completed sub-flow must not clear a sibling's failure.
	default:
		return
	}

	// A flow-ref runs a whole flow inside a block of its caller, so its terminal
	// nests inside the caller's. The longest is the outermost, which is the one
	// the trace actually took.
	if record.DurationNs > d.RootDurationNs {
		d.RootDurationNs = record.DurationNs
	}
}

// considerModelCall folds in what a model call reported and cost.
func (d *TraceDelta) considerModelCall(row TraceRow) {
	if _, isCall := row.Record.ModelCall(); !isCall {
		return
	}
	d.LLMCalls++
	d.Models = appendUnique(d.Models, row.Record.Model)

	if usage := row.Record.Usage; usage != nil {
		d.InputTokens += int64(usage.InputTokens)
		d.OutputTokens += int64(usage.OutputTokens)
		d.CachedTokens += int64(usage.CachedTokens)
	}

	// A cost that is not known is counted apart from the total rather than added
	// as zero. Summing an unknown as nothing is what turns "we could not price
	// this" into "this was free" one addition later.
	amount, known := row.Priced.CostUSD()
	if !known {
		d.UnpricedCalls++
		return
	}
	d.CostUSD += amount
}

// entryRankOf scores how well a kind names what started a trace.
func entryRankOf(kind string) int {
	switch kind {
	case ingest.KindSourceReceive:
		return entryRankSourceReceive
	case ingest.KindSourceEmit:
		return entryRankSourceEmit
	case ingest.KindFlowStarted:
		return entryRankFlowStarted
	default:
		return entryRankFallback
	}
}

// entryLabel renders what started a trace, so a list can show it without opening
// the trace: the request that arrived, the schedule that fired, or the flow that
// ran.
func entryLabel(record ingest.TraceRecord) string {
	attrs := decodeAttrs(record.Attrs)
	switch record.Kind {
	case ingest.KindSourceReceive:
		method, _ := attrs[attrMethod].(string)
		route, _ := attrs[attrRoute].(string)
		return strings.TrimSpace(method + " " + route)
	case ingest.KindSourceEmit:
		schedule, _ := attrs[attrSchedule].(string)
		return schedule
	default:
		return record.Flow
	}
}

// decodeAttrs reads a record's attributes, yielding nothing rather than failing
// when they cannot be read: a label is worth less than the record carrying it.
func decodeAttrs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// appendUnique adds a value to a set, ignoring the empty string.
func appendUnique(set []string, value string) []string {
	if value == "" {
		return set
	}
	for _, existing := range set {
		if existing == value {
			return set
		}
	}
	return append(set, value)
}
