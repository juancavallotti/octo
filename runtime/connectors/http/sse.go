// Server-sent events for the http connector: one open connection, addressed by
// an id of its own, that any block can write a frame to.
package http

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// sseStreamIDBytes is the number of random bytes in a stream id.
	sseStreamIDBytes = 16
	// defaultSSEStreamVar is the variable a route stamps its stream address into.
	defaultSSEStreamVar = "sseStream"
	// streamAddressSep separates the connector from the stream id in an address.
	streamAddressSep = ":"
	// defaultSSEHeartbeat is how often an idle stream says something. Proxies and
	// load balancers cull a connection that has gone quiet for 30-60s, so a stream
	// that is merely waiting on slow work has to keep talking to survive one.
	defaultSSEHeartbeat = 15 * time.Second
	// sseWriteTimeout bounds a single frame write. Writes are synchronous on the
	// flow's goroutine, which is what gives a slow client real backpressure — this
	// is what keeps a client that has stopped reading entirely from parking a flow
	// worker forever.
	sseWriteTimeout = 10 * time.Second

	sseContentType = "text/event-stream"
	// sseComment is a no-op line the protocol allows; it is the heartbeat.
	sseComment = ": ping\n\n"
)

// errStreamClosed reports a write to a stream that has already ended: the client
// disconnected, a block closed it, the route timed out, or the connector is
// shutting down. It is an ordinary outcome, not a fault — see the sse-event
// block's ifClosed setting.
var errStreamClosed = errors.New("sse stream is closed")

// frame is one server-sent event.
type frame struct {
	event string
	data  string
}

// sseSettings turns a route into an event stream. It is inert unless Enabled, so
// a route that leaves it alone behaves exactly as it did before.
//
// The fields are grouped in an object rather than spread across the source's
// settings because the editor only evaluates showIf for an object's sub-fields —
// and only against strings, so a boolean gate would never match. A named group is
// the honest way to say "these belong to one feature".
type sseSettings struct {
	// Serve this route as an event stream: blocks push frames to the caller over
	// one long-lived connection instead of the flow returning a single response.
	Enabled bool `json:"enabled" octo:"label=Enabled,default=false"`
	// Variable the stream's id lands in. An sse-event block reads it to find the
	// connection, and a flow may deliberately pass it further — into a queue
	// payload, say — so work running elsewhere can write to the same stream.
	StreamVar string `json:"streamVar" octo:"label=Stream ID variable,default=sseStream"`
	// Emit the flow's response message as one last frame before closing.
	FinalEvent bool `json:"finalEvent" octo:"label=Send final event,default=false"`
	// The 'event:' name for that final frame.
	FinalEventName string `json:"finalEventName" octo:"label=Final event name"`
	// Close the stream once the flow's own invocation terminates. Turn it off when
	// the flow hands its work to a continuation or a queue and finishes early: the
	// detached invocations are the ones with something left to say. Pair it with
	// maxDuration, and with a timeout long enough to cover the wait for the first
	// frame.
	CloseOnComplete *bool `json:"closeOnComplete" octo:"label=Close on complete,default=true"`
	// How often an idle stream writes a keep-alive comment; unset uses 15s.
	Heartbeat duration `json:"heartbeat" octo:"label=Heartbeat,type=string,default=15s"`
	// Hard cap on how long one stream may stay open; unset is unlimited.
	MaxDuration duration `json:"maxDuration" octo:"label=Max duration,type=string"`
	// How many streams this route holds open at once; unset is unlimited.
	MaxStreams int `json:"maxStreams" octo:"label=Max streams"`
}

// sseConfig is the route's SSE settings resolved once at build time.
type sseConfig struct {
	enabled         bool
	streamVar       string
	finalEvent      bool
	finalEventName  string
	closeOnComplete bool
	heartbeat       time.Duration
	maxDuration     time.Duration
	maxStreams      int
}

// newSSEConfig resolves the settings, applying the defaults the editor only
// advertises. An unset or zero heartbeat means the default rather than "off":
// a stream nothing ever writes to is exactly the one a proxy culls.
func newSSEConfig(set sseSettings) sseConfig {
	cfg := sseConfig{
		enabled:        set.Enabled,
		streamVar:      strings.TrimSpace(set.StreamVar),
		finalEvent:     set.FinalEvent,
		finalEventName: set.FinalEventName,
		// A pointer, so an explicit false is distinguishable from unset.
		closeOnComplete: set.CloseOnComplete == nil || *set.CloseOnComplete,
		heartbeat:       time.Duration(set.Heartbeat),
		maxDuration:     time.Duration(set.MaxDuration),
		maxStreams:      set.MaxStreams,
	}
	if cfg.streamVar == "" {
		cfg.streamVar = defaultSSEStreamVar
	}
	if cfg.heartbeat <= 0 {
		cfg.heartbeat = defaultSSEHeartbeat
	}
	return cfg
}

// sseStream is one open server-sent-events connection.
//
// It is addressed by an id of its own rather than by the EventID of the message
// that opened it. EventID is rekeyed whenever a message crosses into another
// invocation — flow-ref, split, queue-dispatch all do it deliberately — so an id
// derived from it would stop resolving exactly where streaming from detached work
// is most useful. The id is a plain string in a message variable, so it survives
// every copy, rekey and serialization a message goes through, and a flow can pass
// it somewhere else on purpose.
//
// Every method takes mu, which is what makes the stream safe to write from a flow
// goroutine while the handler goroutine is parked elsewhere. Once closed is set
// the stream never writes again: the handler is then free to return, and
// returning from a handler is what makes any further use of the ResponseWriter
// undefined.
type sseStream struct {
	id  string
	src *source

	mu        sync.Mutex
	w         http.ResponseWriter
	rc        *http.ResponseController
	opened    bool
	closed    bool
	closeOnce sync.Once
	done      chan struct{}
}

// newSSEStream mints a stream over w. It does not touch the response: nothing is
// written until the first frame (see open).
func newSSEStream(src *source, w http.ResponseWriter) (*sseStream, error) {
	id, err := newSSEStreamID()
	if err != nil {
		return nil, err
	}
	return &sseStream{
		id:   id,
		src:  src,
		w:    w,
		rc:   http.NewResponseController(w),
		done: make(chan struct{}),
	}, nil
}

// newSSEStreamID mints a stream's identity within its connector.
func newSSEStreamID() (string, error) {
	buf := make([]byte, sseStreamIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate sse stream id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// streamAddress renders the value a route publishes in its stream variable:
// which connector holds the stream, and which stream. Naming the connector is
// what makes the address complete — a block handed one needs no configuration to
// act on it, wherever it runs and however the address reached it.
func streamAddress(connector, id string) string {
	return connector + streamAddressSep + id
}

// parseStreamAddress splits an address into its connector and stream id. An
// address without a connector is still valid — a hand-written id, say — and
// leaves the choice to the block's own connector setting. The id is hex, so the
// last separator is the boundary even if a connector name contains one.
func parseStreamAddress(address string) (connector, id string) {
	sep := strings.LastIndex(address, streamAddressSep)
	if sep < 0 {
		return "", address
	}
	return address[:sep], address[sep+1:]
}

// emit writes one frame, committing the response to a stream on the first call.
// msg supplies the response headers the route is configured to propagate, so a
// block may still shape them right up until that first frame.
func (s *sseStream) emit(msg *types.Message, f frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStreamClosed
	}
	s.open(msg)
	return s.writeLocked(func() error { return writeFrame(s.w, f) })
}

// ping writes a comment line to keep an idle connection alive. It never opens the
// stream: a heartbeat must not be what turns a request whose outcome is still
// undecided into a committed 200.
func (s *sseStream) ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errStreamClosed
	}
	if !s.opened {
		return nil
	}
	return s.writeLocked(func() error {
		if _, err := io.WriteString(s.w, sseComment); err != nil {
			return fmt.Errorf("write sse heartbeat: %w", err)
		}
		return nil
	})
}

// close makes the stream permanently inert and wakes whoever is parked on done.
// Taking mu is the point of it: close cannot return while another goroutine is
// mid-write, so once it has, nothing will touch the ResponseWriter again.
func (s *sseStream) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked()
}

// isOpen reports whether the response has been committed to a stream — that is,
// whether anything has been written yet. It is what decides between finishing as
// a stream and falling back to an ordinary buffered response.
func (s *sseStream) isOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opened
}

// open commits the response to being a stream. It runs on the first frame rather
// than at request start, which is what lets everything before that frame behave
// like an ordinary route: a filter block still rejects with a real 401,
// vars.httpStatus still chooses the code, and a flow that never emits still gets
// the buffered response writeResult would have written. The caller holds mu.
func (s *sseStream) open(msg *types.Message) {
	if s.opened {
		return
	}
	s.opened = true

	// The server's WriteTimeout applies to the whole response, so on a long-lived
	// stream it is a countdown to being cut off. Clear it; each write sets its own
	// bounded deadline instead (see writeLocked).
	_ = s.rc.SetWriteDeadline(time.Time{})

	if msg != nil {
		s.src.propagateResponseHeaders(s.w, msg)
	}
	header := s.w.Header()
	header.Set("Content-Type", sseContentType)
	// no-transform tells proxies not to buffer or recompress the stream, and
	// X-Accel-Buffering is the nginx-specific way of saying the same thing.
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("X-Accel-Buffering", "no")
	// A stream is always 200 — EventSource treats anything else as a failure — so
	// vars.httpStatus applies only on the non-stream path.
	s.w.WriteHeader(http.StatusOK)
}

// writeLocked performs one write against a deadline and flushes it, closing the
// stream if either fails: a broken connection makes every later frame pointless,
// and closing here is what wakes the parked handler. The caller holds mu and has
// already checked closed.
func (s *sseStream) writeLocked(write func() error) error {
	// Not every ResponseWriter supports deadlines (httptest's recorder does not).
	// Losing the bound is not a reason to refuse to stream.
	_ = s.rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))

	if err := write(); err != nil {
		s.closeLocked()
		return fmt.Errorf("write sse frame: %w", err)
	}
	if err := s.rc.Flush(); err != nil {
		s.closeLocked()
		return fmt.Errorf("flush sse frame: %w", err)
	}
	return nil
}

// closeLocked is close's body for callers already holding mu.
func (s *sseStream) closeLocked() {
	s.closed = true
	s.closeOnce.Do(func() { close(s.done) })
}

// writeFrame encodes one event in the text/event-stream wire format.
//
// Data is split into one "data:" line per line: a bare newline inside a value
// ends the field, so passing one through would silently truncate the event. A
// lone carriage return terminates a line too, so it is folded into a newline
// rather than left to split the frame somewhere the reader cannot see.
func writeFrame(w io.Writer, f frame) error {
	var b strings.Builder
	if f.event != "" {
		b.WriteString("event: ")
		b.WriteString(sanitizeSSEField(f.event))
		b.WriteByte('\n')
	}
	data := strings.ReplaceAll(f.data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")
	for _, line := range strings.Split(data, "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	// The blank line is what dispatches the event.
	b.WriteByte('\n')

	if _, err := io.WriteString(w, b.String()); err != nil {
		return fmt.Errorf("write sse frame: %w", err)
	}
	return nil
}

// sanitizeSSEField replaces the line terminators that would end a single-line
// field early, so an event name can never split a frame.
func sanitizeSSEField(value string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(value)
}

// messageData renders a message as frame data: the raw payload when the message
// carries one, otherwise its body as JSON. It is the default an sse-event uses
// when no data expression is configured, and what the route's final frame sends.
// An unrenderable body yields an empty frame rather than failing the write —
// there is nothing useful to say to a client mid-stream about a marshalling bug,
// and the flow's own error handling is the place that hears about it.
func messageData(msg *types.Message) string {
	if msg == nil || msg.Body == nil {
		return ""
	}
	if _, rawData, ok := msg.RawBody(); ok {
		return rawData
	}
	raw, err := msg.BodyJSON()
	if err != nil {
		return ""
	}
	return string(raw)
}

// trackStream registers st so a block can find it by id from any invocation.
func (c *Connector) trackStream(st *sseStream) {
	c.mu.Lock()
	c.streams[st.id] = st
	c.mu.Unlock()
}

// stream returns the open stream with the given id. It reports false for an id
// that never existed, one whose stream has ended, and one belonging to another
// replica — all of which are the same thing to a caller: there is nothing here to
// write to.
func (c *Connector) stream(id string) (*sseStream, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st, ok := c.streams[id]
	return st, ok
}

// forgetStream removes the registry entry; safe to call more than once.
func (c *Connector) forgetStream(id string) {
	c.mu.Lock()
	delete(c.streams, id)
	c.mu.Unlock()
}
