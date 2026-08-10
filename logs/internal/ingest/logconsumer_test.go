package ingest

import (
	"context"
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

func (s *captureStore) Insert(_ context.Context, e LogEvent) error {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
	s.got <- struct{}{}
	return nil
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
