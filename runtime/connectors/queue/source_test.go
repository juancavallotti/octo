package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// newHandleSource builds a source wired to the connector with a buffered output
// channel, ready to drive handle directly without a queue backend.
func newHandleSource(t *testing.T, c *Connector) (*source, chan *types.Message) {
	t.Helper()
	out := make(chan *types.Message, 1)
	return &source{conn: c, out: out, subject: "work", done: make(chan struct{})}, out
}

// echoWorker reads one message, applies fn to set the terminal outcome, and
// publishes it keyed by the message EventID so the parked handler can respond.
func echoWorker(out <-chan *types.Message, fn func(msg *types.Message) types.FlowEvent) {
	go func() {
		msg, ok := <-out
		if !ok {
			return
		}
		ev := fn(msg)
		ev.EventID = msg.EventID
		ev.OccurredAt = time.Now()
		core.DefaultEventBus().Publish(ev)
	}()
}

func TestHandleCompletedReturnsResult(t *testing.T) {
	c := startConnector(t)
	s, out := newHandleSource(t, c)

	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = "processed"
		msg.Variables.Set("handled", true)
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	in, _ := types.NewMessage("corr-1")
	in.Body = "request"
	reply, err := s.handle(context.Background(), *in)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if reply.Body != "processed" {
		t.Fatalf("reply body = %v, want processed", reply.Body)
	}
	if handled, _ := reply.Variables.Bool("handled"); !handled {
		t.Fatalf("reply did not carry the handled variable: %+v", reply.Variables)
	}
}

func TestHandleFailedReturnsError(t *testing.T) {
	c := startConnector(t)
	s, out := newHandleSource(t, c)

	wantErr := errors.New("boom")
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		return types.FlowEvent{Kind: types.FlowEventFailed, Result: msg, Err: wantErr}
	})

	in, _ := types.NewMessage("")
	_, err := s.handle(context.Background(), *in)
	if !errors.Is(err, wantErr) {
		t.Fatalf("handle err = %v, want %v", err, wantErr)
	}
}

func TestHandleDroppedReturnsInputUnchanged(t *testing.T) {
	c := startConnector(t)
	s, out := newHandleSource(t, c)

	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		return types.FlowEvent{Kind: types.FlowEventDropped, Result: msg}
	})

	in, _ := types.NewMessage("")
	in.Body = "kept"
	reply, err := s.handle(context.Background(), *in)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if reply.Body != "kept" {
		t.Fatalf("reply body = %v, want kept", reply.Body)
	}
}

func TestHandleTimesOutWhenFlowNeverFinishes(t *testing.T) {
	c := startConnector(t)
	s, out := newHandleSource(t, c)
	s.timeout = 50 * time.Millisecond

	// Drain the message but never publish a terminal event.
	go func() { <-out }()

	in, _ := types.NewMessage("")
	_, err := s.handle(context.Background(), *in)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

func TestHandleStopUnblocksParkedHandler(t *testing.T) {
	c := startConnector(t)
	s, out := newHandleSource(t, c)
	go func() { <-out }()

	in, _ := types.NewMessage("")
	errc := make(chan error, 1)
	go func() {
		_, err := s.handle(context.Background(), *in)
		errc <- err
	}()

	close(s.done)
	select {
	case err := <-errc:
		if !errors.Is(err, errSourceStopped) {
			t.Fatalf("err = %v, want errSourceStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handle did not return after stop")
	}
}
