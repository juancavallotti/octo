package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestWriteFrame(t *testing.T) {
	tests := []struct {
		name string
		in   frame
		want string
	}{
		{"data only", frame{data: `{"a":1}`}, "data: {\"a\":1}\n\n"},
		{"named event", frame{event: "progress", data: "50"}, "event: progress\ndata: 50\n\n"},
		{"empty data", frame{}, "data: \n\n"},
		// A bare newline inside a value ends the field, so it has to become a
		// second data line or the rest of the payload is silently dropped.
		{"multi-line data", frame{data: "one\ntwo"}, "data: one\ndata: two\n\n"},
		{"crlf data", frame{data: "one\r\ntwo"}, "data: one\ndata: two\n\n"},
		{"lone cr data", frame{data: "one\rtwo"}, "data: one\ndata: two\n\n"},
		// An event name is a single-line field; a newline in it would split the frame.
		{"newline in event name", frame{event: "a\nb", data: "x"}, "event: a b\ndata: x\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			if err := writeFrame(&b, tt.in); err != nil {
				t.Fatalf("writeFrame: %v", err)
			}
			if got := b.String(); got != tt.want {
				t.Errorf("frame = %q, want %q", got, tt.want)
			}
		})
	}
}

// newTestStream builds a stream over a recorder, with the source config the
// stream reads at open time.
func newTestStream(t *testing.T, src *source) (*sseStream, *httptest.ResponseRecorder) {
	t.Helper()
	if src == nil {
		src = &source{}
	}
	rec := httptest.NewRecorder()
	st, err := newSSEStream(src, rec)
	if err != nil {
		t.Fatalf("newSSEStream: %v", err)
	}
	return st, rec
}

func testMessage(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	return msg
}

func TestStreamOpensOnFirstFrame(t *testing.T) {
	st, rec := newTestStream(t, nil)
	msg := testMessage(t)

	if st.isOpen() {
		t.Fatal("a stream must not commit the response before its first frame")
	}
	if err := st.emit(msg, frame{data: "one"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !st.isOpen() {
		t.Error("stream should be open after a frame")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != sseContentType {
		t.Errorf("Content-Type = %q, want %q", ct, sseContentType)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache, no-transform" {
		t.Errorf("Cache-Control = %q, want no-cache, no-transform", cc)
	}
	if b := rec.Header().Get("X-Accel-Buffering"); b != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", b)
	}
	if got := rec.Body.String(); got != "data: one\n\n" {
		t.Errorf("body = %q", got)
	}
}

// TestStreamOpenIgnoresHTTPStatusVar pins that a stream is always 200: EventSource
// treats any other status as a failure, so vars.httpStatus applies only to the
// non-stream path.
func TestStreamOpenIgnoresHTTPStatusVar(t *testing.T) {
	st, rec := newTestStream(t, nil)
	msg := testMessage(t)
	msg.Variables.Set(httpStatusVar, float64(http.StatusAccepted))

	if err := st.emit(msg, frame{data: "x"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestStreamOpenPropagatesResponseHeaders locks the payoff of opening lazily: the
// route's configured response headers are read off the message that emits the
// first frame, so a block can still shape them.
func TestStreamOpenPropagatesResponseHeaders(t *testing.T) {
	st, rec := newTestStream(t, &source{respHeaders: []string{"X-Trace", "Content-Type"}})
	msg := testMessage(t)
	msg.Variables.Set("X-Trace", "abc123")
	msg.Variables.Set("Content-Type", "text/evil") // source-managed, must not win

	if err := st.emit(msg, frame{data: "x"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if got := rec.Header().Get("X-Trace"); got != "abc123" {
		t.Errorf("X-Trace = %q, want abc123", got)
	}
	if got := rec.Header().Get("Content-Type"); got != sseContentType {
		t.Errorf("Content-Type = %q, want %q", got, sseContentType)
	}
}

func TestStreamOpensOnce(t *testing.T) {
	st, rec := newTestStream(t, nil)
	msg := testMessage(t)

	for _, data := range []string{"one", "two", "three"} {
		if err := st.emit(msg, frame{data: data}); err != nil {
			t.Fatalf("emit %s: %v", data, err)
		}
	}
	want := "data: one\n\ndata: two\n\ndata: three\n\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	// The recorder records only the first WriteHeader, so a second open would show
	// up as duplicated headers rather than a wrong code; check the body shape above
	// plus that nothing re-wrote the header block.
	if n := strings.Count(rec.Body.String(), sseContentType); n != 0 {
		t.Errorf("headers leaked into the body %d times", n)
	}
}

// TestEmitAfterCloseIsInert is the invariant the whole design rests on: a block
// holding a stream from a detached invocation may outlive the handler, and once
// the handler has returned, touching its ResponseWriter is undefined. A closed
// stream must therefore write nothing at all.
func TestEmitAfterCloseIsInert(t *testing.T) {
	st, rec := newTestStream(t, nil)
	msg := testMessage(t)

	if err := st.emit(msg, frame{data: "before"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	st.close()

	before := rec.Body.String()
	if err := st.emit(msg, frame{data: "after"}); !errors.Is(err, errStreamClosed) {
		t.Errorf("emit after close = %v, want errStreamClosed", err)
	}
	if err := st.ping(); !errors.Is(err, errStreamClosed) {
		t.Errorf("ping after close = %v, want errStreamClosed", err)
	}
	if got := rec.Body.String(); got != before {
		t.Errorf("a closed stream wrote %q", strings.TrimPrefix(got, before))
	}
}

// TestCloseOnUnopenedStreamWritesNothing covers the fallback path: a stream that
// never emitted must leave the response untouched so writeResult can shape it.
func TestCloseOnUnopenedStreamWritesNothing(t *testing.T) {
	st, rec := newTestStream(t, nil)
	st.close()

	if st.isOpen() {
		t.Error("closing an unopened stream must not open it")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}
	if _, ok := rec.Header()[http.CanonicalHeaderKey("Content-Type")]; ok {
		t.Error("an unopened stream must not set Content-Type")
	}
}

// TestClaimUnopened pins the atomicity the fallback response depends on. Testing
// isOpen and then writing would be a race: a stream is addressable by id, so a
// detached invocation can commit it in between, and the handler would then write a
// second status line and a JSON body into the middle of a live stream. Claiming
// closes the stream in the same critical section, so the racing emit loses.
func TestClaimUnopened(t *testing.T) {
	t.Run("an unopened stream is claimable, and the claim shuts emits out", func(t *testing.T) {
		st, rec := newTestStream(t, nil)

		if !st.claimUnopened() {
			t.Fatal("an unopened stream should be claimable")
		}
		if err := st.emit(testMessage(t), frame{data: "racer"}); !errors.Is(err, errStreamClosed) {
			t.Errorf("emit after a claim = %v, want errStreamClosed", err)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("a claimed stream let a write through: %q", rec.Body.String())
		}
		if _, ok := rec.Header()[http.CanonicalHeaderKey("Content-Type")]; ok {
			t.Error("a claimed stream must leave the response uncommitted")
		}
	})

	t.Run("a committed stream is not claimable", func(t *testing.T) {
		st, _ := newTestStream(t, nil)
		if err := st.emit(testMessage(t), frame{data: "x"}); err != nil {
			t.Fatalf("emit: %v", err)
		}
		if st.claimUnopened() {
			t.Error("a stream that has already streamed must not be claimable")
		}
	})

	// A block that closes without emitting leaves the response unwritten and still
	// owed to the caller, so closing must not forfeit the claim.
	t.Run("a closed but unopened stream is still claimable", func(t *testing.T) {
		st, _ := newTestStream(t, nil)
		st.close()
		if !st.claimUnopened() {
			t.Error("a stream closed before it streamed still owes a response")
		}
	})
}

func TestCloseIsIdempotentAndSignalsDone(t *testing.T) {
	st, _ := newTestStream(t, nil)

	select {
	case <-st.done:
		t.Fatal("done is closed before close()")
	default:
	}

	st.close()
	st.close() // must not panic on a second close of done

	select {
	case <-st.done:
	default:
		t.Error("close() must signal done")
	}
}

// TestPingDoesNotOpenStream pins that a heartbeat never commits a response whose
// outcome is still undecided — otherwise an idle flow that was going to reject
// with a 401 would be locked into a 200 stream by a timer.
func TestPingDoesNotOpenStream(t *testing.T) {
	st, rec := newTestStream(t, nil)

	if err := st.ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if st.isOpen() {
		t.Error("ping must not open the stream")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}

	if err := st.emit(testMessage(t), frame{data: "x"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := st.ping(); err != nil {
		t.Fatalf("ping after open: %v", err)
	}
	if got := rec.Body.String(); got != "data: x\n\n"+sseComment {
		t.Errorf("body = %q", got)
	}
}

// TestStreamIDsAreIndependent pins that a stream's address is minted rather than
// derived from the message: nothing a flow does to EventID can invalidate it.
func TestStreamIDsAreIndependent(t *testing.T) {
	msg := testMessage(t)
	seen := make(map[string]bool)
	for range 100 {
		st, _ := newTestStream(t, nil)
		if st.id == "" {
			t.Fatal("stream id is empty")
		}
		if st.id == msg.EventID {
			t.Fatal("stream id must not be derived from a message EventID")
		}
		if seen[st.id] {
			t.Fatalf("duplicate stream id %q", st.id)
		}
		seen[st.id] = true
	}
}

// TestStreamAddressRoundTrip pins that an address is self-describing: a block
// handed one knows both which connector holds the stream and which stream, so it
// needs no configuration to act on an address that reached it from anywhere.
func TestStreamAddressRoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		wantConnector string
		wantID        string
	}{
		{"connector and id", streamAddress("api", "abc123"), "api", "abc123"},
		{"default connector", streamAddress(connectorType, "abc123"), connectorType, "abc123"},
		// A bare id is still usable; the block's own connector setting decides.
		{"bare id", "abc123", "", "abc123"},
		// The id is hex, so the last separator is the boundary.
		{"connector containing a colon", streamAddress("a:b", "abc123"), "a:b", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			connector, id := parseStreamAddress(tt.address)
			if connector != tt.wantConnector || id != tt.wantID {
				t.Errorf("parseStreamAddress(%q) = %q, %q; want %q, %q",
					tt.address, connector, id, tt.wantConnector, tt.wantID)
			}
		})
	}
}

func TestConnectorStreamRegistry(t *testing.T) {
	c := &Connector{streams: make(map[string]*sseStream)}
	st, _ := newTestStream(t, nil)

	if _, ok := c.stream(st.id); ok {
		t.Error("unregistered id should not resolve")
	}

	c.trackStream(st)
	got, ok := c.stream(st.id)
	if !ok || got != st {
		t.Fatalf("stream(%q) = %v, %v", st.id, got, ok)
	}

	c.forgetStream(st.id)
	c.forgetStream(st.id) // safe twice
	if _, ok := c.stream(st.id); ok {
		t.Error("forgotten id should not resolve")
	}
}
