package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// recordingContinuation stands in for the rest of the flow, keeping what was
// resumed so a test can assert on the elements a split produced.
type recordingContinuation struct {
	mu   sync.Mutex
	msgs []*types.Message
	err  error
}

func (r *recordingContinuation) Resume(_ context.Context, msg *types.Message) error {
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return nil
}

func (r *recordingContinuation) resumed() []*types.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*types.Message(nil), r.msgs...)
}

// buildSplit builds a split block and wires it to a recording continuation, the
// way the flow builder wires it to the rest of a root chain.
func buildSplit(t *testing.T, cfg types.BlockConfig) (*split, *recordingContinuation) {
	t.Helper()
	cfg.Type = blockKindSplit
	proc, err := rootBuilder(testRegistry()).splitBlock(cfg)
	if err != nil {
		t.Fatalf("build split: %v", err)
	}
	block, ok := proc.(*split)
	if !ok {
		t.Fatalf("expected *split, got %T", proc)
	}
	cont := &recordingContinuation{}
	block.SetContinuation(cont)
	return block, cont
}

// groupVarsOf reads the three correlation variables off an element.
func groupVarsOf(t *testing.T, msg *types.Message) (id string, index, size int) {
	t.Helper()
	id, _ = msg.Variables.String(varGroupID)
	index, _ = msg.Variables.Int(varGroupIndex)
	size, _ = msg.Variables.Int(varGroupSize)
	return id, index, size
}

func TestSplitExpressionModeFansOutWithGroupVars(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Items: "body.lines"})

	msg := mustMessage(t)
	msg.Body = map[string]any{"lines": []any{"a", "b", "c"}}
	parentID := msg.EventID

	out, err := block.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !out.StopRequested() {
		t.Fatal("split should stop its own chain: the blocks after it belong to the continuation")
	}

	elements := cont.resumed()
	if len(elements) != 3 {
		t.Fatalf("resumed %d elements, want 3", len(elements))
	}
	for i, elem := range elements {
		id, index, size := groupVarsOf(t, elem)
		if id != parentID {
			t.Errorf("element %d groupId = %q, want the parent EventID %q", i, id, parentID)
		}
		if index != i {
			t.Errorf("element %d groupIndex = %d", i, index)
		}
		// The size is known up front here, so every element carries it — which is
		// what lets the aggregator complete on a count regardless of arrival order.
		if size != 3 {
			t.Errorf("element %d groupSize = %d, want 3", i, size)
		}
		if elem.EventID == parentID {
			t.Errorf("element %d kept the parent EventID; each element is its own invocation", i)
		}
	}
	bodies := make([]string, len(elements))
	for i, elem := range elements {
		bodies[i], _ = elem.Body.(string)
	}
	if got := strings.Join(bodies, ","); got != "a,b,c" {
		t.Errorf("element bodies = %q, want the three items", got)
	}
}

func TestSplitStopFlagDoesNotLeakIntoElements(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Items: "body.lines"})

	msg := mustMessage(t)
	msg.Body = map[string]any{"lines": []any{"a", "b"}}
	if _, err := block.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// The stop flag rides in Variables and elements copy the parent's variables,
	// so dispatching after requesting stop would silently halt every element at
	// the first block of the tail.
	for i, elem := range cont.resumed() {
		if elem.StopRequested() {
			t.Fatalf("element %d carries the parent's stop flag", i)
		}
	}
}

func TestSplitElementsDoNotAliasTheParentBody(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Items: "body.rows"})

	row := map[string]any{"n": float64(1)}
	msg := mustMessage(t)
	msg.Body = map[string]any{"rows": []any{row}}
	if _, err := block.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// An element outlives this goroutine and the parent's body stays readable from
	// the buildResponse slot, so sharing would be a data race between a downstream
	// block mutating an element and the response serializing the collection.
	elem := cont.resumed()[0]
	body, _ := elem.Body.(map[string]any)
	body["n"] = float64(99)
	if row["n"] != float64(1) {
		t.Fatal("mutating an element changed the parent's body: the element was not copied")
	}
}

func TestSplitTokenizingModes(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.BlockConfig
		body string
		want []string
	}{
		{
			name: "lines",
			cfg:  types.BlockConfig{Mode: splitModeLines},
			body: "one\ntwo\nthree",
			want: []string{"one", "two", "three"},
		},
		{
			name: "lines with carriage returns",
			cfg:  types.BlockConfig{Mode: splitModeLines},
			body: "one\r\ntwo\r\n",
			want: []string{"one", "two"},
		},
		{
			name: "delimiter",
			cfg:  types.BlockConfig{Mode: splitModeDelimiter, Delimiter: ","},
			body: "a,b,c",
			want: []string{"a", "b", "c"},
		},
		{
			name: "chunk",
			cfg:  types.BlockConfig{Mode: splitModeChunk, ChunkSize: 2},
			body: "abcde",
			want: []string{"ab", "cd", "e"},
		},
		{
			name: "chunk counts runes not bytes",
			cfg:  types.BlockConfig{Mode: splitModeChunk, ChunkSize: 2},
			body: "héllo",
			want: []string{"hé", "ll", "o"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, cont := buildSplit(t, tt.cfg)
			msg := mustMessage(t)
			msg.Body = tt.body
			if _, err := block.Process(context.Background(), msg); err != nil {
				t.Fatalf("Process: %v", err)
			}

			elements := cont.resumed()
			got := make([]string, len(elements))
			for i, elem := range elements {
				got[i], _ = elem.Body.(string)
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("elements = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitStreamingRevealsSizeOnTheFinalElement(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Mode: splitModeLines})

	msg := mustMessage(t)
	msg.Body = "one\ntwo\nthree"
	if _, err := block.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	// A tokenizing split cannot know the total while it is emitting, so every
	// element but the last carries sizeUnknown. The lookahead identifies the final
	// element and stamps it with the real count, so the group still learns its
	// size exactly once and completes on a count rather than a last-marker.
	elements := cont.resumed()
	for i, elem := range elements[:len(elements)-1] {
		if _, _, size := groupVarsOf(t, elem); size != sizeUnknown {
			t.Errorf("element %d groupSize = %d, want %d while streaming", i, size, sizeUnknown)
		}
	}
	if _, _, size := groupVarsOf(t, elements[len(elements)-1]); size != 3 {
		t.Errorf("final element groupSize = %d, want the real total 3", size)
	}
}

func TestSplitReportsTheRealTotalToBuildResponse(t *testing.T) {
	response := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "set-payload-from-size"},
	}}
	reg := testRegistry()
	reg.MustRegister("set-payload-from-size", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			size, _ := msg.Variables.Int(varGroupSize)
			msg.Body = map[string]any{"accepted": size}
			return msg, nil
		}), nil
	})

	proc, err := rootBuilder(reg).splitBlock(types.BlockConfig{
		Type: blockKindSplit, Mode: splitModeLines, BuildResponse: &response,
	})
	if err != nil {
		t.Fatalf("build split: %v", err)
	}
	block, _ := proc.(*split)
	block.SetContinuation(&recordingContinuation{})

	msg := mustMessage(t)
	msg.Body = "a\nb\nc\nd"
	out, err := block.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// buildResponse runs after every element is dispatched, which is the one place
	// a streaming split can report an honest total rather than "unknown".
	body, _ := out.Body.(map[string]any)
	if body["accepted"] != 4 {
		t.Fatalf("response body = %v, want the real total 4", body)
	}
	if !out.StopRequested() {
		t.Error("the flow should stop after the response is built")
	}
}

func TestSplitEmptyCollectionDispatchesNothing(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Items: "body.lines"})

	msg := mustMessage(t)
	msg.Body = map[string]any{"lines": []any{}}
	out, err := block.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(cont.resumed()) != 0 {
		t.Fatal("an empty collection should dispatch nothing")
	}
	if size, _ := out.Variables.Int(varGroupSize); size != 0 {
		t.Fatalf("groupSize = %d, want 0", size)
	}
}

func TestSplitDispatchErrorPolicy(t *testing.T) {
	tests := []struct {
		name      string
		onError   string
		wantError bool
	}{
		{name: "skip carries on", onError: splitOnErrorSkip},
		{name: "abort fails the block", onError: splitOnErrorAbort, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			block, cont := buildSplit(t, types.BlockConfig{Items: "body.lines", OnError: tt.onError})
			cont.err = errors.New("pool closed")

			msg := mustMessage(t)
			msg.Body = map[string]any{"lines": []any{"a", "b"}}
			_, err := block.Process(context.Background(), msg)
			if tt.wantError != (err != nil) {
				t.Fatalf("Process error = %v, want error: %v", err, tt.wantError)
			}
		})
	}
}

func TestSplitRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.BlockConfig
	}{
		{name: "expression mode without items", cfg: types.BlockConfig{Mode: splitModeExpression}},
		{name: "unknown mode", cfg: types.BlockConfig{Mode: "sideways"}},
		{name: "delimiter mode without a delimiter", cfg: types.BlockConfig{Mode: splitModeDelimiter}},
		{name: "chunk mode without a size", cfg: types.BlockConfig{Mode: splitModeChunk}},
		{name: "chunk mode with a negative size", cfg: types.BlockConfig{Mode: splitModeChunk, ChunkSize: -1}},
		{name: "unknown onError", cfg: types.BlockConfig{Items: "body.a", OnError: "explode"}},
		{name: "foreign slot", cfg: types.BlockConfig{Items: "body.a", Condition: "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Type = blockKindSplit
			if _, err := rootBuilder(testRegistry()).splitBlock(tt.cfg); err == nil {
				t.Fatal("expected a build error")
			}
		})
	}
}

func TestSplitRejectsBeingNested(t *testing.T) {
	// Inside a composite's slot there is no rest-of-the-flow to continue into, and
	// the author needs to hear that at build time rather than watch elements
	// vanish at runtime.
	nested := rootBuilder(testRegistry()).branch(core.BranchBody)
	_, err := nested.splitBlock(types.BlockConfig{Type: blockKindSplit, Items: "body.a"})
	if err == nil {
		t.Fatal("a split inside a composite slot must fail the build")
	}
	if !strings.Contains(err.Error(), "top-level") {
		t.Fatalf("error should say where a split may live, got %q", err)
	}
}

func TestSplitRequiresATextBodyToTokenize(t *testing.T) {
	block, _ := buildSplit(t, types.BlockConfig{Mode: splitModeLines})

	msg := mustMessage(t)
	msg.Body = map[string]any{"not": "text"}
	if _, err := block.Process(context.Background(), msg); err == nil {
		t.Fatal("tokenizing a non-text body should fail")
	}
}

func TestSplitRejectsANonArrayItemsResult(t *testing.T) {
	block, _ := buildSplit(t, types.BlockConfig{Items: "body.count"})

	msg := mustMessage(t)
	msg.Body = map[string]any{"count": float64(3)}
	if _, err := block.Process(context.Background(), msg); err == nil {
		t.Fatal("an items expression that is not a collection should fail")
	}
}

func TestSplitReadsARawContentBody(t *testing.T) {
	block, cont := buildSplit(t, types.BlockConfig{Mode: splitModeLines})

	msg := mustMessage(t)
	msg.SetRawBody("text/csv", "a\nb")
	if _, err := block.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(cont.resumed()) != 2 {
		t.Fatalf("resumed %d elements, want 2", len(cont.resumed()))
	}
}
