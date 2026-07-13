package k8s

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
	"github.com/nats-io/nats-server/v2/server"
	natsserver "github.com/nats-io/nats-server/v2/test"
	"github.com/nats-io/nats.go"
)

// runServer starts an embedded NATS server on a random port for a hermetic test,
// returning its client URL. It is shut down when the test ends.
func runServer(t *testing.T) string {
	t.Helper()
	srv := natsserver.RunServer(&server.Options{Port: -1})
	t.Cleanup(srv.Shutdown)
	return srv.ClientURL()
}

// connect dials the embedded server and closes the connection at test end.
func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(conn.Close)
	return conn
}

func bodyMessage(s string) types.Message {
	return types.Message{Body: s}
}

// TestEncodeDecodeRoundTrip verifies body, ids and typed vars survive the
// message ⇄ NATS-headers conversion without a server.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := types.Message{
		EventID:       "evt-1",
		CorrelationID: "corr-1",
		Body:          map[string]any{"hello": "world"},
	}
	in.Variables.Set("tenant", "acme")
	in.Variables.Set("count", 42)
	in.Variables.Set("enabled", true)

	m, err := encodeMsg("subject", in)
	if err != nil {
		t.Fatalf("encodeMsg: %v", err)
	}
	if got := m.Header.Get(headerVarPrefix + "tenant"); got != `"acme"` {
		t.Fatalf("tenant header = %q, want %q", got, `"acme"`)
	}

	out, err := decodeMsg(m)
	if err != nil {
		t.Fatalf("decodeMsg: %v", err)
	}
	if out.EventID != "evt-1" || out.CorrelationID != "corr-1" {
		t.Fatalf("ids = %q/%q, want evt-1/corr-1", out.EventID, out.CorrelationID)
	}
	if tenant, _ := out.Variables.String("tenant"); tenant != "acme" {
		t.Fatalf("tenant = %q, want acme", tenant)
	}
	if count, _ := out.Variables.Int("count"); count != 42 {
		t.Fatalf("count = %d, want 42", count)
	}
	if enabled, _ := out.Variables.Bool("enabled"); !enabled {
		t.Fatalf("enabled = false, want true")
	}
	if out.RawContent {
		t.Fatalf("RawContent = true, want false for a JSON message")
	}
}

func TestEncodeDecodePreservesRawContent(t *testing.T) {
	var in types.Message
	in.SetRawBody("text/html", "<h1>hi</h1>")

	m, err := encodeMsg("subject", in)
	if err != nil {
		t.Fatalf("encodeMsg: %v", err)
	}
	if got := m.Header.Get(headerRawContent); got != "true" {
		t.Fatalf("raw-content header = %q, want %q", got, "true")
	}

	out, err := decodeMsg(m)
	if err != nil {
		t.Fatalf("decodeMsg: %v", err)
	}
	ct, data, ok := out.RawBody()
	if !ok {
		t.Fatalf("RawBody ok = false; RawContent=%v Body=%#v", out.RawContent, out.Body)
	}
	if ct != "text/html" || data != "<h1>hi</h1>" {
		t.Errorf("RawBody = (%q, %q), want (text/html, <h1>hi</h1>)", ct, data)
	}
}

// TestRequestReply verifies a request gets the responder's reply over NATS, with
// vars propagated as headers in both directions.
func TestRequestReply(t *testing.T) {
	q := newNATSQueues(connect(t, runServer(t)), "dep-1")
	ctx := context.Background()

	sub, err := q.Subscribe(ctx, "greet", func(_ context.Context, m types.Message) (types.Message, error) {
		name, _ := m.Body.(string)
		tenant, _ := m.Variables.String("tenant")
		out := bodyMessage("hello " + name)
		out.Variables.Set("tenant", tenant)
		return out, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	in := bodyMessage("world")
	in.Variables.Set("tenant", "acme")

	reply, err := q.Request(ctx, "greet", in, core.WithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got, _ := reply.Body.(string); got != "hello world" {
		t.Fatalf("reply body = %q, want %q", got, "hello world")
	}
	if got, _ := reply.Variables.String("tenant"); got != "acme" {
		t.Fatalf("reply tenant var = %q, want acme", got)
	}
}

// TestQueueGroupCompetingConsumers verifies the deployment's queue group delivers
// each published message to exactly one consumer, even across subscriptions.
func TestQueueGroupCompetingConsumers(t *testing.T) {
	q := newNATSQueues(connect(t, runServer(t)), "dep-1")
	ctx := context.Background()

	const total = 100
	var handled atomic.Int64
	var wg sync.WaitGroup
	wg.Add(total)

	handler := func(_ context.Context, _ types.Message) (types.Message, error) {
		handled.Add(1)
		wg.Done()
		return types.Message{}, nil
	}
	for i := 0; i < 2; i++ {
		sub, err := q.Subscribe(ctx, "jobs", handler, core.WithListeners(3))
		if err != nil {
			t.Fatalf("Subscribe: %v", err)
		}
		defer func() { _ = sub.Close() }()
	}

	for i := 0; i < total; i++ {
		if err := q.Publish(ctx, "jobs", types.Message{}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("only %d/%d handled", handled.Load(), total)
	}

	// Give any erroneous duplicate deliveries a moment to land, then confirm each
	// message was handled exactly once.
	time.Sleep(100 * time.Millisecond)
	if got := handled.Load(); got != total {
		t.Fatalf("handled %d, want exactly %d", got, total)
	}
}

// TestSubjectScopingIsolatesDeployments verifies two deployments sharing a broker
// do not see each other's messages on the same user subject.
func TestSubjectScopingIsolatesDeployments(t *testing.T) {
	url := runServer(t)
	a := newNATSQueues(connect(t, url), "dep-a")
	b := newNATSQueues(connect(t, url), "dep-b")
	ctx := context.Background()

	got := make(chan struct{}, 1)
	sub, err := a.Subscribe(ctx, "shared", func(_ context.Context, _ types.Message) (types.Message, error) {
		got <- struct{}{}
		return types.Message{}, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// Publishing from deployment b on the same user subject must not reach a.
	if err := b.Publish(ctx, "shared", types.Message{}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case <-got:
		t.Fatal("deployment a received deployment b's message")
	case <-time.After(200 * time.Millisecond):
	}
}
