package ingest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
)

// runServer starts an embedded NATS server on a random port for a hermetic test.
func runServer(t *testing.T) string {
	t.Helper()
	srv := natsserver.RunServer(&server.Options{Port: -1})
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// captureStore records every inserted event for assertions.
type captureStore struct {
	mu     sync.Mutex
	events []LogEvent
	got    chan struct{}
}

func newCaptureStore(n int) *captureStore {
	return &captureStore{got: make(chan struct{}, n)}
}

// Insert refuses a cancelled context the way a real pool does, which is what
// makes every test here an assertion about *which* context the consumer wrote
// under. Ignoring it — as this fake did — records inserts the database would
// have rejected, and the shutdown path looks like it worked.
func (s *captureStore) Insert(ctx context.Context, e LogEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	s.got <- struct{}{}
	return nil
}

// logMsg renders one record on the wire.
func logMsg(msg string) *nats.Msg {
	return &nats.Msg{Subject: LogSubject, Data: []byte(fmt.Sprintf(
		`{"time":"2026-06-28T10:00:00Z","level":"INFO","msg":%q,"deploymentId":"dep-9"}`, msg))}
}

func (s *captureStore) messages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	for i, e := range s.events {
		out[i] = e.Message
	}
	return out
}

// TestConsumerWritesWhatItHoldsOnShutdown covers the records left in the buffer
// when a termination signal arrives. Workers used to return on cancellation and
// abandon them — and core NATS does not redeliver, so they were gone. On a
// rolling restart, which is every deploy, that is a log line lost per worker.
//
// The store refuses a cancelled context, so this fails if the drain writes under
// the subscription's context instead of one of its own.
func TestConsumerWritesWhatItHoldsOnShutdown(t *testing.T) {
	store := newCaptureStore(4)
	work := make(chan *nats.Msg, 4)
	work <- logMsg("first")
	work <- logMsg("second")

	NewLogConsumer(store, 1).drain(context.Background(), work)

	if got := store.messages(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Errorf("stored %v, want both buffered records written on the way out", got)
	}
}

// wedgedStore never answers, standing in for a database that has stopped
// responding rather than refused.
type wedgedStore struct{ entered chan struct{} }

func (s *wedgedStore) Insert(ctx context.Context, _ LogEvent) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

// TestConsumerCloseGivesUpOnAWedgedStore is the other side of finishing in-flight
// writes: they get the shutdown budget, not their own full timeout. A write is
// allowed to outlive the subscription so it can finish — but not to hold Close
// open for logWriteTimeout, which would spend a pod's whole termination grace
// period waiting on a database that is not going to answer.
func TestConsumerCloseGivesUpOnAWedgedStore(t *testing.T) {
	// The assertion below allows twice the budget for scheduling slack, so that
	// is the relation the constants have to satisfy — otherwise the bound being
	// ruled out is inside the tolerance and the test proves nothing.
	if shutdownFlushTimeout*2 >= logWriteTimeout {
		t.Fatalf("shutdownFlushTimeout (%v) must leave room for the 2x bound below before logWriteTimeout (%v)",
			shutdownFlushTimeout, logWriteTimeout)
	}

	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	store := &wedgedStore{entered: make(chan struct{}, 1)}
	sub, err := NewLogConsumer(store, 1).Start(context.Background(), conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := pub.Publish(LogSubject, logMsg("stuck").Data); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-store.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the record never reached the store")
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- sub.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
		// Bounded by the shutdown budget, with slack for scheduling — and well
		// under logWriteTimeout, which is the bound being ruled out.
		if elapsed := time.Since(start); elapsed > shutdownFlushTimeout*2 {
			t.Errorf("Close took %v, want it bounded by the %v shutdown budget", elapsed, shutdownFlushTimeout)
		}
	case <-time.After(logWriteTimeout):
		t.Fatalf("Close did not return within %v: a stuck write is holding the shutdown open", logWriteTimeout)
	}
}

// TestConsumerShedsRecordsItHasNoRoomFor pins the behaviour under a writer that
// has fallen behind: drop, count, and stay non-blocking. The callback used to
// block instead, which pushed the loss into the NATS client's own slow-consumer
// handling — the same records gone, reported as a client-side error naming
// neither a count nor an app.
func TestConsumerShedsRecordsItHasNoRoomFor(t *testing.T) {
	consumer := NewLogConsumer(newCaptureStore(1), 1)

	work := make(chan *nats.Msg, 1)
	consumer.offer(work, logMsg("kept"))
	consumer.offer(work, logMsg("shed"))
	consumer.offer(work, logMsg("shed too"))

	if got := consumer.Dropped(); got != 2 {
		t.Errorf("Dropped() = %d, want 2", got)
	}
	if len(work) != 1 {
		t.Errorf("buffered %d records, want the one that fit", len(work))
	}
}

// holdStore blocks every insert until released, so a test can be certain the
// writes are in flight when the shutdown arrives rather than hoping they are.
type holdStore struct {
	*captureStore
	started chan struct{}
	release chan struct{}
}

func (s *holdStore) Insert(ctx context.Context, e LogEvent) error {
	s.started <- struct{}{}
	<-s.release
	return s.captureStore.Insert(ctx, e)
}

// TestConsumerFinishesInsertsInFlightAtShutdown is the other half of #260, and
// the one that cost a record on every deploy: not the buffer, but the writes
// already running when the signal arrived. They were issued under the
// subscription's context, so cancelling it aborted them — Close's promise to
// "wait for in-flight inserts to finish" meant waiting for them to fail fast.
//
// The ordering here is exact, not hopeful: every record is parked inside its
// insert before the context is cancelled, and only released afterwards. Against
// the old code every one of those inserts returns context.Canceled and nothing
// is stored.
func TestConsumerFinishesInsertsInFlightAtShutdown(t *testing.T) {
	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	const records = 3
	base := newCaptureStore(records)
	store := &holdStore{
		captureStore: base,
		started:      make(chan struct{}, records),
		release:      make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := NewLogConsumer(store, records).Start(ctx, conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := conn.Flush(); err != nil { // ensure the SUB is registered before publishing
		t.Fatalf("flush: %v", err)
	}

	for _, m := range []string{"a", "b", "c"} {
		if err := pub.Publish(LogSubject, logMsg(m).Data); err != nil {
			t.Fatalf("publish %s: %v", m, err)
		}
	}

	for i := 0; i < records; i++ {
		select {
		case <-store.started:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of %d records reached the store", i, records)
		}
	}

	// Cancelling the parent marks the subscription context done before anything
	// is released, so every insert below runs with it already dead.
	cancel()
	close(store.release)

	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close returns only once every worker has finished, so the count is final.
	if got := base.messages(); len(got) != records {
		t.Errorf("stored %v, want all %d records written across the shutdown", got, records)
	}
}

func TestConsumerPersistsShippedRecord(t *testing.T) {
	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	store := newCaptureStore(1)
	sub, err := NewLogConsumer(store, 4).Start(context.Background(), conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := conn.Flush(); err != nil { // ensure the SUB is registered before publishing
		t.Fatalf("flush: %v", err)
	}

	rec := `{"time":"2026-06-28T10:00:00Z","level":"INFO","msg":"hi","deploymentId":"dep-9","appName":"svc","appVersion":"v1","k":"v"}`
	if err := pub.Publish(LogSubject, []byte(rec)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case <-store.got:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the record to be stored")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 {
		t.Fatalf("stored %d events, want 1", len(store.events))
	}
	if got := store.events[0]; got.DeploymentID != "dep-9" || got.Message != "hi" || got.AppName != "svc" {
		t.Errorf("stored event = %+v, want dep-9/hi/svc", got)
	}
}

// TestConsumerDropsUndecodableRecord verifies a bad record is dropped without
// reaching the store and without stalling the subscription.
func TestConsumerDropsUndecodableRecord(t *testing.T) {
	url := runServer(t)
	pub := connect(t, url)
	conn := connect(t, url)

	store := newCaptureStore(1)
	sub, err := NewLogConsumer(store, 2).Start(context.Background(), conn)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := conn.Flush(); err != nil { // ensure the SUB is registered before publishing
		t.Fatalf("flush: %v", err)
	}

	if err := pub.Publish(LogSubject, []byte(`garbage`)); err != nil {
		t.Fatalf("publish bad: %v", err)
	}
	// A valid record after the bad one must still be stored, proving the pipeline
	// kept flowing.
	if err := pub.Publish(LogSubject, []byte(`{"deploymentId":"d","msg":"ok"}`)); err != nil {
		t.Fatalf("publish good: %v", err)
	}

	select {
	case <-store.got:
	case <-time.After(2 * time.Second):
		t.Fatal("valid record after a bad one was not stored")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.events) != 1 || store.events[0].Message != "ok" {
		t.Errorf("events = %+v, want exactly the valid record", store.events)
	}
}
