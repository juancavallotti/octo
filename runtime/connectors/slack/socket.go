// This file provides the slack connector's source: Socket Mode. Instead of Slack
// posting to a public route, the source opens a websocket to Slack with the
// connector's app-level token and receives events, slash commands and
// interactivity over it. Each envelope becomes a message whose body is the same
// payload an HTTP flow holds after slack-verify-request, so the blocks after the
// source are the same either way.
//
// Slack requires every envelope to be acknowledged within 3 seconds. An envelope
// that accepts a response payload (slash commands, most interactivity) is acked
// with the flow's final body, the way an HTTP flow's response body is the reply;
// any other is acked on receipt, so a slow flow never makes Slack retry it.
package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// socketSourceType is the source type a flow names to receive Socket Mode
	// traffic from a slack connector.
	socketSourceType = "slack"
	// envelopeTypeVar is the variable each message carries its envelope type in:
	// events_api, slash_commands or interactive.
	envelopeTypeVar = "slackEnvelopeType"
	// defaultAckTimeout leaves headroom under Slack's 3-second ack deadline for
	// the ack itself to travel.
	defaultAckTimeout = 2500 * time.Millisecond
	// minReconnectDelay and maxReconnectDelay bound the backoff between failed
	// connection attempts.
	minReconnectDelay = time.Second
	maxReconnectDelay = 30 * time.Second
)

// Envelope types Slack sends over a Socket Mode connection.
const (
	envelopeHello      = "hello"
	envelopeDisconnect = "disconnect"
)

// socketSettings is the slack source's typed configuration.
type socketSettings struct {
	// How long to wait for the flow's result before acking without a response
	// payload. Slack's deadline is 3s.
	AckTimeout duration `json:"ackTimeout" octo:"label=Ack timeout,type=string,default=2500ms"`
}

// envelope is one frame Slack sends over the socket.
type envelope struct {
	Type                   string          `json:"type"`
	EnvelopeID             string          `json:"envelope_id"`
	Payload                json.RawMessage `json:"payload"`
	AcceptsResponsePayload bool            `json:"accepts_response_payload"`
	Reason                 string          `json:"reason"`
}

// ack is the frame that acknowledges an envelope, optionally carrying the
// response Slack shows for it.
type ack struct {
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// socketConn is one websocket to Slack. gorilla/websocket allows a single
// concurrent writer, and acks are written from the envelope handlers.
type socketConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *socketConn) ack(envelopeID string, payload json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ws.WriteJSON(ack{EnvelopeID: envelopeID, Payload: payload}); err != nil {
		slog.Warn("slack socket: ack failed", "envelope_id", envelopeID, "error", err)
	}
}

// socketSource holds a Socket Mode connection open for one flow, reconnecting
// whenever Slack asks it to or the connection drops.
type socketSource struct {
	conn       *Connector
	out        chan<- *types.Message
	ackTimeout time.Duration
	dialer     *websocket.Dialer

	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	unsubscribe func()

	mu      sync.Mutex
	current *socketConn
	pending map[string]chan types.FlowEvent
}

// NewSource builds the slack source. It needs the connector's app token, which
// is only required of connectors that receive Socket Mode traffic.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(
	cfg types.SourceConfig, out chan<- *types.Message, _ core.SourceDeps,
) (core.MessageSource, error) {
	if cfg.Type != socketSourceType {
		return nil, fmt.Errorf("slack connector has no %q source", cfg.Type)
	}
	var set socketSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	if c.appToken == "" {
		return nil, errors.New("slack source requires the slack connector's appToken")
	}
	ackTimeout := time.Duration(set.AckTimeout)
	if ackTimeout <= 0 {
		ackTimeout = defaultAckTimeout
	}
	return &socketSource{
		conn:       c,
		out:        out,
		ackTimeout: ackTimeout,
		dialer:     websocket.DefaultDialer,
		pending:    make(map[string]chan types.FlowEvent),
	}, nil
}

// Start subscribes to flow results and starts the connection loop on its own
// goroutine.
func (s *socketSource) Start(context.Context) error {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.unsubscribe = core.DefaultEventBus().Subscribe(s.onFlowEvent)
	s.wg.Go(s.run)
	return nil
}

// Stop cancels the loop, closes the open connection to unblock its read, and
// waits for every envelope handler, so nothing is sent on out after it returns.
func (s *socketSource) Stop(context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	s.mu.Lock()
	if s.current != nil {
		_ = s.current.ws.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	s.unsubscribe()
	return nil
}

// run connects, serves the connection until it ends, and reconnects. The delay
// grows while attempts fail and resets once Slack has said hello.
func (s *socketSource) run() {
	delay := minReconnectDelay
	for s.ctx.Err() == nil {
		greeted, err := s.connectAndServe()
		if s.ctx.Err() != nil {
			return
		}
		if greeted {
			delay = minReconnectDelay
		}
		if err != nil {
			slog.Warn("slack socket: connection ended", "error", err, "retry_in", delay)
			if !s.sleep(delay) {
				return
			}
			delay = min(delay*2, maxReconnectDelay)
		}
	}
}

// sleep waits for d, reporting false when the source stops first.
func (s *socketSource) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// connectAndServe opens one connection and reads envelopes until Slack asks to
// disconnect (a nil error, so run reconnects at once) or the read fails.
// greeted reports whether Slack's hello arrived.
func (s *socketSource) connectAndServe() (greeted bool, err error) {
	url, err := s.openURL()
	if err != nil {
		return false, err
	}
	ws, resp, err := s.dialer.DialContext(s.ctx, url, nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	c := &socketConn{ws: ws}
	s.setCurrent(c)
	defer func() {
		s.setCurrent(nil)
		_ = ws.Close()
	}()

	for {
		var env envelope
		if err := ws.ReadJSON(&env); err != nil {
			return greeted, fmt.Errorf("read: %w", err)
		}
		switch env.Type {
		case envelopeHello:
			greeted = true
		case envelopeDisconnect:
			slog.Info("slack socket: disconnect requested", "reason", env.Reason)
			return greeted, nil
		default:
			if env.EnvelopeID == "" {
				continue
			}
			s.wg.Go(func() { s.handle(c, env) })
		}
	}
}

// openURL asks Slack for a fresh websocket URL; each URL is single-use.
func (s *socketSource) openURL() (string, error) {
	resp, err := s.conn.call(s.ctx, s.conn.appToken, "apps.connections.open", map[string]any{})
	if err != nil {
		return "", err
	}
	url, _ := resp["url"].(string)
	if url == "" {
		return "", errors.New("slack apps.connections.open: no url in response")
	}
	return url, nil
}

func (s *socketSource) setCurrent(c *socketConn) {
	s.mu.Lock()
	s.current = c
	s.mu.Unlock()
}

// handle runs one envelope through the flow and acks it on the connection it
// arrived on.
func (s *socketSource) handle(c *socketConn, env envelope) {
	msg, err := types.NewMessage("")
	if err != nil {
		slog.Error("slack socket: build message", "error", err)
		c.ack(env.EnvelopeID, nil)
		return
	}
	if err := msg.SetBodyJSON(env.Payload); err != nil {
		slog.Warn("slack socket: undecodable payload", "type", env.Type, "error", err)
		c.ack(env.EnvelopeID, nil)
		return
	}
	msg.Variables.Set(envelopeTypeVar, env.Type)

	if !env.AcceptsResponsePayload {
		c.ack(env.EnvelopeID, nil)
		s.send(msg)
		return
	}

	ch := s.track(msg.EventID)
	defer s.forget(msg.EventID)
	if !s.send(msg) {
		return
	}
	c.ack(env.EnvelopeID, s.awaitResponse(ch))
}

// send delivers msg onto the flow channel unless the source stops first.
func (s *socketSource) send(msg *types.Message) bool {
	select {
	case s.out <- msg:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// awaitResponse waits up to the ack timeout for the flow and returns its final
// body as the ack's response payload. A flow that fails, is dropped, runs out of
// time or ends on a non-JSON body gets a plain ack.
func (s *socketSource) awaitResponse(ch chan types.FlowEvent) json.RawMessage {
	timer := time.NewTimer(s.ackTimeout)
	defer timer.Stop()
	select {
	case ev := <-ch:
		if ev.Kind != types.FlowEventCompleted || ev.Result == nil || ev.Result.RawContent || ev.Result.Body == nil {
			return nil
		}
		raw, err := ev.Result.BodyJSON()
		if err != nil {
			return nil
		}
		return raw
	case <-timer.C:
		return nil
	case <-s.ctx.Done():
		return nil
	}
}

// track registers a buffered result channel under eventID; the buffer lets
// onFlowEvent deliver without blocking the flow worker.
func (s *socketSource) track(eventID string) chan types.FlowEvent {
	ch := make(chan types.FlowEvent, 1)
	s.mu.Lock()
	s.pending[eventID] = ch
	s.mu.Unlock()
	return ch
}

func (s *socketSource) forget(eventID string) {
	s.mu.Lock()
	delete(s.pending, eventID)
	s.mu.Unlock()
}

// onFlowEvent hands a terminal flow event to the envelope waiting on it. It runs
// on the flow worker, so it never blocks.
func (s *socketSource) onFlowEvent(ev types.FlowEvent) {
	if ev.Kind == types.FlowEventStarted {
		return
	}
	s.mu.Lock()
	ch, ok := s.pending[ev.EventID]
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}
