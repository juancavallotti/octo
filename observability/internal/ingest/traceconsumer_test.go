package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/juancavallotti/octo/observability/internal/cost"
)

const (
	testDeployment  = "11111111-1111-1111-1111-111111111111"
	testIntegration = "22222222-2222-2222-2222-222222222222"
	testRateID      = "33333333-3333-3333-3333-333333333333"
)

// traceStore captures whole batches, because their sizes and boundaries are what
// the batching rules are about — a store that only counted rows could not tell a
// flush that happened from one that did not.
type traceStore struct {
	mu      sync.Mutex
	batches [][]TraceRow
	err     error

	wrote chan int
}

func newTraceStore() *traceStore {
	return &traceStore{wrote: make(chan int, 64)}
}

// Insert refuses a cancelled context the way a real pool does, which is what
// makes every test here an assertion about *which* context the consumer flushed
// under. A store that ignored it would record batches the database would have
// rejected, and the shutdown path would look like it worked.
func (s *traceStore) Insert(ctx context.Context, rows []TraceRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	// Copied: the consumer reuses its slice between flushes, so keeping the
	// caller's would leave every recorded batch aliasing the same array.
	batch := make([]TraceRow, len(rows))
	copy(batch, rows)
	s.batches = append(s.batches, batch)
	err := s.err
	s.mu.Unlock()

	s.wrote <- len(batch)
	return err
}

// awaitBatches waits for n batches to have been written.
func (s *traceStore) awaitBatches(t *testing.T, n int) [][]TraceRow {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-s.wrote:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for batch %d of %d", i+1, n)
		}
	}
	return s.taken()
}

func (s *traceStore) taken() [][]TraceRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]TraceRow, len(s.batches))
	copy(out, s.batches)
	return out
}

func (s *traceStore) sizes() []int {
	sizes := []int{}
	for _, batch := range s.taken() {
		sizes = append(sizes, len(batch))
	}
	return sizes
}

// fixedIntegrations answers every lookup the same way and counts the asking.
type fixedIntegrations struct {
	id    string
	found bool
	err   error
	calls atomic.Int64
}

func (f *fixedIntegrations) Resolve(context.Context, string) (string, bool, error) {
	f.calls.Add(1)
	return f.id, f.found, f.err
}

func resolvesTo(integrationID string) *fixedIntegrations {
	return &fixedIntegrations{id: integrationID, found: integrationID != ""}
}

// testCard is a real rate card rather than a stubbed pricer, so the test asserts
// what a record will actually be stored with.
func testCard() *cost.Table {
	return cost.NewTable([]cost.Rate{{
		ID:          testRateID,
		Provider:    "OPENAI",
		Pattern:     "gpt-4o",
		Operator:    cost.OpEquals,
		InputPer1M:  2.5,
		OutputPer1M: 10,
	}})
}

// testConsumer builds a consumer with no folder, which is what every case here
// wants: these tests are about batching, shedding and the shutdown drain, and a
// folder would hold the very records they assert on. The folding path has its own
// tests below and in internal/fold.
func testConsumer(store TraceStore, resolver integrations) *TraceConsumer {
	return NewTraceConsumer(store, resolver, testCard(), nil)
}

// traceMsg renders one record on the wire.
func traceMsg(seq int, kind string) *nats.Msg {
	return &nats.Msg{Subject: TraceSubject, Data: []byte(fmt.Sprintf(
		`{"seq":%d,"kind":%q,"traceId":"tr-1","at":"2026-06-28T10:00:00Z","durationNs":1000,`+
			`"flow":"orders","deploymentId":%q,"appName":"svc","appVersion":"v3"}`,
		seq, kind, testDeployment))}
}

func modelCallMsg(model string, inputTokens, outputTokens int) *nats.Msg {
	return &nats.Msg{Subject: TraceSubject, Data: []byte(fmt.Sprintf(
		`{"seq":7,"kind":%q,"traceId":"tr-1","at":"2026-06-28T10:00:00Z","deploymentId":%q,`+
			`"attrs":{"model":%q,"usage":{"inputTokens":%d,"outputTokens":%d}}}`,
		KindLLMTurn, testDeployment, model, inputTokens, outputTokens))}
}

// feed runs the writer over a pre-filled channel and returns once it is done, so
// the batching rules can be exercised without a broker in the way.
func feed(t *testing.T, c *TraceConsumer, msgs []*nats.Msg) {
	t.Helper()
	in := make(chan *nats.Msg, len(msgs))
	for _, m := range msgs {
		in <- m
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.write(ctx, in)
}

// TestTraceConsumerStoresAShippedRecord walks one record all the way from the
// subject to the store, which is the only test here that involves a broker.
func TestTraceConsumerStoresAShippedRecord(t *testing.T) {
	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	store := newTraceStore()
	sub, err := testConsumer(store, resolvesTo(testIntegration)).Start(context.Background(), conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := conn.Flush(); err != nil { // ensure the SUB is registered before publishing
		t.Fatalf("flush: %v", err)
	}

	if err := pub.Publish(TraceSubject, traceMsg(1, KindFlowStarted).Data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	batches := store.awaitBatches(t, 1)
	if len(batches[0]) != 1 {
		t.Fatalf("first batch held %d records, want 1", len(batches[0]))
	}
	row := batches[0][0]
	if row.Record.TraceID != "tr-1" || row.Record.Kind != KindFlowStarted {
		t.Errorf("stored record = %+v, want the flow.started of tr-1", row.Record)
	}
	if row.Record.AppVersion != "v3" {
		t.Errorf("app version = %q, want the version stamped by the emitting pod", row.Record.AppVersion)
	}
	if row.IntegrationID != testIntegration {
		t.Errorf("integration = %q, want %q", row.IntegrationID, testIntegration)
	}
}

// TestTraceConsumerKeepsGoingAfterABadRecord checks an undecodable record is
// dropped where it is found rather than taking the batch behind it down with it.
func TestTraceConsumerKeepsGoingAfterABadRecord(t *testing.T) {
	store := newTraceStore()
	feed(t, testConsumer(store, resolvesTo(testIntegration)), []*nats.Msg{
		{Data: []byte(`garbage`)},
		{Data: []byte(`{"kind":"flow.started"}`)}, // decodable JSON, but no deployment to attribute it to
		traceMsg(2, KindFlowCompleted),
	})

	batches := store.taken()
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("batches = %v, want the one good record", store.sizes())
	}
	if got := batches[0][0].Record.Kind; got != KindFlowCompleted {
		t.Errorf("stored %q, want the record that followed the bad ones", got)
	}
}

// TestTraceConsumerFlushesAFullBatch is the size rule. The extra records past the
// limit are what make it a real assertion: a consumer that only ever flushed on
// its deadline would have written all of them at once.
func TestTraceConsumerFlushesAFullBatch(t *testing.T) {
	const extra = 3

	msgs := make([]*nats.Msg, 0, traceBatchSize+extra)
	for i := 0; i < traceBatchSize+extra; i++ {
		msgs = append(msgs, traceMsg(i, KindBlockPostInvoke))
	}

	store := newTraceStore()
	feed(t, testConsumer(store, resolvesTo(testIntegration)), msgs)

	want := []int{traceBatchSize, extra}
	if got := store.sizes(); !equalInts(got, want) {
		t.Errorf("batch sizes = %v, want %v", got, want)
	}
}

// TestTraceConsumerFlushesPartialBatchesOnTheirDeadline is the other half of the
// rule: a deployment quiet enough never to fill a batch must not have its records
// held until one day it does.
//
// Two rounds, because one only proves the deadline works once. The steady state
// for a quiet deployment is every batch going out this way, which needs the clock
// to be re-armed by each new batch's first record rather than started once.
func TestTraceConsumerFlushesPartialBatchesOnTheirDeadline(t *testing.T) {
	store := newTraceStore()
	consumer := testConsumer(store, resolvesTo(testIntegration))

	in := make(chan *nats.Msg, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Queued before the writer starts, so both records are in hand when it looks
	// and the batch boundary is the deadline rather than a scheduling accident.
	in <- traceMsg(1, KindFlowStarted)
	in <- traceMsg(2, KindFlowCompleted)

	done := make(chan struct{})
	go func() {
		defer close(done)
		consumer.write(ctx, in)
	}()

	// Far short of a full batch, so only the deadline can produce these writes.
	if got := len(store.awaitBatches(t, 1)[0]); got != 2 {
		t.Fatalf("first batch held %d records, want the 2 that were waiting", got)
	}

	in <- traceMsg(3, KindFlowStarted)
	in <- traceMsg(4, KindFlowCompleted)

	batches := store.awaitBatches(t, 1)
	if len(batches) != 2 {
		t.Fatalf("wrote %v, want a second batch", store.sizes())
	}
	if got := len(batches[1]); got != 2 {
		t.Errorf("second batch held %d records, want 2", got)
	}

	cancel()
	<-done
}

// TestTraceConsumerWritesWhatItHoldsOnShutdown covers the records that are the
// easiest to lose: taken off the wire, not yet written, and never redelivered
// because the runtime ships them fire-and-forget.
func TestTraceConsumerWritesWhatItHoldsOnShutdown(t *testing.T) {
	store := newTraceStore()

	// feed hands the writer an already-cancelled context, so it takes the
	// shutdown path with records still queued.
	feed(t, testConsumer(store, resolvesTo(testIntegration)), []*nats.Msg{
		traceMsg(1, KindFlowStarted),
		traceMsg(2, KindFlowCompleted),
	})

	batches := store.taken()
	if len(batches) != 1 || len(batches[0]) != 2 {
		t.Fatalf("batch sizes = %v, want one batch of 2 written on the way out", store.sizes())
	}
}

// TestTraceConsumerWritesTheRecordItTookAsTheContextDied covers the branch a
// cancelled writer can still be scheduled into: select does not prefer ctx.Done()
// over a ready record, so the loop can take one more message after cancellation
// and must not then write it — or the batch it joined — under the dead context.
//
// The race itself cannot be forced from outside; what is asserted here is the
// property that makes it harmless, on the path the loop hands that record to.
func TestTraceConsumerWritesTheRecordItTookAsTheContextDied(t *testing.T) {
	store := newTraceStore()
	consumer := testConsumer(store, resolvesTo(testIntegration))

	in := make(chan *nats.Msg, 2)
	in <- traceMsg(2, KindBlockPostInvoke)
	in <- traceMsg(3, KindFlowCompleted)

	batch := newTraceBatch(store)
	defer batch.stop()
	consumer.drain(traceMsg(1, KindFlowStarted), in, batch)

	batches := store.taken()
	if len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("batch sizes = %v, want one batch of 3 — the record in hand and the two behind it", store.sizes())
	}
	if got := batches[0][0].Record.Seq; got != 1 {
		t.Errorf("first record has seq %d, want the one already taken off the channel", got)
	}
}

// TestTraceConsumerShedsRecordsItHasNoRoomFor pins the behaviour under a writer
// that has fallen behind: drop, count, and stay non-blocking.
func TestTraceConsumerShedsRecordsItHasNoRoomFor(t *testing.T) {
	consumer := testConsumer(newTraceStore(), resolvesTo(testIntegration))

	work := make(chan *nats.Msg, 1)
	consumer.offer(work, traceMsg(1, KindFlowStarted))
	if got := consumer.Dropped(); got != 0 {
		t.Fatalf("dropped %d records with room to spare, want 0", got)
	}

	// The buffer is full now; everything after it is shed rather than queued.
	for i := 0; i < 3; i++ {
		consumer.offer(work, traceMsg(i, KindFlowStarted))
	}
	if got := consumer.Dropped(); got != 3 {
		t.Errorf("dropped = %d, want 3", got)
	}
	if len(work) != 1 {
		t.Errorf("queued %d records, want the buffer to still hold exactly its 1", len(work))
	}
}

// TestTraceConsumerPricesAModelCall checks the cost is frozen onto the record on
// the way in, with the rate that produced it recorded beside the number.
func TestTraceConsumerPricesAModelCall(t *testing.T) {
	store := newTraceStore()
	feed(t, testConsumer(store, resolvesTo(testIntegration)), []*nats.Msg{
		modelCallMsg("gpt-4o", 1_000_000, 500_000),
	})

	batches := store.taken()
	if len(batches) != 1 || len(batches[0]) != 1 {
		t.Fatalf("batch sizes = %v, want one record", store.sizes())
	}
	priced := batches[0][0].Priced

	if priced.Status != cost.StatusPriced {
		t.Fatalf("status = %q, want %q", priced.Status, cost.StatusPriced)
	}
	if priced.PriceID != testRateID {
		t.Errorf("price id = %q, want the rate that priced it (%q)", priced.PriceID, testRateID)
	}
	amount, known := priced.CostUSD()
	if want := 2.5 + 5.0; !known || amount != want {
		t.Errorf("cost = %v (known %v), want %v", amount, known, want)
	}
}

// TestTraceConsumerLeavesAnUnknownModelUnpriced is the rule the whole cost
// package exists for, checked where it is applied: a model with no rate must
// arrive at the store carrying no cost, not a cost of nothing.
func TestTraceConsumerLeavesAnUnknownModelUnpriced(t *testing.T) {
	store := newTraceStore()
	feed(t, testConsumer(store, resolvesTo(testIntegration)), []*nats.Msg{
		modelCallMsg("some-model-nobody-published", 1_000, 1_000),
	})

	priced := store.taken()[0][0].Priced
	if priced.Status != cost.StatusUnpricedModel {
		t.Errorf("status = %q, want %q", priced.Status, cost.StatusUnpricedModel)
	}
	if _, known := priced.CostUSD(); known {
		t.Error("an unpriced call reported a known cost")
	}
}

// TestTraceConsumerLeavesANonModelCallWithNoCostStatus keeps the two kinds of
// "no cost" apart. A block invocation is not a model call whose provider went
// quiet: stored as no_usage it would report every flow in the system as an
// unpriced model call, and the count of the ones that really are would be lost
// inside it.
func TestTraceConsumerLeavesANonModelCallWithNoCostStatus(t *testing.T) {
	store := newTraceStore()
	feed(t, testConsumer(store, resolvesTo(testIntegration)), []*nats.Msg{
		traceMsg(1, KindBlockPostInvoke),
	})

	if got := store.taken()[0][0].Priced.Status; got != "" {
		t.Errorf("cost status = %q, want it empty: the record is not a model call", got)
	}
}

// TestTraceConsumerStoresARecordItCannotAttribute covers both ways the lookup
// comes back empty. Neither is a reason to throw the record away: the trace still
// happened, and a record dropped here is the evidence of whatever went wrong.
func TestTraceConsumerStoresARecordItCannotAttribute(t *testing.T) {
	cases := []struct {
		name     string
		resolver *fixedIntegrations
	}{
		{"no such deployment", resolvesTo("")},
		{"lookup failed", &fixedIntegrations{err: errors.New("database is down")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newTraceStore()
			feed(t, testConsumer(store, tc.resolver), []*nats.Msg{traceMsg(1, KindFlowStarted)})

			batches := store.taken()
			if len(batches) != 1 || len(batches[0]) != 1 {
				t.Fatalf("batch sizes = %v, want the record stored anyway", store.sizes())
			}
			if got := batches[0][0].IntegrationID; got != "" {
				t.Errorf("integration = %q, want it left empty", got)
			}
		})
	}
}

// TestTraceConsumerKeepsBatchingAfterAFailedWrite checks a store error costs the
// batch that hit it and nothing more. Losing a batch is the documented cost of
// at-most-once shipping; losing the writer would be an outage.
func TestTraceConsumerKeepsBatchingAfterAFailedWrite(t *testing.T) {
	store := newTraceStore()
	store.err = errors.New("insert failed")

	msgs := make([]*nats.Msg, 0, traceBatchSize+1)
	for i := 0; i < traceBatchSize+1; i++ {
		msgs = append(msgs, traceMsg(i, KindBlockPostInvoke))
	}
	feed(t, testConsumer(store, resolvesTo(testIntegration)), msgs)

	want := []int{traceBatchSize, 1}
	if got := store.sizes(); !equalInts(got, want) {
		t.Errorf("batch sizes = %v, want %v — the batch after a failure still went", got, want)
	}
}

func equalInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
