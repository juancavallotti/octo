// The observer path: a sub-flow a block runs once per event it reports, and the
// filter deciding which events reach it.
//
// It began as the ai-agent's, and everything here is still shaped by that: an
// agent on a token stream emits thousands of events per run, so an excluded kind
// has to cost a map lookup and nothing else. It is generic now because the shape
// is not agent-specific — any block that produces a series of things worth
// watching, while still owing its caller one answer at the end, wants exactly
// this. A block passes its own label and its own vocabulary; the machinery is
// the same.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/runtime/types"
)

// emitter runs a block's events sub-flow.
//
// A nil emitter is inert, so a block reports unconditionally and one with no
// events path pays nothing for the calls.
type emitter struct {
	flow *Flow
	// kinds is the emit allow-list, or nil to emit every kind.
	kinds map[string]bool
	// label is the block type, for the log lines below. The events path reports
	// rather than participates, so its failures are logged and not propagated —
	// which makes naming the block that owned it the only way to find it again.
	label string
}

// wants reports whether a kind reaches the sub-flow at all.
//
// It is checked before an event is built rather than after, so an excluded kind
// costs one map lookup — no fields, no message, no invocation. That is what makes
// emit a real lever on a token stream (or a chatty command's output) instead of
// just a filter with extra steps.
func (e *emitter) wants(kind string) bool {
	if e == nil {
		return false
	}
	return e.kinds == nil || e.kinds[kind]
}

// send runs the sub-flow for one event and reports whether the run should stop.
func (e *emitter) send(ctx context.Context, parent *types.Message, kind string, fields map[string]any) bool {
	if !e.wants(kind) {
		return false
	}
	msg, err := e.message(parent, kind, fields)
	if err != nil {
		slog.Debug("could not build an event", "block", e.label, "kind", kind, "error", err)
		return false
	}
	out, err := e.flow.Process(ctx, msg)
	if err != nil {
		// The path reports on the run; it does not get to fail it. A broken sink is
		// worth a log line, not worth discarding the work the agent has already done.
		slog.Debug("events path failed", "block", e.label, "kind", kind, "error", err)
		return false
	}
	// A stop is the one thing that travels back. It means nobody is listening any
	// more — an sse-event whose caller hung up is the ordinary cause — and carrying
	// on would spend a whole run reporting to no one.
	return out != nil && out.StopRequested()
}

// message builds the event's own message: a fresh one carrying the parent's
// variables, with the event as its body.
//
// It is deliberately not the parent message. An ai-agent's tool branches run on
// that one and overwrite its body with each call's arguments, so handing it over
// would both corrupt the loop and hand the sub-flow a body that is not the event.
// It is deliberately not a Clone either: Clone deep-copies the body, a cost that
// would be paid once per fragment on a token stream.
//
// Carrying the variables over is what makes the path useful with no configuration
// at all — vars.sseStream travels with them, so an sse-event in the path writes to
// the caller that started the run.
func (e *emitter) message(parent *types.Message, kind string, fields map[string]any) (*types.Message, error) {
	msg, err := types.NewMessage(parent.CorrelationID)
	if err != nil {
		return nil, err
	}
	for name, v := range parent.Variables {
		msg.Variables.Set(name, v)
	}
	body := make(map[string]any, len(fields)+1)
	body["type"] = kind
	for name, v := range fields {
		body[name] = v
	}
	msg.SetBody(body)
	return msg, nil
}

// emitKinds validates an emit list against the block's own vocabulary, returning
// nil for an empty list so every kind passes.
//
// A name outside the vocabulary is an error rather than a filter that matches
// nothing: an emit list is how an author says what they want to watch, and a
// typo in it would otherwise present as silence.
func emitKinds(label string, known map[string]bool, emit []string) (map[string]bool, error) {
	if len(emit) == 0 {
		return nil, nil
	}
	kinds := make(map[string]bool, len(emit))
	for _, kind := range emit {
		if !known[kind] {
			return nil, fmt.Errorf(
				"%s emit %q is not an event type (one of: %s)", label, kind, knownEventKinds(known))
		}
		kinds[kind] = true
	}
	return kinds, nil
}

// knownEventKinds lists a vocabulary for an error message, sorted so the same
// bad config always reports the same way.
func knownEventKinds(known map[string]bool) string {
	names := make([]string, 0, len(known))
	for kind := range known {
		names = append(names, kind)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
