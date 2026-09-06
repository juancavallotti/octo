package repo

import (
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/cost"
	"github.com/juancavallotti/octo/observability/internal/ingest"
)

// shuffleRounds is how many orderings each fold is checked against. Records
// arrive out of sequence order — a publisher stamps the number and then queues
// the record — so a summary that depended on arrival order would summarize the
// same trace differently on different runs.
const shuffleRounds = 10

// costTolerance is how far two orderings of the same costs may land apart before
// the difference is the fold rather than the last bit of a float sum. Fractions
// of a cent below any price anyone bills at, and many orders of magnitude above
// the rounding it exists to absorb.
const costTolerance = 1e-12

var traceStart = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// at returns a timestamp offset from the trace's start, in milliseconds.
func at(ms int) time.Time { return traceStart.Add(time.Duration(ms) * time.Millisecond) }

func ms(n int) int64 { return int64(n) * int64(time.Millisecond) }

// record builds a row with the fields most tests care about.
func record(seq int64, kind string, opts ...func(*ingest.TraceRow)) ingest.TraceRow {
	row := ingest.TraceRow{
		Record: ingest.TraceRecord{
			TraceID:      "t1",
			Seq:          seq,
			Kind:         kind,
			DeploymentID: "dep-a",
			AppName:      "orders",
			AppVersion:   "v3",
			Time:         at(0),
		},
		IntegrationID: "int-a",
	}
	for _, opt := range opts {
		opt(&row)
	}
	return row
}

func endingAt(endMs int, durationMs int) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) {
		r.Record.Time = at(endMs)
		r.Record.DurationNs = ms(durationMs)
	}
}

func inFlow(flow string) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) { r.Record.Flow = flow }
}

func withAttrs(raw string) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) { r.Record.Attrs = json.RawMessage(raw) }
}

func fromDeployment(deployment, integration, app, version string) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) {
		r.Record.DeploymentID = deployment
		r.Record.AppName = app
		r.Record.AppVersion = version
		r.IntegrationID = integration
	}
}

func modelCall(model string, usage *cost.Usage, priced cost.Priced) func(*ingest.TraceRow) {
	return func(r *ingest.TraceRow) {
		r.Record.Model = model
		r.Record.Usage = usage
		r.Priced = priced
	}
}

// priced builds a pricing result carrying a known cost, the way cost.Table does.
// rateID is a real uuid because price_id is a uuid column: cost.Priced carries
// the llm_prices row id, and a stand-in string would fail the insert.
const rateID = "55555555-5555-5555-5555-555555555555"

func priced(amount float64, provider, priceID string) cost.Priced {
	table := cost.NewTable([]cost.Rate{{
		ID: priceID, Provider: provider, Pattern: "m", Operator: cost.OpEquals,
		InputPer1M: amount * 1e6, OutputPer1M: 0,
	}})
	return table.Price(cost.Call{Model: "m", Usage: &cost.Usage{InputTokens: 1}})
}

// foldOne folds rows and returns the single delta they must produce.
func foldOne(t *testing.T, rows []ingest.TraceRow) TraceDelta {
	t.Helper()
	got := FoldTraces(rows)
	if len(got) != 1 {
		t.Fatalf("folded into %d traces, want 1", len(got))
	}
	return got[0]
}

// foldEveryOrder folds the same rows in many orders and fails if any two
// orderings disagree, returning the delta they all produced.
func foldEveryOrder(t *testing.T, rows []ingest.TraceRow) TraceDelta {
	t.Helper()
	want := foldOne(t, rows)
	for round := range shuffleRounds {
		shuffled := make([]ingest.TraceRow, len(rows))
		copy(shuffled, rows)
		rand.New(rand.NewPCG(uint64(round), 0)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		got := foldOne(t, shuffled)
		// Cost is the one field that is a float sum, and floating-point addition
		// is not associative: the same three priced calls added in another order
		// can land a bit apart. Comparing its bits would make this test fail for
		// the arithmetic rather than for the ordering it is about.
		if diff := got.CostUSD - want.CostUSD; diff > costTolerance || diff < -costTolerance {
			t.Fatalf("ordering %d summed cost to %v, want %v", round, got.CostUSD, want.CostUSD)
		}
		got.CostUSD = want.CostUSD

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ordering %d summarized differently:\n got %+v\nwant %+v", round, got, want)
		}
	}
	return want
}

// The interval a trace occupies runs from the earliest thing that began to the
// latest thing that ended, and a record is stamped when it finished. Reading the
// timestamp alone would place the trace's start at its first completion.
func TestFoldSpansTheWholeInterval(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindSourceReceive, endingAt(0, 0)),
		record(2, ingest.KindBlockPostInvoke, endingAt(30, 25)),
		record(3, ingest.KindBlockPostInvoke, endingAt(90, 55)),
		record(4, ingest.KindFlowCompleted, endingAt(95, 95), inFlow("orders")),
	})

	// The flow terminal ends at 95ms having taken 95ms, so the trace began at 0.
	if !got.StartedAt.Equal(at(0)) {
		t.Errorf("started at %s, want %s", got.StartedAt, at(0))
	}
	if !got.EndedAt.Equal(at(95)) {
		t.Errorf("ended at %s, want %s", got.EndedAt, at(95))
	}
	if got.Records != 4 {
		t.Errorf("records = %d, want 4", got.Records)
	}
}

// A block that finished before anything else began still moves the trace's start
// back, which is only visible if the interval is used rather than the timestamp.
func TestFoldStartIsTheEarliestBeginning(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindBlockPostInvoke, endingAt(50, 40)),
		record(2, ingest.KindBlockPostInvoke, endingAt(20, 20)),
	})

	if !got.StartedAt.Equal(at(0)) {
		t.Fatalf("started at %s, want %s — the earliest record began at 0", got.StartedAt, at(0))
	}
}

func TestFoldEntryPoint(t *testing.T) {
	cases := []struct {
		name      string
		rows      []ingest.TraceRow
		wantKind  string
		wantLabel string
	}{
		{
			name: "an http request names itself by method and route",
			rows: []ingest.TraceRow{
				record(1, ingest.KindSourceReceive, withAttrs(`{"method":"POST","route":"/orders/{id}","status":201}`)),
				record(2, ingest.KindFlowStarted, inFlow("orders")),
			},
			wantKind:  ingest.KindSourceReceive,
			wantLabel: "POST /orders/{id}",
		},
		{
			name: "a schedule names itself by its expression",
			rows: []ingest.TraceRow{
				record(1, ingest.KindSourceEmit, withAttrs(`{"schedule":"0 */5 * * * *"}`)),
				record(2, ingest.KindFlowStarted, inFlow("sweep")),
			},
			wantKind:  ingest.KindSourceEmit,
			wantLabel: "0 */5 * * * *",
		},
		{
			// Nothing said how the trace was triggered, so the flow it ran is
			// the most specific honest answer.
			name: "a trace with no source falls back to the flow",
			rows: []ingest.TraceRow{
				record(1, ingest.KindFlowStarted, inFlow("orders")),
				record(2, ingest.KindBlockPostInvoke, inFlow("orders")),
			},
			wantKind:  ingest.KindFlowStarted,
			wantLabel: "orders",
		},
		{
			// A source record outranks a flow record even when it arrives later
			// and carries a higher sequence.
			name: "a source record outranks a flow record whatever the order",
			rows: []ingest.TraceRow{
				record(9, ingest.KindSourceReceive, withAttrs(`{"method":"GET","route":"/health"}`)),
				record(1, ingest.KindFlowStarted, inFlow("orders")),
			},
			wantKind:  ingest.KindSourceReceive,
			wantLabel: "GET /health",
		},
		{
			// Nothing here names an entry point, so no label is invented from a
			// block that happened to sort first.
			name: "blocks alone name no entry point",
			rows: []ingest.TraceRow{
				record(4, ingest.KindBlockPostInvoke, inFlow("orders")),
				record(5, ingest.KindBlockPostInvoke, inFlow("orders")),
			},
			wantKind:  "",
			wantLabel: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := foldEveryOrder(t, tc.rows)
			if got.EntryKind != tc.wantKind {
				t.Errorf("entry kind = %q, want %q", got.EntryKind, tc.wantKind)
			}
			if got.EntryLabel != tc.wantLabel {
				t.Errorf("entry label = %q, want %q", got.EntryLabel, tc.wantLabel)
			}
		})
	}
}

// The identity shown for a trace is the entry record's, not whichever record was
// folded last — which matters once a trace crosses deployments.
func TestFoldIdentityComesFromTheEntryRecord(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindSourceReceive,
			fromDeployment("dep-front", "int-front", "storefront", "v9"),
			withAttrs(`{"method":"POST","route":"/checkout"}`)),
		record(2, ingest.KindFlowStarted, inFlow("checkout"),
			fromDeployment("dep-front", "int-front", "storefront", "v9")),
		// The work continued in another deployment after a queue hop.
		record(1, ingest.KindBlockPostInvoke, inFlow("fulfil"),
			fromDeployment("dep-back", "int-back", "fulfilment", "v2")),
	})

	switch {
	case got.DeploymentID != "dep-front":
		t.Errorf("deployment = %q, want the entry's dep-front", got.DeploymentID)
	case got.IntegrationID != "int-front":
		t.Errorf("integration = %q, want int-front", got.IntegrationID)
	case got.AppName != "storefront" || got.AppVersion != "v9":
		t.Errorf("app = %s/%s, want storefront/v9", got.AppName, got.AppVersion)
	}

	// Every deployment the trace touched is still recorded.
	want := []string{"dep-back", "dep-front"}
	if !reflect.DeepEqual(got.DeploymentIDs, want) {
		t.Errorf("deployments = %v, want %v", got.DeploymentIDs, want)
	}
}

func TestFoldStatus(t *testing.T) {
	cases := []struct {
		name string
		rows []ingest.TraceRow
		want string
	}{
		{
			name: "a completed flow is ok",
			rows: []ingest.TraceRow{record(1, ingest.KindFlowCompleted, inFlow("orders"))},
			want: StatusOK,
		},
		{
			name: "a dropped flow is dropped",
			rows: []ingest.TraceRow{record(1, ingest.KindFlowDropped, inFlow("orders"))},
			want: StatusDropped,
		},
		{
			name: "a failed flow is failed",
			rows: []ingest.TraceRow{record(1, ingest.KindFlowFailed, inFlow("orders"))},
			want: StatusFailed,
		},
		{
			// A sub-flow completing must not clear the failure beside it.
			name: "a failure anywhere outranks everything else",
			rows: []ingest.TraceRow{
				record(1, ingest.KindFlowCompleted, inFlow("orders")),
				record(2, ingest.KindFlowFailed, inFlow("orders.charge")),
				record(3, ingest.KindFlowDropped, inFlow("orders.notify")),
			},
			want: StatusFailed,
		},
		{
			name: "a drop outranks a completion",
			rows: []ingest.TraceRow{
				record(1, ingest.KindFlowCompleted, inFlow("orders")),
				record(2, ingest.KindFlowDropped, inFlow("orders.filter")),
			},
			want: StatusDropped,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldEveryOrder(t, tc.rows); got.Status != tc.want {
				t.Fatalf("status = %q, want %q", got.Status, tc.want)
			}
		})
	}
}

// A flow reached through flow-ref times itself and nests inside its caller, so
// the longest terminal is the outermost — the duration the trace actually took.
func TestFoldRootDurationIsTheOutermostFlow(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindFlowCompleted, inFlow("orders"), endingAt(100, 100)),
		record(2, ingest.KindFlowCompleted, inFlow("orders.enrich"), endingAt(60, 40)),
	})

	if got.RootDurationNs != ms(100) {
		t.Fatalf("root duration = %dms, want 100ms", got.RootDurationNs/int64(time.Millisecond))
	}
}

func TestFoldModelCalls(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindSourceReceive),
		record(2, ingest.KindLLMTurn, modelCall("claude-3-5-sonnet-20241022",
			&cost.Usage{InputTokens: 1000, OutputTokens: 200, ThinkingTokens: 50, CachedTokens: 300},
			priced(0.01, "ANTHROPIC", rateID))),
		record(3, ingest.KindLLMTurn, modelCall("claude-3-5-sonnet-20241022",
			&cost.Usage{InputTokens: 500, OutputTokens: 100, CachedTokens: 0},
			priced(0.005, "ANTHROPIC", rateID))),
		record(4, ingest.KindLLMEmbed, modelCall("text-embedding-3-small",
			&cost.Usage{InputTokens: 900}, priced(0.002, "OPENAI", rateID))),
		record(5, ingest.KindBlockPostInvoke),
	})

	switch {
	case got.LLMCalls != 3:
		t.Errorf("llm calls = %d, want 3", got.LLMCalls)
	case got.InputTokens != 2400:
		t.Errorf("input tokens = %d, want 2400", got.InputTokens)
	case got.OutputTokens != 300:
		t.Errorf("output tokens = %d, want 300", got.OutputTokens)
	case got.CachedTokens != 300:
		t.Errorf("cached tokens = %d, want 300", got.CachedTokens)
	case got.UnpricedCalls != 0:
		t.Errorf("unpriced calls = %d, want 0", got.UnpricedCalls)
	}

	want := []string{"claude-3-5-sonnet-20241022", "text-embedding-3-small"}
	if !reflect.DeepEqual(got.Models, want) {
		t.Errorf("models = %v, want %v", got.Models, want)
	}
	if got.CostUSD < 0.0169 || got.CostUSD > 0.0171 {
		t.Errorf("cost = %v, want about 0.017", got.CostUSD)
	}
}

// A call whose cost is unknown is counted apart from the total, never added as
// zero. Summing an unknown as nothing turns "we could not price this" into "this
// was free" one addition later.
func TestFoldCountsUnpricedCallsApartFromTheTotal(t *testing.T) {
	var table *cost.Table // prices nothing

	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindLLMTurn, modelCall("a-known-model",
			&cost.Usage{InputTokens: 1000}, priced(0.01, "OPENAI", rateID))),
		record(2, ingest.KindLLMTurn, func(r *ingest.TraceRow) {
			r.Record.Model = "a-model-nobody-published"
			r.Record.Usage = &cost.Usage{InputTokens: 5000, OutputTokens: 2000}
			r.Priced = table.Price(cost.Call{Model: "a-model-nobody-published", Usage: r.Record.Usage})
		}),
		record(3, ingest.KindLLMTurn, func(r *ingest.TraceRow) {
			r.Record.Model = "a-silent-provider"
			r.Record.Usage = nil
			r.Priced = table.Price(cost.Call{Model: "a-silent-provider"})
		}),
	})

	if got.LLMCalls != 3 {
		t.Errorf("llm calls = %d, want 3", got.LLMCalls)
	}
	if got.UnpricedCalls != 2 {
		t.Errorf("unpriced calls = %d, want 2", got.UnpricedCalls)
	}
	if got.CostUSD < 0.0099 || got.CostUSD > 0.0101 {
		t.Errorf("cost = %v, want only the priced call's 0.01", got.CostUSD)
	}
	// Tokens are still counted for the unpriced call: the provider reported
	// them, and only the money is unknown.
	if got.InputTokens != 6000 || got.OutputTokens != 2000 {
		t.Errorf("tokens = %d/%d, want 6000/2000", got.InputTokens, got.OutputTokens)
	}
}

// The dropped-records marker describes the stream a publisher produced, not any
// one trace. Giving it a summary row would invent a trace that never ran.
func TestFoldSkipsRecordsBelongingToNoTrace(t *testing.T) {
	rows := []ingest.TraceRow{
		record(1, ingest.KindSourceReceive),
		func() ingest.TraceRow {
			row := record(2, ingest.KindTraceDropped, withAttrs(`{"dropped":5,"total":5}`))
			row.Record.TraceID = ""
			return row
		}(),
	}

	got := FoldTraces(rows)
	if len(got) != 1 {
		t.Fatalf("folded into %d traces, want 1", len(got))
	}
	if got[0].TraceID != "t1" {
		t.Fatalf("trace id = %q, want t1", got[0].TraceID)
	}
	if got[0].Records != 1 {
		t.Fatalf("records = %d, want 1 — the stream marker was counted", got[0].Records)
	}
}

// Deltas are emitted in a fixed order so a batch always upserts in the same
// sequence, which keeps two writers touching overlapping traces from deadlocking.
func TestFoldEmitsTracesInAStableOrder(t *testing.T) {
	rows := []ingest.TraceRow{}
	for _, id := range []string{"t-c", "t-a", "t-b"} {
		row := record(1, ingest.KindSourceReceive)
		row.Record.TraceID = id
		rows = append(rows, row)
	}

	for round := range shuffleRounds {
		shuffled := make([]ingest.TraceRow, len(rows))
		copy(shuffled, rows)
		rand.New(rand.NewPCG(uint64(round), 7)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		got := FoldTraces(shuffled)
		ids := []string{got[0].TraceID, got[1].TraceID, got[2].TraceID}
		if !reflect.DeepEqual(ids, []string{"t-a", "t-b", "t-c"}) {
			t.Fatalf("round %d emitted %v, want them sorted", round, ids)
		}
	}
}

// TestFoldChoosesTheSameEntryWhenSequencesTie covers the case that makes a
// sequence number a bad tie-break on its own.
//
// Sequence is unique within one publisher, not within a trace. A trace that
// crossed apps carries records numbered by two different processes, so two
// records of the same rank can share a number — and without a third term the
// winner would be whichever one happened to be read first.
func TestFoldChoosesTheSameEntryWhenSequencesTie(t *testing.T) {
	front := record(1, ingest.KindSourceReceive, fromDeployment("dep-a", "int-a", "front", "v1"),
		withAttrs(`{"method":"GET","route":"/a"}`))
	back := record(1, ingest.KindSourceReceive, fromDeployment("dep-b", "int-b", "back", "v2"),
		withAttrs(`{"method":"POST","route":"/b"}`))

	got := foldEveryOrder(t, []ingest.TraceRow{front, back})

	if got.DeploymentID != "dep-a" {
		t.Errorf("deployment = %q, want dep-a — the lower deployment id breaks the tie", got.DeploymentID)
	}
	if got.AppVersion != "v1" || got.EntryLabel != "GET /a" {
		t.Errorf("identity = %s/%q, want v1/GET /a", got.AppVersion, got.EntryLabel)
	}
}

// TestFoldChoosesTheSameRootFlowWhenSequencesTie is the same hole in the flow
// selection, which tracks its own sequence.
func TestFoldChoosesTheSameRootFlowWhenSequencesTie(t *testing.T) {
	got := foldEveryOrder(t, []ingest.TraceRow{
		record(4, ingest.KindFlowStarted, inFlow("refunds")),
		record(4, ingest.KindFlowStarted, inFlow("orders")),
	})
	if got.RootFlow != "orders" {
		t.Errorf("root flow = %q, want orders — the lower name breaks the tie", got.RootFlow)
	}
	if got.EntryLabel != "orders" {
		t.Errorf("entry label = %q, want orders — same tie, same answer", got.EntryLabel)
	}
}

// A cost the provider reported sums into the total and is not counted as
// unpriced. It is the most certain figure in the table, so a rollup that treated
// it as an unknown would understate a trace made entirely of such calls.
func TestFoldSumsAReportedCostAndDoesNotCallItUnpriced(t *testing.T) {
	reported := func(model string, usage *cost.Usage) func(*ingest.TraceRow) {
		return func(r *ingest.TraceRow) {
			r.Record.Model = model
			r.Record.Usage = usage
			r.Priced = cost.NewPricer().Price(cost.Call{
				Model: model, Provider: "OPENROUTER", Usage: usage,
			})
		}
	}
	amount := func(v float64) *float64 { return &v }

	got := foldEveryOrder(t, []ingest.TraceRow{
		record(1, ingest.KindLLMTurn, reported("anthropic/claude-sonnet-4.5",
			&cost.Usage{InputTokens: 1000, OutputTokens: 200, ReportedCostUSD: amount(0.004)})),
		record(2, ingest.KindLLMTurn, reported("openai/gpt-4o",
			&cost.Usage{InputTokens: 500, OutputTokens: 100, ReportedCostUSD: amount(0.001)})),
	})

	if got.UnpricedCalls != 0 {
		t.Errorf("unpriced calls = %d, want 0 — a reported cost is known", got.UnpricedCalls)
	}
	if got.CostUSD < 0.0049 || got.CostUSD > 0.0051 {
		t.Errorf("cost = %v, want the two reported costs summed to 0.005", got.CostUSD)
	}
	if got.InputTokens != 1500 || got.OutputTokens != 300 {
		t.Errorf("tokens = %d/%d, want 1500/300", got.InputTokens, got.OutputTokens)
	}
}
