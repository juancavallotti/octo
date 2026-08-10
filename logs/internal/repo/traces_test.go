package repo

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/cost"
	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// The deployment and integration columns are uuid, so stored records need real
// ones — unlike the pure fold tests, where any string will do.
const (
	depFront = "11111111-1111-1111-1111-111111111111"
	depBack  = "22222222-2222-2222-2222-222222222222"
	intFront = "33333333-3333-3333-3333-333333333333"
	intBack  = "44444444-4444-4444-4444-444444444444"
)

// newTestTraces opens a pool against TEST_DATABASE_URL, skipping when it is unset
// so `go test ./...` stays green without a database. Each test writes under a
// trace id of its own, so cleanup is two deletes and concurrent runs never
// collide.
func newTestTraces(t *testing.T) (*Traces, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run trace store tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	traceID := "trace-" + t.Name()
	t.Cleanup(func() {
		ctx := context.Background()
		// Scoped by deployment as well as by trace id, because a test may write
		// rows carrying neither: the dropped-records marker belongs to no trace,
		// so cleaning up by trace id alone would leave it behind to fail the next
		// run of the very test that asserts it creates nothing.
		_, _ = pool.Exec(ctx, `DELETE FROM traces WHERE trace_id = $1 OR deployment_id = $2::uuid`, traceID, depFront)
		_, _ = pool.Exec(ctx,
			`DELETE FROM trace_summaries WHERE trace_id = $1 OR deployment_id = $2::uuid`, traceID, depFront)
		pool.Close()
	})
	return NewTraces(pool), traceID
}

// storedSummary is the rollup as the database holds it.
type storedSummary struct {
	DeploymentID   string
	DeploymentIDs  []string
	IntegrationID  *string
	AppName        string
	AppVersion     string
	StartedAt      time.Time
	EndedAt        time.Time
	RootFlow       string
	EntryKind      string
	EntryLabel     string
	EntryRank      int
	Status         string
	RootDurationNs int64
	Records        int
	LLMCalls       int
	InputTokens    int64
	OutputTokens   int64
	CachedTokens   int64
	CostUSD        float64
	UnpricedCalls  int
	Models         []string
}

func readSummary(t *testing.T, store *Traces, traceID string) storedSummary {
	t.Helper()
	var got storedSummary
	err := store.pool.QueryRow(context.Background(),
		`SELECT deployment_id, deployment_ids, integration_id, app_name, app_version,
		        started_at, ended_at, root_flow, entry_kind, entry_label, entry_rank,
		        status, root_duration_ns, records, llm_calls,
		        input_tokens, output_tokens, cached_tokens, cost_usd, unpriced_calls, models
		   FROM trace_summaries WHERE trace_id = $1`, traceID).Scan(
		&got.DeploymentID, &got.DeploymentIDs, &got.IntegrationID, &got.AppName, &got.AppVersion,
		&got.StartedAt, &got.EndedAt, &got.RootFlow, &got.EntryKind, &got.EntryLabel, &got.EntryRank,
		&got.Status, &got.RootDurationNs, &got.Records, &got.LLMCalls,
		&got.InputTokens, &got.OutputTokens, &got.CachedTokens, &got.CostUSD, &got.UnpricedCalls, &got.Models)
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	return got
}

// stored builds a record with a real deployment uuid.
func storedRecord(traceID string, seq int64, kind string, opts ...func(*TraceRow)) TraceRow {
	row := TraceRow{
		Record: ingest.TraceRecord{
			TraceID:      traceID,
			Seq:          seq,
			Kind:         kind,
			DeploymentID: depFront,
			AppName:      "storefront",
			AppVersion:   "v9",
			Time:         at(0),
		},
		IntegrationID: intFront,
	}
	for _, opt := range opts {
		opt(&row)
	}
	return row
}

func TestTracesInsertStoresARecord(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	row := storedRecord(traceID, 12, ingest.KindBlockPostInvoke, endingAt(30, 25), inFlow("orders"))
	row.Record.EventID = "e1"
	row.Record.CorrelationID = "c1"
	row.Record.Path = "orders.charge"
	row.Record.BlockType = "rest"
	row.Record.Body = json.RawMessage(`{"id":7}`)
	row.Record.Vars = json.RawMessage(`{"tenant":"acme"}`)
	row.Record.Attrs = json.RawMessage(`{"status":200}`)

	if err := store.Insert(ctx, []TraceRow{row}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var (
		kind, eventID, correlationID, flow, path, blockType string
		seq, durationNs                                     int64
		body, vars, attrs                                   json.RawMessage
		integrationID                                       string
	)
	err := store.pool.QueryRow(ctx,
		`SELECT kind, seq, event_id, correlation_id, flow, path, block_type, duration_ns,
		        body, vars, attrs, integration_id
		   FROM traces WHERE trace_id = $1`, traceID).Scan(
		&kind, &seq, &eventID, &correlationID, &flow, &path, &blockType, &durationNs,
		&body, &vars, &attrs, &integrationID)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}

	switch {
	case kind != ingest.KindBlockPostInvoke || seq != 12:
		t.Errorf("kind/seq = %s/%d", kind, seq)
	case eventID != "e1" || correlationID != "c1":
		t.Errorf("ids = %s/%s", eventID, correlationID)
	case flow != "orders" || path != "orders.charge" || blockType != "rest":
		t.Errorf("address = %s/%s/%s", flow, path, blockType)
	case durationNs != ms(25):
		t.Errorf("duration = %d, want %d", durationNs, ms(25))
	case string(body) != `{"id": 7}` && string(body) != `{"id":7}`:
		t.Errorf("body = %s", body)
	case integrationID != intFront:
		t.Errorf("integration = %s, want %s", integrationID, intFront)
	}
}

// Absent and empty are different facts for a payload, and the columns have to
// keep them apart: nil means the runtime captured nothing.
func TestTracesInsertKeepsAbsentPayloadsNull(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	if err := store.Insert(ctx, []TraceRow{storedRecord(traceID, 1, ingest.KindFlowStarted)}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var body, vars *string
	var attrs string
	if err := store.pool.QueryRow(ctx,
		`SELECT body, vars, attrs FROM traces WHERE trace_id = $1`, traceID).Scan(&body, &vars, &attrs); err != nil {
		t.Fatalf("read: %v", err)
	}
	if body != nil || vars != nil {
		t.Errorf("uncaptured payloads stored as %v/%v, want NULL", body, vars)
	}
	if attrs != "{}" {
		t.Errorf("attrs = %q, want an empty object", attrs)
	}
}

// A cost that is not known is stored as NULL, never as zero, with cost_status
// beside it saying which kind of unknown it was.
func TestTracesInsertStoresUnknownCostsAsNull(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()
	var table *cost.Table // prices nothing

	usage := &cost.Usage{InputTokens: 1000, OutputTokens: 200}
	unpriced := storedRecord(traceID, 1, ingest.KindLLMTurn)
	unpriced.Record.Model = "a-model-nobody-published"
	unpriced.Record.Usage = usage
	unpriced.Priced = table.Price(cost.Call{Model: unpriced.Record.Model, Usage: usage})

	if err := store.Insert(ctx, []TraceRow{unpriced}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var costUSD *float64
	var status, model string
	var inputTokens *int
	if err := store.pool.QueryRow(ctx,
		`SELECT cost_usd, cost_status, model, input_tokens FROM traces WHERE trace_id = $1`,
		traceID).Scan(&costUSD, &status, &model, &inputTokens); err != nil {
		t.Fatalf("read: %v", err)
	}

	if costUSD != nil {
		t.Errorf("an unpriced call stored a cost of %v, want NULL", *costUSD)
	}
	if status != string(cost.StatusUnpricedModel) {
		t.Errorf("cost status = %q, want %q", status, cost.StatusUnpricedModel)
	}
	// The tokens are still stored: the provider reported them, and only the
	// money is unknown.
	if inputTokens == nil || *inputTokens != 1000 {
		t.Errorf("input tokens = %v, want 1000", inputTokens)
	}
}

// The rollup is derived entirely from the records, so it has to be written in the
// same transaction: records stored without it would leave a trace whose totals
// silently understate it, and nothing afterwards would know to recompute.
func TestTracesInsertRollsUpASummary(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	err := store.Insert(ctx, []TraceRow{
		storedRecord(traceID, 1, ingest.KindSourceReceive,
			withAttrs(`{"method":"POST","route":"/checkout"}`)),
		storedRecord(traceID, 2, ingest.KindFlowStarted, inFlow("checkout")),
		storedRecord(traceID, 3, ingest.KindBlockPostInvoke, inFlow("checkout"), endingAt(30, 25)),
		storedRecord(traceID, 4, ingest.KindFlowCompleted, inFlow("checkout"), endingAt(95, 95)),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got := readSummary(t, store, traceID)
	switch {
	case got.Records != 4:
		t.Errorf("records = %d, want 4", got.Records)
	case got.EntryKind != ingest.KindSourceReceive || got.EntryLabel != "POST /checkout":
		t.Errorf("entry = %s %q", got.EntryKind, got.EntryLabel)
	case got.RootFlow != "checkout":
		t.Errorf("root flow = %q, want checkout", got.RootFlow)
	case got.Status != StatusOK:
		t.Errorf("status = %q, want ok", got.Status)
	case got.RootDurationNs != ms(95):
		t.Errorf("root duration = %d, want %d", got.RootDurationNs, ms(95))
	case !got.StartedAt.Equal(at(0)) || !got.EndedAt.Equal(at(95)):
		t.Errorf("interval = %s..%s", got.StartedAt, got.EndedAt)
	}
}

// The whole point of the rollup's merge rules: records arrive across batches, and
// the summary must come out the same whichever batch landed first.
func TestTracesSummaryMergesAcrossBatchesEitherWay(t *testing.T) {
	// The second batch carries the entry record and the failure; the first
	// carries only interior work. Whichever is stored first, the summary is the
	// same.
	interior := func(traceID string) []TraceRow {
		return []TraceRow{
			storedRecord(traceID, 5, ingest.KindFlowStarted, inFlow("checkout")),
			storedRecord(traceID, 6, ingest.KindBlockPostInvoke, inFlow("checkout"), endingAt(30, 25)),
			storedRecord(traceID, 7, ingest.KindLLMTurn, func(r *TraceRow) {
				r.Record.Model = "m"
				r.Record.Usage = &cost.Usage{InputTokens: 1000, OutputTokens: 200}
				r.Priced = priced(0.01, "OPENAI", rateID)
			}),
		}
	}
	entry := func(traceID string) []TraceRow {
		return []TraceRow{
			storedRecord(traceID, 1, ingest.KindSourceReceive,
				withAttrs(`{"method":"POST","route":"/checkout"}`)),
			storedRecord(traceID, 9, ingest.KindFlowFailed, inFlow("checkout"), endingAt(95, 95)),
		}
	}

	cases := []struct {
		name  string
		order func(traceID string) [][]TraceRow
	}{
		{
			name:  "the entry record arrives last",
			order: func(id string) [][]TraceRow { return [][]TraceRow{interior(id), entry(id)} },
		},
		{
			name:  "the entry record arrives first",
			order: func(id string) [][]TraceRow { return [][]TraceRow{entry(id), interior(id)} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, traceID := newTestTraces(t)
			ctx := context.Background()
			for i, batch := range tc.order(traceID) {
				if err := store.Insert(ctx, batch); err != nil {
					t.Fatalf("batch %d: %v", i, err)
				}
			}

			got := readSummary(t, store, traceID)
			switch {
			case got.Records != 5:
				t.Errorf("records = %d, want 5", got.Records)
			case got.EntryKind != ingest.KindSourceReceive:
				t.Errorf("entry kind = %q, want the source record's", got.EntryKind)
			case got.EntryLabel != "POST /checkout":
				t.Errorf("entry label = %q, want POST /checkout", got.EntryLabel)
			case got.EntryRank != entryRankSourceReceive:
				t.Errorf("entry rank = %d, want %d", got.EntryRank, entryRankSourceReceive)
			case got.RootFlow != "checkout":
				t.Errorf("root flow = %q, want checkout", got.RootFlow)
			case got.Status != StatusFailed:
				t.Errorf("status = %q, want failed", got.Status)
			case got.RootDurationNs != ms(95):
				t.Errorf("root duration = %d, want %d", got.RootDurationNs, ms(95))
			case !got.StartedAt.Equal(at(0)) || !got.EndedAt.Equal(at(95)):
				t.Errorf("interval = %s..%s, want 0..95ms", got.StartedAt, got.EndedAt)
			case got.LLMCalls != 1 || got.InputTokens != 1000 || got.OutputTokens != 200:
				t.Errorf("model accounting = %d calls %d/%d tokens", got.LLMCalls, got.InputTokens, got.OutputTokens)
			case got.CostUSD < 0.0099 || got.CostUSD > 0.0101:
				t.Errorf("cost = %v, want 0.01", got.CostUSD)
			}
		})
	}
}

// A worse entry record arriving later must not overwrite a better one already
// stored, which is the direction a last-writer-wins rule gets wrong.
func TestTracesSummaryKeepsTheBetterEntryRecord(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	if err := store.Insert(ctx, []TraceRow{
		storedRecord(traceID, 1, ingest.KindSourceReceive, withAttrs(`{"method":"GET","route":"/health"}`)),
	}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if err := store.Insert(ctx, []TraceRow{
		storedRecord(traceID, 2, ingest.KindFlowStarted, inFlow("health")),
	}); err != nil {
		t.Fatalf("second batch: %v", err)
	}

	got := readSummary(t, store, traceID)
	if got.EntryKind != ingest.KindSourceReceive || got.EntryLabel != "GET /health" {
		t.Fatalf("entry = %s %q, want the source record kept", got.EntryKind, got.EntryLabel)
	}
	// The flow still lands, because it is chosen on its own terms.
	if got.RootFlow != "health" {
		t.Fatalf("root flow = %q, want health", got.RootFlow)
	}
}

// A trace crossing deployments keeps the entry's identity and accumulates every
// deployment it touched.
func TestTracesSummaryAcrossDeployments(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	back := func(r *TraceRow) {
		r.Record.DeploymentID = depBack
		r.Record.AppName = "fulfilment"
		r.Record.AppVersion = "v2"
		r.IntegrationID = intBack
	}
	err := store.Insert(ctx, []TraceRow{
		storedRecord(traceID, 1, ingest.KindSourceReceive, withAttrs(`{"method":"POST","route":"/checkout"}`)),
		storedRecord(traceID, 1, ingest.KindBlockPostInvoke, inFlow("fulfil"), back),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	got := readSummary(t, store, traceID)
	if got.DeploymentID != depFront {
		t.Errorf("deployment = %s, want the entry's %s", got.DeploymentID, depFront)
	}
	if got.AppName != "storefront" || got.AppVersion != "v9" {
		t.Errorf("app = %s/%s, want storefront/v9", got.AppName, got.AppVersion)
	}
	if got.IntegrationID == nil || *got.IntegrationID != intFront {
		t.Errorf("integration = %v, want %s", got.IntegrationID, intFront)
	}
	if len(got.DeploymentIDs) != 2 {
		t.Errorf("deployments = %v, want both", got.DeploymentIDs)
	}
}

// Sets union rather than replace, and stay deduplicated across batches.
func TestTracesSummaryUnionsModelsAcrossBatches(t *testing.T) {
	store, traceID := newTestTraces(t)
	ctx := context.Background()

	call := func(seq int64, model string) TraceRow {
		return storedRecord(traceID, seq, ingest.KindLLMTurn, func(r *TraceRow) {
			r.Record.Model = model
			r.Record.Usage = &cost.Usage{InputTokens: 10}
			r.Priced = priced(0.001, "OPENAI", rateID)
		})
	}
	if err := store.Insert(ctx, []TraceRow{call(1, "model-a"), call(2, "model-b")}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	if err := store.Insert(ctx, []TraceRow{call(3, "model-b"), call(4, "model-c")}); err != nil {
		t.Fatalf("second batch: %v", err)
	}

	got := readSummary(t, store, traceID)
	want := []string{"model-a", "model-b", "model-c"}
	if len(got.Models) != len(want) {
		t.Fatalf("models = %v, want %v", got.Models, want)
	}
	for i, model := range want {
		if got.Models[i] != model {
			t.Fatalf("models = %v, want %v", got.Models, want)
		}
	}
	if got.LLMCalls != 4 {
		t.Errorf("llm calls = %d, want 4", got.LLMCalls)
	}
}

// The dropped-records marker is stored — it is how a reader learns the stream is
// incomplete — but it belongs to no trace and must not invent a summary row.
func TestTracesInsertStoresTheDroppedMarkerWithoutASummary(t *testing.T) {
	store, _ := newTestTraces(t)
	ctx := context.Background()

	marker := storedRecord("", 99, ingest.KindTraceDropped, withAttrs(`{"dropped":5,"total":5}`))
	if err := store.Insert(ctx, []TraceRow{marker}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Both counts are scoped to this test's deployment, so an unrelated row left
	// by something else cannot make this pass or fail.
	var records, summaries int
	if err := store.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM traces WHERE kind = $1 AND deployment_id = $2::uuid),
		        (SELECT count(*) FROM trace_summaries WHERE deployment_id = $2::uuid)`,
		ingest.KindTraceDropped, depFront).Scan(&records, &summaries); err != nil {
		t.Fatalf("count: %v", err)
	}
	if records != 1 {
		t.Errorf("stored %d dropped markers, want 1", records)
	}
	if summaries != 0 {
		t.Errorf("the marker created %d summary rows, want none", summaries)
	}
}

func TestTracesInsertOfNothingDoesNothing(t *testing.T) {
	store, _ := newTestTraces(t)
	if err := store.Insert(context.Background(), nil); err != nil {
		t.Fatalf("insert: %v", err)
	}
}
