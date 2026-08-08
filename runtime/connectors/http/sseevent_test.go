package http

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// sseEventDeps wires the block to c under the given connector name.
func sseEventDeps(name string, c *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == name {
			return c, true
		}
		return nil, false
	}}
}

// streamOn registers a fresh stream on c and returns it with a message already
// addressed to it, the way an SSE route bound to connector would hand one to a
// flow.
func streamOn(
	t *testing.T, c *Connector, connector string,
) (*sseStream, *httptest.ResponseRecorder, *types.Message) {
	t.Helper()
	st, rec := newTestStream(t, &source{})
	c.trackStream(st)
	msg := testMessage(t)
	msg.Variables.Set(defaultSSEStreamVar, streamAddress(connector, st.id))
	return st, rec, msg
}

func newConnectorWithStreams() *Connector {
	return &Connector{streams: make(map[string]*sseStream)}
}

// isClosed reports whether the stream has ended, off the signal close() raises.
func isClosed(st *sseStream) bool {
	select {
	case <-st.done:
		return true
	default:
		return false
	}
}

//nolint:ireturn // mirrors the BlockFactory signature under test
func newEventBlock(t *testing.T, settings map[string]any, deps core.BlockDeps) core.MessageProcessor {
	t.Helper()
	proc, err := newSSEEvent(settings, deps)
	if err != nil {
		t.Fatalf("newSSEEvent: %v", err)
	}
	return proc
}

func TestSSEEventWritesFrame(t *testing.T) {
	c := newConnectorWithStreams()
	_, rec, msg := streamOn(t, c, connectorType)
	msg.Body = map[string]any{"n": float64(1)}

	proc := newEventBlock(t, map[string]any{"event": "tick"}, sseEventDeps(connectorType, c))
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Error("the block should pass the message through")
	}
	// No data expression, so the body is the payload.
	if got := rec.Body.String(); got != "event: tick\ndata: {\"n\":1}\n\n" {
		t.Errorf("frame = %q", got)
	}
}

func TestSSEEventDataExpression(t *testing.T) {
	c := newConnectorWithStreams()
	_, rec, msg := streamOn(t, c, connectorType)
	msg.Body = map[string]any{"name": "ada"}

	proc := newEventBlock(t,
		map[string]any{"data": `"hello " + body.name`},
		sseEventDeps(connectorType, c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := rec.Body.String(); got != "data: hello ada\n\n" {
		t.Errorf("frame = %q", got)
	}
}

// TestSSEEventStreamExpression covers reaching a stream whose address arrived
// some other way — the queued-job case the minted address exists for. The block
// needs no configuration beyond the expression: the address names its own
// connector.
func TestSSEEventStreamExpression(t *testing.T) {
	c := newConnectorWithStreams()
	st, rec, _ := streamOn(t, c, "public-api")

	// A message that never passed through the SSE route: no stream variable, the
	// address travels in the body instead.
	msg := testMessage(t)
	msg.Body = map[string]any{
		"stream":   streamAddress("public-api", st.id),
		"progress": float64(50),
	}

	proc := newEventBlock(t, map[string]any{
		"stream": "body.stream",
		"data":   "body.progress",
	}, sseEventDeps("public-api", c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := rec.Body.String(); got != "data: 50\n\n" {
		t.Errorf("frame = %q", got)
	}
}

// TestSSEEventBareStreamIDUsesConnectorSetting covers an address with no
// connector in it — a hand-written id — which is what the block's connector
// setting is still there for.
func TestSSEEventBareStreamIDUsesConnectorSetting(t *testing.T) {
	c := newConnectorWithStreams()
	st, rec := newTestStream(t, &source{})
	c.trackStream(st)

	msg := testMessage(t)
	msg.Variables.Set(defaultSSEStreamVar, st.id) // no connector prefix

	proc := newEventBlock(t,
		map[string]any{"data": `"x"`, "connector": "public-api"},
		sseEventDeps("public-api", c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := rec.Body.String(); got != "data: x\n\n" {
		t.Errorf("frame = %q", got)
	}
}

// TestSSEEventStreamExpressionWinsOverVariable pins precedence: an explicit
// expression is the more specific instruction.
func TestSSEEventStreamExpressionWinsOverVariable(t *testing.T) {
	c := newConnectorWithStreams()
	_, viaVar, msg := streamOn(t, c, connectorType)
	target, viaExpr := newTestStream(t, &source{})
	c.trackStream(target)
	msg.Body = map[string]any{"stream": streamAddress(connectorType, target.id)}

	proc := newEventBlock(t,
		map[string]any{"stream": "body.stream", "data": `"x"`},
		sseEventDeps(connectorType, c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if viaVar.Body.Len() != 0 {
		t.Errorf("wrote to the variable's stream: %q", viaVar.Body.String())
	}
	if got := viaExpr.Body.String(); got != "data: x\n\n" {
		t.Errorf("frame = %q", got)
	}
}

func TestSSEEventCloseAndStop(t *testing.T) {
	tests := []struct {
		name       string
		settings   map[string]any
		wantClosed bool
		wantStop   bool
	}{
		{"neither", map[string]any{"data": `"x"`}, false, false},
		{"close only", map[string]any{"data": `"x"`, "close": true}, true, false},
		{"stop only", map[string]any{"data": `"x"`, "stop": true}, false, true},
		{"both", map[string]any{"data": `"x"`, "close": true, "stop": true}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConnectorWithStreams()
			st, _, msg := streamOn(t, c, connectorType)

			proc := newEventBlock(t, tt.settings, sseEventDeps(connectorType, c))
			out, err := proc.Process(context.Background(), msg)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if got := isClosed(st); got != tt.wantClosed {
				t.Errorf("closed = %v, want %v", got, tt.wantClosed)
			}
			if got := out.StopRequested(); got != tt.wantStop {
				t.Errorf("StopRequested = %v, want %v", got, tt.wantStop)
			}
		})
	}
}

// TestSSEEventCloseOnlyWritesNothing covers a block configured purely to end the
// stream: an empty frame would be noise on the wire.
func TestSSEEventCloseOnlyWritesNothing(t *testing.T) {
	c := newConnectorWithStreams()
	st, rec, msg := streamOn(t, c, connectorType)
	msg.Body = nil

	proc := newEventBlock(t, map[string]any{"close": true}, sseEventDeps(connectorType, c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if !isClosed(st) {
		t.Error("the stream should be closed")
	}
	if st.isOpen() {
		t.Error("a close-only block must not commit the response to a stream")
	}
}

// TestSSEEventNoStreamIDAlwaysFails pins the distinction the block draws: being
// wired up wrong is not the same as nobody listening, so no ifClosed policy
// rescues it.
func TestSSEEventNoStreamIDAlwaysFails(t *testing.T) {
	for _, policy := range []string{"", ifClosedStop, ifClosedIgnore, ifClosedError} {
		t.Run("ifClosed="+policy, func(t *testing.T) {
			c := newConnectorWithStreams()
			settings := map[string]any{"data": `"x"`}
			if policy != "" {
				settings["ifClosed"] = policy
			}
			proc := newEventBlock(t, settings, sseEventDeps(connectorType, c))

			_, err := proc.Process(context.Background(), testMessage(t))
			if err == nil {
				t.Fatal("expected an error naming the misconfiguration")
			}
			if !strings.Contains(err.Error(), "no stream id") {
				t.Errorf("error = %v, want it to name the missing stream id", err)
			}
		})
	}
}

func TestSSEEventIfClosed(t *testing.T) {
	tests := []struct {
		name     string
		policy   string
		wantErr  bool
		wantStop bool
	}{
		{"default stops the flow", "", false, true},
		{"stop", ifClosedStop, false, true},
		{"ignore carries on", ifClosedIgnore, false, false},
		{"error fails the block", ifClosedError, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConnectorWithStreams()
			st, _, msg := streamOn(t, c, connectorType)
			// The client hung up: the stream is closed but still addressed.
			st.close()

			settings := map[string]any{"data": `"x"`}
			if tt.policy != "" {
				settings["ifClosed"] = tt.policy
			}
			proc := newEventBlock(t, settings, sseEventDeps(connectorType, c))

			out, err := proc.Process(context.Background(), msg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if out.StopRequested() != tt.wantStop {
				t.Errorf("StopRequested = %v, want %v", out.StopRequested(), tt.wantStop)
			}
		})
	}
}

// TestSSEEventUnknownStreamUsesIfClosed covers an id that resolves to nothing —
// an expired stream, or one living on another replica.
func TestSSEEventUnknownStreamUsesIfClosed(t *testing.T) {
	c := newConnectorWithStreams()
	msg := testMessage(t)
	msg.Variables.Set(defaultSSEStreamVar, "deadbeef")

	proc := newEventBlock(t,
		map[string]any{"data": `"x"`, "ifClosed": ifClosedError},
		sseEventDeps(connectorType, c))

	_, err := proc.Process(context.Background(), msg)
	if !errors.Is(err, errStreamClosed) {
		t.Errorf("error = %v, want errStreamClosed", err)
	}
}

// TestSSEEventUnconfiguredConnectorFails pins that a misnamed connector is a
// configuration fault, not something ifClosed quietly absorbs.
func TestSSEEventUnconfiguredConnectorFails(t *testing.T) {
	c := newConnectorWithStreams()
	_, _, msg := streamOn(t, c, connectorType)

	// The connector is registered under a name of its own; the block still defaults.
	proc := newEventBlock(t,
		map[string]any{"data": `"x"`, "ifClosed": ifClosedIgnore},
		sseEventDeps("public-api", c))

	_, err := proc.Process(context.Background(), msg)
	if err == nil {
		t.Fatal("expected an error for an unresolvable connector")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Errorf("error = %v, want it to name the unconfigured connector", err)
	}
}

// TestSSEEventNamedConnectorNeedsNoSetting pins the ergonomics: a route bound to
// a connector of its own publishes an address that names it, so a block writing
// to that stream needs no connector setting.
func TestSSEEventNamedConnectorNeedsNoSetting(t *testing.T) {
	c := newConnectorWithStreams()
	_, rec, msg := streamOn(t, c, "public-api")

	proc := newEventBlock(t,
		map[string]any{"data": `"x"`},
		sseEventDeps("public-api", c))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := rec.Body.String(); got != "data: x\n\n" {
		t.Errorf("frame = %q", got)
	}
}

func TestSSEEventRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings map[string]any
	}{
		{"unknown ifClosed", map[string]any{"ifClosed": "explode"}},
		{"bad data expression", map[string]any{"data": "body."}},
		{"bad stream expression", map[string]any{"stream": "((("}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newSSEEvent(tt.settings, core.BlockDeps{}); err == nil {
				t.Fatal("expected a build error")
			}
		})
	}
}
