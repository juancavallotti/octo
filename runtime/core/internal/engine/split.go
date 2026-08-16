// The "split" block: one message in, many out. It is half of what `foreach
// mode: map` fuses together, and separating the halves is what makes the shapes
// foreach cannot express possible — streaming a large payload, per-element error
// isolation, and re-joining on a condition rather than positionally.
//
// Splitting takes the flow asynchronous: each element is scheduled through the
// rest of the chain as an invocation of its own, so one bad record fails alone
// and the caller gets its answer from the buildResponse slot without waiting.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// How a split carves its input into elements.
const (
	// splitModeExpression evaluates a CEL expression to a collection. The baseline,
	// and the same input foreach takes.
	splitModeExpression = "expression"
	// splitModeLines, splitModeDelimiter and splitModeChunk tokenize a text body.
	// They are the path that makes a large payload tractable: elements are produced
	// and dispatched one at a time rather than materialized as a collection first.
	splitModeLines     = "lines"
	splitModeDelimiter = "delimiter"
	splitModeChunk     = "chunk"
)

// What a split does when an element cannot be handed to the continuation.
const (
	splitOnErrorSkip  = "skip"
	splitOnErrorAbort = "abort"
)

// sizeUnknown is the group size carried by an element whose split cannot yet
// know the total. Only a streaming mode produces it, and only until the final
// element, which always carries the real count.
const sizeUnknown = -1

// nextElement yields the next element of a split. ok is false once the input is
// exhausted. Elements are pulled one at a time so a tokenizing split holds one
// element rather than the whole collection.
type nextElement func() (value any, ok bool, err error)

// split fans a message out across the rest of its flow, one invocation per
// element.
type split struct {
	mode      string
	items     *expr.Program
	delimiter string
	chunkSize int
	abort     bool

	buildResponse *Flow
	env           map[string]any

	// cont is the rest of the flow, handed over at build time.
	cont core.Continuation
}

func (s *split) SetContinuation(c core.Continuation) { s.cont = c }

// splitBlock builds a split from its config.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) splitBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if err := allowSlots(cfg, blockKindSplit,
		"items", "mode", "delimiter", "chunkSize", "onError", "buildResponse"); err != nil {
		return nil, err
	}
	if err := requireRootChain(b, blockKindSplit); err != nil {
		return nil, err
	}

	mode := cfg.Mode
	if mode == "" {
		mode = splitModeExpression
	}

	s := &split{
		mode:      mode,
		delimiter: cfg.Delimiter,
		chunkSize: cfg.ChunkSize,
		env:       expr.EnvActivation(b.deps.Env),
	}

	if err := s.resolveMode(b, cfg); err != nil {
		return nil, err
	}

	switch cfg.OnError {
	case "", splitOnErrorSkip:
	case splitOnErrorAbort:
		s.abort = true
	default:
		return nil, fmt.Errorf(
			"split block onError %q must be %q or %q", cfg.OnError, splitOnErrorSkip, splitOnErrorAbort)
	}

	response, err := buildTakeoverResponse(b, cfg.BuildResponse)
	if err != nil {
		return nil, err
	}
	s.buildResponse = response
	return s, nil
}

// resolveMode validates the settings the chosen mode needs, and compiles the
// items expression when there is one. Each mode reads exactly one setting, so a
// mode that is missing its own is a config error rather than a silent default.
func (s *split) resolveMode(b *builder, cfg types.BlockConfig) error {
	switch s.mode {
	case splitModeExpression:
		if cfg.Items == "" {
			return errors.New("split block in expression mode requires an items expression")
		}
		items, err := expr.CompileMessage(b.deps.Resources, cfg.Items)
		if err != nil {
			return fmt.Errorf("split items: %w", err)
		}
		s.items = items
	case splitModeDelimiter:
		if cfg.Delimiter == "" {
			return errors.New("split block in delimiter mode requires a delimiter")
		}
	case splitModeChunk:
		if cfg.ChunkSize <= 0 {
			return errors.New("split block in chunk mode requires a positive chunkSize")
		}
	case splitModeLines:
	default:
		return fmt.Errorf(
			"split block mode %q must be one of %q, %q, %q, %q",
			s.mode, splitModeExpression, splitModeLines, splitModeDelimiter, splitModeChunk)
	}
	return nil
}

// Process carves the message into elements, hands each to the continuation, and
// then answers the caller. The message it returns stops the chain: the blocks
// after this one belong to the continuation now, and running them here too would
// process every element's work twice.
func (s *split) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	next, knownSize, err := s.elements(msg)
	if err != nil {
		return nil, err
	}

	// The group is identified by the message being split. It is already unique per
	// invocation and every element derives from it, so nothing has to be minted or
	// agreed — and because each element is rekeyed for its own invocation, this is
	// what preserves the grouping the rekey would otherwise destroy.
	groupID := msg.EventID

	total, err := s.fanOut(ctx, msg, groupID, next, knownSize)
	if err != nil {
		return nil, err
	}

	// The response sees the real total even in a streaming mode, because by now
	// every element has been dispatched and counted. That is what lets a
	// buildResponse report "accepted: 50000" honestly rather than -1.
	msg.Variables.Set(varGroupID, groupID)
	msg.Variables.Set(varGroupSize, total)
	return applyTakeover(ctx, msg, s.buildResponse)
}

// fanOut pulls elements and dispatches them, returning how many were produced.
//
// It keeps one element back so the last one can be recognized. That lookahead is
// the whole reason a streaming split can still tell its aggregator how big the
// group is: the final element's index is the count.
func (s *split) fanOut(
	ctx context.Context, msg *types.Message, groupID string, next nextElement, knownSize int,
) (int, error) {
	var (
		pending     any
		havePending bool
		index       int
	)
	for {
		value, ok, err := next()
		if err != nil {
			return index, err
		}
		if !ok {
			break
		}
		if havePending {
			if err := s.emit(ctx, msg, groupID, index, knownSize, pending); err != nil {
				return index, err
			}
			index++
		}
		pending, havePending = value, true
	}
	if havePending {
		size := knownSize
		if size == sizeUnknown {
			size = index + 1
		}
		if err := s.emit(ctx, msg, groupID, index, size, pending); err != nil {
			return index, err
		}
		index++
	}
	return index, nil
}

// emit builds one element message and schedules it through the rest of the flow.
func (s *split) emit(
	ctx context.Context, parent *types.Message, groupID string, index, size int, value any,
) error {
	elem, err := elementMessage(parent, value)
	if err != nil {
		return err
	}
	elem.Variables.Set(varGroupID, groupID)
	elem.Variables.Set(varGroupIndex, index)
	elem.Variables.Set(varGroupSize, size)

	// Resume parks when the flow's pool is saturated rather than failing, so a
	// split of 50,000 runs at the rate the flow can absorb and peak memory stays
	// flat. Only a genuine dispatch failure — a shutdown, a cancelled context —
	// reaches the policy below.
	if err := s.cont.Resume(ctx, elem); err != nil {
		if s.abort {
			return fmt.Errorf("split element %d: %w", index, err)
		}
		// The announced group size still counts this element, so a downstream
		// aggregator will wait for something that is never coming and complete on
		// its timeout instead. Saying so here is the difference between a
		// diagnosable delay and a mystery.
		slog.Warn("split element dropped; a downstream aggregate will complete on timeout",
			"index", index, "group", groupID, "error", err)
	}
	return nil
}

// elementMessage builds the message carrying one element. It deliberately does
// not clone the parent: the parent's body is the whole collection, and copying
// it per element is what makes the fused foreach quadratic. Instead it copies
// only the element, which keeps the total work linear in the input.
//
// The copy itself is not optional. The element is a value out of the parent's
// body, it outlives this goroutine, and the parent's body is still readable from
// the buildResponse slot — so sharing it would be a data race between a block
// mutating an element and the response serializing the collection.
func elementMessage(parent *types.Message, value any) (*types.Message, error) {
	elem, err := types.NewMessage(parent.CorrelationID)
	if err != nil {
		return nil, err
	}
	elem.SetBody(value)
	elem = elem.Clone()

	for name, v := range parent.Variables {
		elem.Variables.Set(name, v)
	}
	return elem, nil
}

// elements returns the element source for the configured mode, along with the
// group size when the mode knows it up front (sizeUnknown otherwise).
func (s *split) elements(msg *types.Message) (nextElement, int, error) {
	if s.mode == splitModeExpression {
		return s.expressionElements(msg)
	}
	text, err := textBody(msg)
	if err != nil {
		return nil, 0, err
	}
	switch s.mode {
	case splitModeLines:
		return lineElements(text), sizeUnknown, nil
	case splitModeDelimiter:
		return delimiterElements(text, s.delimiter), sizeUnknown, nil
	default:
		return chunkElements(text, s.chunkSize), sizeUnknown, nil
	}
}

// expressionElements evaluates the items expression to a collection. The size is
// known up front here, so every element can carry it.
func (s *split) expressionElements(msg *types.Message) (nextElement, int, error) {
	value, err := s.items.Eval(expr.MessageActivation(msg, s.env))
	if err != nil {
		return nil, 0, fmt.Errorf("split items: %w", err)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, 0, fmt.Errorf("split items expression must evaluate to an array, got %T", value)
	}
	i := 0
	return func() (any, bool, error) {
		if i >= len(items) {
			return nil, false, nil
		}
		item := items[i]
		i++
		return item, true, nil
	}, len(items), nil
}

// textBody returns the message's payload as text: the raw data of a raw-content
// message, or a plain string body. The tokenizing modes are for payloads that
// are text rather than decoded JSON, so anything else is a config mistake.
func textBody(msg *types.Message) (string, error) {
	if _, raw, ok := msg.RawBody(); ok {
		return raw, nil
	}
	if text, ok := msg.Body.(string); ok {
		return text, nil
	}
	return "", fmt.Errorf(
		"split in a tokenizing mode requires a text body (raw content or a string), got %T", msg.Body)
}

// lineElements yields one line at a time.
func lineElements(text string) nextElement {
	rest := text
	return func() (any, bool, error) {
		if rest == "" {
			return nil, false, nil
		}
		line, remainder, found := strings.Cut(rest, "\n")
		if found {
			rest = remainder
		} else {
			rest = ""
		}
		return strings.TrimSuffix(line, "\r"), true, nil
	}
}

// delimiterElements yields the text between separators. An empty input yields
// nothing rather than one empty element, since splitting nothing into one blank
// message is never what was meant.
func delimiterElements(text, sep string) nextElement {
	rest := text
	done := text == ""
	return func() (any, bool, error) {
		if done {
			return nil, false, nil
		}
		part, remainder, found := strings.Cut(rest, sep)
		if found {
			rest = remainder
		} else {
			done = true
		}
		return part, true, nil
	}
}

// chunkElements yields fixed-size runs of runes. It walks the text rather than
// converting it to a rune slice, so a large payload still costs one element at a
// time.
func chunkElements(text string, size int) nextElement {
	pos := 0
	return func() (any, bool, error) {
		if pos >= len(text) {
			return nil, false, nil
		}
		start := pos
		for n := 0; n < size && pos < len(text); n++ {
			_, width := utf8.DecodeRuneInString(text[pos:])
			pos += width
		}
		return text[start:pos], true, nil
	}
}
