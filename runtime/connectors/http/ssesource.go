// The streaming half of the http route source: one request held open while the
// flow — and anything the flow handed its work to — writes frames to it.
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// handleSSE turns one request into a flow execution that streams. It mirrors
// handle, except the response is a live connection blocks write to rather than a
// result the flow returns.
func (s *source) handleSSE(w http.ResponseWriter, r *http.Request) {
	body, status, ok := s.readBody(w, r)
	if !ok {
		writeError(w, status, "invalid request body")
		return
	}
	if !s.acquireStream() {
		writeError(w, http.StatusServiceUnavailable, "too many open streams")
		return
	}
	defer s.releaseStream()
	if !s.acquireSend() {
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}

	msg, st, err := s.openStream(w, r, body)
	if err != nil {
		s.releaseSend()
		writeError(w, http.StatusInternalServerError, "could not open stream")
		return
	}
	// Closing before forgetting is what makes returning from this handler safe: a
	// block that resolved the stream just before it was forgotten still finds it
	// closed, and close cannot return while a write is in flight. After that, no
	// goroutine can touch the ResponseWriter again.
	defer s.conn.forgetStream(st.id)
	defer st.close()

	ch := s.conn.track(msg.EventID)
	defer s.conn.forget(msg.EventID)

	sent := s.send(r, msg)
	s.releaseSend()
	if !sent {
		writeError(w, http.StatusServiceUnavailable, "server is shutting down")
		return
	}

	s.awaitStream(w, r, ch, st)
}

// openStream builds the request's message and the stream it will be written to,
// stamping the stream's id into the message so any block downstream — in this
// flow, a sub-flow, or a job this flow queues — can address it.
func (s *source) openStream(
	w http.ResponseWriter, r *http.Request, body []byte,
) (*types.Message, *sseStream, error) {
	msg, err := s.buildMessage(r, body)
	if err != nil {
		return nil, nil, err
	}
	st, err := newSSEStream(s, w)
	if err != nil {
		return nil, nil, err
	}
	msg.Variables.Set(s.sse.streamVar, streamAddress(s.connectorName, st.id))
	s.conn.trackStream(st)
	slog.Debug("sse stream opened", "stream", st.id, "pattern", s.pattern)
	return msg, st, nil
}

// awaitStream holds the request open until the stream ends. Unlike awaitResult it
// can see the flow's terminal event and keep going: with closeOnComplete off, the
// invocation that opened the stream is only the first of several that may write.
func (s *source) awaitStream(w http.ResponseWriter, r *http.Request, ch chan result, st *sseStream) {
	// The route timeout bounds the wait for the *first* frame, not the whole
	// stream. Because nothing is written until then, a flow that hangs still gets a
	// clean 504 rather than a client parked on a half-open connection.
	firstFrame, stopFirstFrame := timerFor(s.timeout)
	defer stopFirstFrame()
	heartbeat, stopHeartbeat := tickerFor(s.sse.heartbeat)
	defer stopHeartbeat()
	maxDuration, stopMaxDuration := timerFor(s.sse.maxDuration)
	defer stopMaxDuration()

	for {
		select {
		case res := <-ch:
			if s.finish(w, st, res) {
				return
			}
		case <-st.done:
			return
		case <-heartbeat:
			// A failed heartbeat closes the stream itself, so the next turn of the
			// loop leaves through st.done. One exit is easier to reason about than
			// two that mean the same thing.
			_ = st.ping()
		case <-firstFrame:
			if s.haltUnopened(w, st, http.StatusGatewayTimeout, "flow timed out") {
				return
			}
		case <-maxDuration:
			slog.Warn("sse stream hit its max duration", "stream", st.id, "pattern", s.pattern)
			return
		case <-r.Context().Done():
			return
		case <-s.conn.done:
			s.haltUnopened(w, st, http.StatusServiceUnavailable, "server is shutting down")
			return
		}
	}
}

// haltUnopened answers a request that has not streamed anything yet with an
// ordinary error response, and reports whether it did. A committed stream is past
// the point where a status means anything, so it is left alone — which is also why
// the first-frame timeout stops applying once a frame has gone out.
//
// Claiming rather than testing is what keeps it safe: a detached invocation could
// otherwise commit the stream between the test and the write, and this would put a
// status line and a JSON body into the middle of a live stream.
func (s *source) haltUnopened(w http.ResponseWriter, st *sseStream, status int, message string) bool {
	if !st.claimUnopened() {
		return false
	}
	writeError(w, status, message)
	return true
}

// finish handles the flow's terminal event and reports whether the handler is
// done. With closeOnComplete off it is not: the flow has handed its work on, and
// the invocations it handed it to are the ones with something to say.
func (s *source) finish(w http.ResponseWriter, st *sseStream, res result) bool {
	// A flow that only kicked work off has nothing to say yet, and answering now
	// would end the response before the work it started could write to it. A
	// failure is still answered: a dispatch that failed is a stream nothing will
	// ever write to. The isOpen here is only a hint — claimUnopened below is what
	// actually decides, atomically.
	if !st.isOpen() && !s.sse.closeOnComplete && res.kind != types.FlowEventFailed {
		return false
	}
	// Nothing streamed, and now nothing can: the route is still an ordinary one, so
	// the flow's status, headers and body all apply. That is the point of opening
	// lazily.
	if st.claimUnopened() {
		s.writeResult(w, res)
		return true
	}
	switch res.kind {
	case types.FlowEventCompleted:
		if s.sse.finalEvent {
			s.write(st, res.msg, frame{event: s.sse.finalEventName, data: messageData(res.msg)})
		}
	case types.FlowEventFailed:
		// The response is already a committed 200, so a failure can only be told,
		// not signalled by status. A flow that wants to say more puts its own
		// sse-event in its error: chain; this is what a client sees when it does
		// not, and it beats an EOF indistinguishable from a clean end of stream.
		s.write(st, res.msg, frame{event: errorKey, data: errorFrameData(res.err)})
	case types.FlowEventDropped:
		// A dropped message has nothing to send. Closing says so.
	}
	return s.sse.closeOnComplete
}

// write emits a frame the source itself authored, logging rather than propagating
// a failure: the handler is on its way out either way, and a client that has gone
// away is the ordinary reason.
func (s *source) write(st *sseStream, msg *types.Message, f frame) {
	if err := st.emit(msg, f); err != nil {
		slog.Debug("sse final frame not delivered", "stream", st.id, "error", err)
	}
}

// errorFrameData renders the default error frame's payload, matching the shape
// writeError uses on the non-stream path so a client parses failures one way.
func errorFrameData(err error) string {
	message := "flow processing failed"
	if err != nil {
		message = err.Error()
	}
	raw, marshalErr := json.Marshal(map[string]string{errorKey: message})
	if marshalErr != nil {
		return `{"error":"flow processing failed"}`
	}
	return string(raw)
}

// acquireStream reserves one of the route's stream slots. Streams are held open
// by the client, not by the flow, so an unbounded route lets a caller take
// connections until the process runs out of descriptors.
func (s *source) acquireStream() bool {
	if s.sse.maxStreams <= 0 {
		return true
	}
	if s.sseLive.Add(1) > int64(s.sse.maxStreams) {
		s.sseLive.Add(-1)
		return false
	}
	return true
}

// releaseStream returns a stream slot.
func (s *source) releaseStream() {
	if s.sse.maxStreams > 0 {
		s.sseLive.Add(-1)
	}
}

// timerFor returns a one-shot channel and its stop func. A non-positive duration
// yields a nil channel, which blocks forever in a select — the idiom for "this
// bound is not configured".
func timerFor(d time.Duration) (<-chan time.Time, func()) {
	if d <= 0 {
		return nil, func() {}
	}
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// tickerFor is timerFor for a repeating channel.
func tickerFor(d time.Duration) (<-chan time.Time, func()) {
	if d <= 0 {
		return nil, func() {}
	}
	t := time.NewTicker(d)
	return t.C, t.Stop
}
