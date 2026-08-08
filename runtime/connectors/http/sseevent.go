// This file provides the "sse-event" block: it writes one server-sent event to an
// open stream. The stream is addressed by an id the route minted and left in a
// message variable, so the block reaches the caller's connection from wherever it
// runs — the flow that took the request, a sub-flow, or a job the flow queued.
package http

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// The policies for a frame written to a stream that is no longer there.
const (
	// ifClosedStop ends the flow. The default: a caller hanging up is ordinary,
	// and carrying on producing output nobody will read is the waste worth
	// avoiding.
	ifClosedStop = "stop"
	// ifClosedIgnore carries on, for a flow whose remaining work matters even
	// though nobody is listening.
	ifClosedIgnore = "ignore"
	// ifClosedError fails the block, routing to the flow's error handling.
	ifClosedError = "error"
)

func registerSSEEvent() {
	core.MustRegisterBlock("sse-event", newSSEEvent)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "sse-event",
		Label:    "SSE Event",
		Category: core.CategoryProcessor,
		// The http package registers no package-level extension metadata, so both
		// of these are stated rather than inherited.
		Group: "Integration",
		Icon:  "Radio",
		Description: "Write one server-sent event to the caller's open stream. Requires a route " +
			"with server-sent events enabled; optionally closes the stream and stops the flow.",
		Config: reflect.TypeFor[sseEventSettings](),
	})
}

// sseEventSettings is the sse-event block's typed configuration.
type sseEventSettings struct {
	// The 'event:' name clients dispatch on. Leave empty for an unnamed event,
	// which browsers deliver to EventSource.onmessage.
	Event string `json:"event" octo:"label=Event name"`
	// CEL expression for the frame's data. Defaults to the message body as JSON.
	Data string `json:"data" octo:"label=Data,type=cel"`
	// CEL expression for the stream to write to. Defaults to vars.sseStream, the
	// variable an SSE route stamps its stream address into — set this when the
	// route renames that variable, or when the address reached this flow another
	// way, such as in the payload of a queued job.
	Stream string `json:"stream" octo:"label=Stream,type=cel"`
	// Which http connector owns the stream. Rarely needed: an address published by
	// a route already names its connector. It applies only to a bare stream id.
	Connector string `json:"connector" octo:"label=Connector,ref=connector:http"`
	// End the stream after this event. The client sees the connection close.
	Close bool `json:"close" octo:"label=Close stream,default=false"`
	// Stop the flow after this event, so no later block runs. Usually paired with
	// Close; on its own it ends the flow while leaving the stream open for work
	// running elsewhere (see the route's Close on complete setting).
	Stop bool `json:"stop" octo:"label=Stop flow,default=false"`
	// What to do when the stream has already ended — the client disconnected, a
	// block closed it, or it belongs to another replica.
	IfClosed string `json:"ifClosed" octo:"label=If closed,type=enum,enum=stop|ignore|error,default=stop"`
}

// sseEvent writes one frame to an open stream.
type sseEvent struct {
	connectors func(name string) (core.Connector, bool)
	connector  string
	event      string
	data       *expr.Program
	stream     *expr.Program
	env        map[string]any
	closeAfter bool
	stopAfter  bool
	ifClosed   string
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSSEEvent(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg sseEventSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := validateIfClosed(cfg.IfClosed); err != nil {
		return nil, err
	}

	data, err := compileOptional(deps, cfg.Data, "data")
	if err != nil {
		return nil, err
	}
	stream, err := compileOptional(deps, cfg.Stream, "stream")
	if err != nil {
		return nil, err
	}

	connector := strings.TrimSpace(cfg.Connector)
	if connector == "" {
		connector = connectorType
	}
	return &sseEvent{
		connectors: deps.Connector,
		connector:  connector,
		event:      cfg.Event,
		data:       data,
		stream:     stream,
		env:        expr.EnvActivation(deps.Env),
		closeAfter: cfg.Close,
		stopAfter:  cfg.Stop,
		ifClosed:   orDefault(cfg.IfClosed, ifClosedStop),
	}, nil
}

// validateIfClosed rejects an unknown policy at build time rather than letting it
// silently behave like the default.
func validateIfClosed(value string) error {
	switch value {
	case "", ifClosedStop, ifClosedIgnore, ifClosedError:
		return nil
	default:
		return fmt.Errorf("sse-event: unknown ifClosed %q (want stop, ignore or error)", value)
	}
}

// compileOptional compiles an expression that may be absent, naming the field in
// any failure so a broken expression is diagnosable at startup.
func compileOptional(deps core.BlockDeps, expression, field string) (*expr.Program, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, nil //nolint:nilnil // absent is a valid configuration, not a failure
	}
	program, err := expr.CompileMessage(deps.Resources, expression)
	if err != nil {
		return nil, fmt.Errorf("sse-event: compile %s: %w", field, err)
	}
	return program, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// Process writes the frame. Reaching the stream raises two questions with
// deliberately different answers: whether the block is wired up at all — no stream
// id, no such connector — which is a configuration mistake and always fails, and
// whether the connection is still there, which is an ordinary runtime condition
// the ifClosed policy decides.
func (e *sseEvent) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	address, err := e.streamAddress(msg)
	if err != nil {
		return nil, err
	}
	name, id := parseStreamAddress(address)
	if name == "" {
		name = e.connector
	}
	conn, err := e.httpConnector(name)
	if err != nil {
		return nil, err
	}

	st, ok := conn.stream(id)
	if !ok {
		return e.closed(msg, fmt.Errorf("sse-event: %w (stream %q)", errStreamClosed, id))
	}
	if err := e.write(st, msg); err != nil {
		return e.closed(msg, err)
	}

	if e.closeAfter {
		st.close()
	}
	if e.stopAfter {
		msg.RequestStop()
	}
	return msg, nil
}

// write emits the frame, unless the block is configured purely to close: a block
// with neither a name nor data has nothing to say, and an empty frame would be
// noise on the wire.
func (e *sseEvent) write(st *sseStream, msg *types.Message) error {
	data, err := e.frameData(msg)
	if err != nil {
		return err
	}
	if e.event == "" && data == "" {
		return nil
	}
	return st.emit(msg, frame{event: e.event, data: data})
}

// frameData renders the frame's payload: the configured expression, or the
// message body as JSON when there is none.
func (e *sseEvent) frameData(msg *types.Message) (string, error) {
	if e.data == nil {
		return messageData(msg), nil
	}
	data, err := e.data.EvalString(expr.MessageActivation(msg, e.env))
	if err != nil {
		return "", fmt.Errorf("sse-event data: %w", err)
	}
	return data, nil
}

// streamAddress resolves which stream to write to.
func (e *sseEvent) streamAddress(msg *types.Message) (string, error) {
	address := ""
	if e.stream == nil {
		address, _ = msg.Variables.String(defaultSSEStreamVar)
	} else {
		evaluated, err := e.stream.EvalString(expr.MessageActivation(msg, e.env))
		if err != nil {
			return "", fmt.Errorf("sse-event stream: %w", err)
		}
		address = evaluated
	}
	if address = strings.TrimSpace(address); address == "" {
		return "", errors.New(
			"sse-event found no stream id: enable server-sent events on the flow's http source, " +
				"or point the block's \"stream\" expression at one")
	}
	return address, nil
}

// httpConnector resolves the connector holding the stream registry.
//
// The lookup happens per message rather than once at build time: a flow reached
// through flow-ref or fed by a queue may be built before the flow whose source
// starts the connector, so a build-time lookup would miss for reasons that have
// nothing to do with the configuration. It is a mutex-guarded map read, which
// costs nothing next to the write it precedes.
func (e *sseEvent) httpConnector(name string) (*Connector, error) {
	if e.connectors == nil {
		return nil, fmt.Errorf("sse-event: connector %q requested but no connectors are available", name)
	}
	connector, ok := e.connectors(name)
	if !ok {
		return nil, fmt.Errorf(
			"sse-event: http connector %q is not configured; set the block's \"connector\" to the "+
				"one that owns the stream", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("sse-event: connector %q is not an http connector", name)
	}
	return conn, nil
}

// closed applies the ifClosed policy: the stream this block was pointed at is no
// longer there to write to. A configuration fault never arrives here — those fail
// outright, because "nobody is listening" and "this block is wired up wrong"
// deserve different outcomes.
func (e *sseEvent) closed(msg *types.Message, err error) (*types.Message, error) {
	switch e.ifClosed {
	case ifClosedIgnore:
		return msg, nil
	case ifClosedError:
		return nil, err
	default: // ifClosedStop
		msg.RequestStop()
		return msg, nil
	}
}
