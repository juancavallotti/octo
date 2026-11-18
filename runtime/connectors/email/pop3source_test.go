package email

import (
	"slices"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func newPOP3Source(t *testing.T, f *pop3Fixture, out chan *types.Message, settings map[string]any) *pop3Source {
	t.Helper()
	src, err := f.conn.NewSource(types.SourceConfig{Connector: "drop", Type: pop3Type, Settings: settings}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	return src.(*pop3Source)
}

func failSubject(name string) func(string) types.FlowEventKind {
	return func(s string) types.FlowEventKind {
		if s == name {
			return types.FlowEventFailed
		}
		return types.FlowEventCompleted
	}
}

func TestPOP3SourceDeleteMode(t *testing.T) {
	f := startPOP3(t, securityTLS)
	for _, s := range []string{"a", "fails", "c"} {
		f.srv.add(s)
	}
	out := make(chan *types.Message)
	flow := runFakeFlow(t, out, failSubject("fails"))
	src := newPOP3Source(t, f, out, map[string]any{"onSuccess": "delete"})
	ctx := servicesCtx(t, true, newMemKV())

	if err := src.poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"a", "fails", "c"}) {
		t.Errorf("flow saw %v", got)
	}
	if got := f.srv.subjects(); !slices.Equal(got, []string{"fails"}) {
		t.Errorf("left on server %v, want only the failed message", got)
	}
}

func TestPOP3SourceKeepMode(t *testing.T) {
	f := startPOP3(t, securityTLS)
	f.srv.add("before")
	out := make(chan *types.Message)
	flow := runFakeFlow(t, out, failSubject("fails"))
	src := newPOP3Source(t, f, out, map[string]any{})
	ctx := servicesCtx(t, true, newMemKV())

	// The first poll records what is already there and handles nothing.
	if err := src.poll(ctx); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if got := flow.seen(); len(got) != 0 {
		t.Fatalf("first poll emitted %v", got)
	}

	f.srv.add("new")
	f.srv.add("fails")
	if err := src.poll(ctx); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"new", "fails"}) {
		t.Fatalf("second poll saw %v", got)
	}

	// Only the failed message is read again; nothing is ever deleted.
	if err := src.poll(ctx); err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"new", "fails", "fails"}) {
		t.Errorf("third poll saw %v", got)
	}
	if got := f.srv.subjects(); len(got) != 3 {
		t.Errorf("keep mode changed the drop: %v", got)
	}

	// The record is pruned to what is still on the server.
	f.srv.mu.Lock()
	f.srv.messages = f.srv.messages[2:]
	f.srv.mu.Unlock()
	if err := src.poll(ctx); err != nil {
		t.Fatalf("fourth poll: %v", err)
	}
	record, _, err := src.loadHandled(ctx, servicesKV(ctx))
	if err != nil {
		t.Fatalf("loadHandled: %v", err)
	}
	if len(record.UIDLs) != 0 {
		t.Errorf("handled set kept UIDLs no longer on the server: %v", record.UIDLs)
	}
}

func TestPOP3SourceSettingsValidation(t *testing.T) {
	f := startPOP3(t, securityTLS)
	_, err := f.conn.NewSource(types.SourceConfig{Settings: map[string]any{"onSuccess": "seen"}}, make(chan *types.Message), core.SourceDeps{})
	if err == nil {
		t.Fatal("expected a settings error")
	}
}

func TestPOP3SourceOneSourcePerConnector(t *testing.T) {
	f := startPOP3(t, securityTLS)
	cfg := types.SourceConfig{Connector: "drop", Type: pop3Type, Settings: map[string]any{}}
	if _, err := f.conn.NewSource(cfg, make(chan *types.Message), core.SourceDeps{}); err != nil {
		t.Fatalf("first source: %v", err)
	}
	if _, err := f.conn.NewSource(cfg, make(chan *types.Message), core.SourceDeps{}); err == nil {
		t.Fatal("a second source on one pop3 connector should be refused")
	}
}

func TestPOP3SourceNeverDeletesWhatNoFlowSaw(t *testing.T) {
	f := startPOP3(t, securityTLS)
	f.srv.addRaw("broken", crlf("this is not a header line\n\nbody\n"))
	f.srv.add("good")
	out := make(chan *types.Message)
	flow := runFakeFlow(t, out, completeAll)
	src := newPOP3Source(t, f, out, map[string]any{"onSuccess": "delete"})
	if err := src.poll(servicesCtx(t, true, newMemKV())); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"good"}) {
		t.Errorf("flow saw %v", got)
	}
	if got := f.srv.subjects(); !slices.Equal(got, []string{"broken"}) {
		t.Errorf("left on server %v, want the unreadable message kept", got)
	}
}

func TestPOP3SourceBaselineSurvivesAFailedSave(t *testing.T) {
	f := startPOP3(t, securityTLS)
	f.srv.add("old")
	out := make(chan *types.Message)
	flow := runFakeFlow(t, out, completeAll)
	src := newPOP3Source(t, f, out, map[string]any{})
	kv := newMemKV()
	kv.failSets = 1
	ctx := servicesCtx(t, true, kv)

	if err := src.poll(ctx); err == nil {
		t.Fatal("expected the first save to fail")
	}
	f.srv.add("arrived-meanwhile")
	if err := src.poll(ctx); err != nil {
		t.Fatalf("retry poll: %v", err)
	}
	if err := src.poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"arrived-meanwhile"}) {
		t.Errorf("flow saw %v, want the message that arrived after the first attempt", got)
	}
}
