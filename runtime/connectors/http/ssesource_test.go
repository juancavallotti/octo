package http

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// sseFrame is one decoded event. A heartbeat comment is reported as a frame of
// its own so a test can assert the stream is being kept alive.
type sseFrame struct {
	event   string
	data    string
	comment string
}

// sseClient is an in-progress SSE response: the request is still open and frames
// are read as they arrive, which is what the buffered do() helper cannot do.
type sseClient struct {
	resp   *http.Response
	reader *bufio.Reader
}

// openSSE issues the request and waits for its response headers. On an SSE route
// those are written by the first frame, so this returns as soon as the flow emits
// — or, if it never does, with whatever ordinary response the route fell back to.
func openSSE(t *testing.T, url string) *sseClient {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// No client timeout: staying open is the point.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return &sseClient{resp: resp, reader: bufio.NewReader(resp.Body)}
}

// next reads one event: the lines up to the next blank line.
func (c *sseClient) next(t *testing.T) sseFrame {
	t.Helper()
	var (
		f     sseFrame
		lines []string
	)
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		switch line = strings.TrimSuffix(line, "\n"); {
		case line == "":
			f.data = strings.Join(lines, "\n")
			return f
		case strings.HasPrefix(line, ":"):
			f.comment = strings.TrimSpace(line[1:])
		case strings.HasPrefix(line, "event:"):
			f.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
}

// expectClosed asserts the server ended the stream with nothing further to say.
func (c *sseClient) expectClosed(t *testing.T) {
	t.Helper()
	rest, err := io.ReadAll(c.reader)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("unexpected trailing data %q", rest)
	}
}

// streamFrom resolves the stream a route addressed on msg, the way the sse-event
// block does: the variable holds an address naming both connector and stream.
func streamFrom(c *Connector, msg *types.Message) *sseStream {
	address, ok := msg.Variables.String(defaultSSEStreamVar)
	if !ok {
		return nil
	}
	_, id := parseStreamAddress(address)
	st, _ := c.stream(id)
	return st
}

// streamWorker reads one message and hands it to fn together with the stream the
// route opened for it, standing in for the blocks that emit. Whatever fn returns
// is published as the flow's terminal event.
func streamWorker(
	c *Connector, out <-chan *types.Message,
	fn func(msg *types.Message, st *sseStream) types.FlowEvent,
) {
	go func() {
		msg, ok := <-out
		if !ok {
			return
		}
		st := streamFrom(c, msg)
		ev := fn(msg, st)
		ev.EventID = msg.EventID
		ev.OccurredAt = time.Now()
		core.DefaultEventBus().Publish(ev)
	}()
}

// ssePath is the route every SSE test registers.
const ssePath = "/stream"

// sseRoute is the settings for an SSE route, with the sub-settings a test wants
// merged into the nested group.
func sseRoute(sse map[string]any) map[string]any {
	settings := map[string]any{"enabled": true}
	for k, v := range sse {
		settings[k] = v
	}
	return map[string]any{"path": ssePath, "sse": settings}
}

func TestSSEStreamsFramesThenCloses(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(nil))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		for _, data := range []string{`{"n":1}`, `{"n":2}`} {
			if err := st.emit(msg, frame{event: "tick", data: data}); err != nil {
				t.Errorf("emit: %v", err)
			}
		}
		msg.Body = map[string]any{"done": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if client.resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", client.resp.StatusCode)
	}
	if ct := client.resp.Header.Get("Content-Type"); ct != sseContentType {
		t.Errorf("Content-Type = %q, want %q", ct, sseContentType)
	}

	for _, want := range []string{`{"n":1}`, `{"n":2}`} {
		got := client.next(t)
		if got.event != "tick" || got.data != want {
			t.Errorf("frame = %+v, want event tick data %s", got, want)
		}
	}
	// finalEvent is off, so the terminal message closes without being sent.
	client.expectClosed(t)
}

func TestSSEFinalEvent(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{
		"finalEvent":     true,
		"finalEventName": "done",
	}))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		if err := st.emit(msg, frame{data: "first"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		msg.Body = map[string]any{"total": float64(2)}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if got := client.next(t); got.data != "first" {
		t.Errorf("first frame = %+v", got)
	}
	final := client.next(t)
	if final.event != "done" || final.data != `{"total":2}` {
		t.Errorf("final frame = %+v, want event done with the response body", final)
	}
	client.expectClosed(t)
}

// TestSSEUnopenedFallsBackToBufferedResponse is the payoff of opening lazily: a
// flow that emits nothing is still an ordinary route, so its status, headers and
// body all apply. Without it an SSE route could never reject a request.
func TestSSEUnopenedFallsBackToBufferedResponse(t *testing.T) {
	c, base := startConnector(t, nil)
	settings := sseRoute(nil)
	settings["responseHeaders"] = []string{"WWW-Authenticate"}
	out := newSource(t, c, settings)

	streamWorker(c, out, func(msg *types.Message, _ *sseStream) types.FlowEvent {
		msg.Body = map[string]any{errorKey: "unauthorized"}
		msg.Variables.Set(httpStatusVar, http.StatusUnauthorized)
		msg.Variables.Set("WWW-Authenticate", `Bearer realm="octo"`)
		msg.RequestStop()
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/stream", nil)
	resp := do(t, req)

	if resp.status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.status)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := resp.header.Get("WWW-Authenticate"); got != `Bearer realm="octo"` {
		t.Errorf("WWW-Authenticate = %q", got)
	}
	if !bytes.Contains(resp.body, []byte("unauthorized")) {
		t.Errorf("body = %s", resp.body)
	}
}

// TestSSEUnhandledFailureSendsErrorFrame covers the case where the response is
// already a committed 200: the failure can only be told, and a bare EOF would be
// indistinguishable from a clean end of stream.
func TestSSEUnhandledFailureSendsErrorFrame(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(nil))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		if err := st.emit(msg, frame{data: "partial"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		return types.FlowEvent{
			Kind:   types.FlowEventFailed,
			Result: msg,
			Err:    errors.New("upstream exploded"),
		}
	})

	client := openSSE(t, base+"/stream")
	if client.resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (the stream was already committed)", client.resp.StatusCode)
	}
	if got := client.next(t); got.data != "partial" {
		t.Errorf("first frame = %+v", got)
	}
	failure := client.next(t)
	if failure.event != errorKey || !strings.Contains(failure.data, "upstream exploded") {
		t.Errorf("error frame = %+v", failure)
	}
	client.expectClosed(t)
}

// TestSSEFailureBeforeAnyFrameIsAnOrdinary500 pins the other half: nothing is
// committed, so the failure gets a real status rather than a frame.
func TestSSEFailureBeforeAnyFrameIsAnOrdinary500(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(nil))

	streamWorker(c, out, func(msg *types.Message, _ *sseStream) types.FlowEvent {
		return types.FlowEvent{Kind: types.FlowEventFailed, Result: msg, Err: errors.New("boom")}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/stream", nil)
	resp := do(t, req)
	if resp.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.status)
	}
	if !bytes.Contains(resp.body, []byte("boom")) {
		t.Errorf("body = %s", resp.body)
	}
}

// TestSSEStreamOutlivesItsInvocation is the case a stream id of its own exists
// for: the flow finishes early, having handed its work somewhere else, and the
// detached work writes to the same connection afterwards.
func TestSSEStreamOutlivesItsInvocation(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{"closeOnComplete": false}))

	detached := make(chan string, 1)
	streamWorker(c, out, func(msg *types.Message, _ *sseStream) types.FlowEvent {
		// Hand the address on the way a flow would to a queued job, then finish.
		address, _ := msg.Variables.String(defaultSSEStreamVar)
		detached <- address
		msg.Body = map[string]any{"accepted": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	go func() {
		address := <-detached
		msg, err := types.NewMessage("")
		if err != nil {
			return
		}
		_, id := parseStreamAddress(address)
		st, ok := c.stream(id)
		if !ok {
			return
		}
		_ = st.emit(msg, frame{event: "progress", data: "50"})
		_ = st.emit(msg, frame{event: "progress", data: "100"})
		st.close()
	}()

	client := openSSE(t, base+"/stream")
	for _, want := range []string{"50", "100"} {
		got := client.next(t)
		if got.event != "progress" || got.data != want {
			t.Errorf("frame = %+v, want progress %s", got, want)
		}
	}
	client.expectClosed(t)
}

// TestSSEEmitAfterHandlerReturnedIsInert is the invariant that makes detached
// emits safe: once the handler has returned, writing to its ResponseWriter is
// undefined, so a stream held past that point must refuse to write.
func TestSSEEmitAfterHandlerReturnedIsInert(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(nil))

	held := make(chan *sseStream, 1)
	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		if err := st.emit(msg, frame{data: "one"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		held <- st
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if got := client.next(t); got.data != "one" {
		t.Errorf("frame = %+v", got)
	}
	client.expectClosed(t)

	st := <-held
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if err := st.emit(msg, frame{data: "too late"}); !errors.Is(err, errStreamClosed) {
		t.Errorf("emit after the handler returned = %v, want errStreamClosed", err)
	}
	if _, ok := c.stream(st.id); ok {
		t.Error("the stream should have been forgotten when the handler returned")
	}
}

func TestSSEHeartbeatKeepsAnIdleStreamAlive(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{
		"heartbeat":       "20ms",
		"closeOnComplete": false,
	}))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		// Open the stream, then go quiet: only heartbeats should follow.
		if err := st.emit(msg, frame{data: "hello"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if got := client.next(t); got.data != "hello" {
		t.Errorf("frame = %+v", got)
	}
	if got := client.next(t); got.comment == "" {
		t.Errorf("expected a heartbeat comment, got %+v", got)
	}
}

func TestSSEMaxDurationClosesTheStream(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{
		"closeOnComplete": false,
		"maxDuration":     "80ms",
		"heartbeat":       "1h", // out of the way
	}))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		if err := st.emit(msg, frame{data: "hello"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if got := client.next(t); got.data != "hello" {
		t.Errorf("frame = %+v", got)
	}
	client.expectClosed(t)
}

// TestSSEFirstFrameTimeout pins that the route timeout still bounds a flow that
// never emits — and that because nothing is committed, it is a real 504.
func TestSSEFirstFrameTimeout(t *testing.T) {
	c, base := startConnector(t, map[string]any{"requestTimeout": "50ms"})
	out := newSource(t, c, sseRoute(nil))

	// A worker that takes the message and never answers.
	go func() { <-out }()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/stream", nil)
	resp := do(t, req)
	if resp.status != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", resp.status)
	}
}

func TestSSEMaxStreams(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{
		"maxStreams":      1,
		"closeOnComplete": false,
	}))

	go func() {
		for msg := range out {
			if st := streamFrom(c, msg); st != nil {
				_ = st.emit(msg, frame{data: "open"})
			}
		}
	}()

	first := openSSE(t, base+"/stream")
	if got := first.next(t); got.data != "open" {
		t.Fatalf("first stream frame = %+v", got)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/stream", nil)
	resp := do(t, req)
	if resp.status != http.StatusServiceUnavailable {
		t.Errorf("second stream status = %d, want 503", resp.status)
	}
}

// TestSSEConnectorStopClosesOpenStreams pins the shutdown hazard: Shutdown waits
// for handlers to return, so a stream nobody closes would hold teardown open
// until the context deadline.
func TestSSEConnectorStopClosesOpenStreams(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, sseRoute(map[string]any{"closeOnComplete": false}))

	streamWorker(c, out, func(msg *types.Message, st *sseStream) types.FlowEvent {
		if err := st.emit(msg, frame{data: "open"}); err != nil {
			t.Errorf("emit: %v", err)
		}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	client := openSSE(t, base+"/stream")
	if got := client.next(t); got.data != "open" {
		t.Fatalf("frame = %+v", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Stop(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop blocked on an open stream")
	}
	client.expectClosed(t)
}

// TestSSEDisabledLeavesTheRouteUnchanged guards the whole feature being inert by
// default: the settings group exists on every route, and an untouched one must
// not alter a thing.
func TestSSEDisabledLeavesTheRouteUnchanged(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{"path": "/plain"})

	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any{"ok": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/plain", nil)
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.status)
	}
	if ct := resp.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(string(resp.body)); got != `{"ok":true}` {
		t.Errorf("body = %s", got)
	}
}
