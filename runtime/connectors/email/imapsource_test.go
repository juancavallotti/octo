package email

import (
	"slices"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// newIMAPSource builds an imap source on f's connector with settings.
func newIMAPSource(t *testing.T, f *imapFixture, out chan *types.Message, settings map[string]any) *imapSource {
	t.Helper()
	src, err := f.conn.NewSource(types.SourceConfig{Connector: "inbox", Type: imapType, Settings: settings}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	return src.(*imapSource)
}

func TestIMAPSourceActsOnlyOnCompletedFlows(t *testing.T) {
	cases := []struct {
		name      string
		settings  map[string]any
		wantInbox map[string]bool
		wantDone  map[string]bool
	}{
		{
			name:      "seen",
			settings:  map[string]any{},
			wantInbox: map[string]bool{"old": true, "ok": true, "fails": false, "dropped": true},
			wantDone:  map[string]bool{},
		},
		{
			name:      "move",
			settings:  map[string]any{"onSuccess": "move", "moveTo": "Done"},
			wantInbox: map[string]bool{"old": true, "fails": false},
			wantDone:  map[string]bool{"ok": false, "dropped": false},
		},
		{
			name:      "delete",
			settings:  map[string]any{"onSuccess": "delete"},
			wantInbox: map[string]bool{"old": true, "fails": false},
			wantDone:  map[string]bool{},
		},
	}
	outcome := func(subject string) types.FlowEventKind {
		switch subject {
		case "fails":
			return types.FlowEventFailed
		case "dropped":
			return types.FlowEventDropped
		default:
			return types.FlowEventCompleted
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := startIMAP(t, securityTLS)
			f.deliver(t, "old", imap.FlagSeen)
			for _, s := range []string{"ok", "fails", "dropped"} {
				f.deliver(t, s)
			}
			out := make(chan *types.Message)
			flow := runFakeFlow(t, out, outcome)
			src := newIMAPSource(t, f, out, tc.settings)

			if err := src.poll(servicesCtx(t, true, newMemKV())); err != nil {
				t.Fatalf("poll: %v", err)
			}
			if got := flow.seen(); !slices.Equal(got, []string{"ok", "fails", "dropped"}) {
				t.Errorf("flow saw %v", got)
			}
			assertState(t, "INBOX", f.state(t, defaultMailbox), tc.wantInbox)
			assertState(t, "Done", f.state(t, "Done"), tc.wantDone)

			// The failed message is read again on the next poll, and only it.
			if err := src.poll(servicesCtx(t, true, newMemKV())); err != nil {
				t.Fatalf("second poll: %v", err)
			}
			if got := flow.seen(); !slices.Equal(got[3:], []string{"fails"}) {
				t.Errorf("second poll saw %v, want only the failed message", got[3:])
			}
		})
	}
}

func TestIMAPSourceMaxPerPollTakesOldestFirst(t *testing.T) {
	f := startIMAP(t, securityTLS)
	for _, s := range []string{"1", "2", "3"} {
		f.deliver(t, s)
	}
	out := make(chan *types.Message)
	flow := runFakeFlow(t, out, completeAll)
	src := newIMAPSource(t, f, out, map[string]any{"maxPerPoll": 2})

	ctx := servicesCtx(t, true, newMemKV())
	if err := src.poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"1", "2"}) {
		t.Errorf("first poll saw %v", got)
	}
	if err := src.poll(ctx); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Errorf("after second poll saw %v", got)
	}
}

func TestIMAPSourceCursorMode(t *testing.T) {
	f := startIMAP(t, securityTLS)
	f.deliver(t, "before")
	out := make(chan *types.Message)
	failing := map[string]bool{"b": true}
	flow := runFakeFlow(t, out, func(s string) types.FlowEventKind {
		if failing[s] {
			return types.FlowEventFailed
		}
		return types.FlowEventCompleted
	})
	src := newIMAPSource(t, f, out, map[string]any{"onSuccess": "none"})
	kv := newMemKV()
	ctx := servicesCtx(t, true, kv)

	// The first poll only places the cursor: mail already there is not new.
	if err := src.poll(ctx); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if got := flow.seen(); len(got) != 0 {
		t.Fatalf("first poll emitted %v", got)
	}

	for _, s := range []string{"a", "b", "c"} {
		f.deliver(t, s)
	}
	// a completes, b fails: the poll stops at b so the cursor never passes it.
	if err := src.poll(ctx); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("second poll saw %v", got)
	}

	failing["b"] = false
	if err := src.poll(ctx); err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if got := flow.seen(); !slices.Equal(got, []string{"a", "b", "b", "c"}) {
		t.Errorf("third poll saw %v", got)
	}
	// none never changes the mailbox.
	assertState(t, "INBOX", f.state(t, defaultMailbox), map[string]bool{"before": false, "a": false, "b": false, "c": false})

	// A cursor from another UIDVALIDITY restarts at the mailbox's end.
	cursor, version, err := src.loadCursor(ctx, kv)
	if err != nil {
		t.Fatalf("loadCursor: %v", err)
	}
	cursor.UIDValidity++
	cursor.LastUID = 0
	if _, err := src.saveCursor(ctx, kv, cursor, version); err != nil {
		t.Fatalf("saveCursor: %v", err)
	}
	if err := src.poll(ctx); err != nil {
		t.Fatalf("fourth poll: %v", err)
	}
	if got := flow.seen(); len(got) != 4 {
		t.Errorf("a UIDVALIDITY reset replayed mail: %v", got)
	}
}

func TestIMAPSourceOnlyLeaderPolls(t *testing.T) {
	for _, leader := range []bool{true, false} {
		f := startIMAP(t, securityTLS)
		f.deliver(t, "hello")
		out := make(chan *types.Message)
		flow := runFakeFlow(t, out, completeAll)
		src := newIMAPSource(t, f, out, map[string]any{"interval": "1h"})

		if err := src.Start(servicesCtx(t, leader, newMemKV())); err != nil {
			t.Fatalf("Start: %v", err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for leader && len(flow.seen()) == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !leader {
			time.Sleep(100 * time.Millisecond)
		}
		if err := src.Stop(t.Context()); err != nil {
			t.Fatalf("Stop: %v", err)
		}

		if got := len(flow.seen()); (got == 1) != leader {
			t.Errorf("leader=%v: flow saw %d messages", leader, got)
		}
	}
}

func TestIMAPSourceSettingsValidation(t *testing.T) {
	f := startIMAP(t, securityTLS)
	for name, settings := range map[string]map[string]any{
		"move without target": {"onSuccess": "move"},
		"unknown action":      {"onSuccess": "archive"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.conn.NewSource(types.SourceConfig{Settings: settings}, make(chan *types.Message), core.SourceDeps{})
			if err == nil {
				t.Fatal("expected a settings error")
			}
		})
	}
}

func TestIMAPSourceOneSourcePerMailbox(t *testing.T) {
	f := startIMAP(t, securityTLS)
	build := func(settings map[string]any) (*imapSource, error) {
		src, err := f.conn.NewSource(types.SourceConfig{Connector: "inbox", Type: imapType, Settings: settings},
			make(chan *types.Message), core.SourceDeps{})
		if err != nil {
			return nil, err
		}
		return src.(*imapSource), nil
	}

	first, err := build(map[string]any{})
	if err != nil {
		t.Fatalf("first source: %v", err)
	}
	// INBOX is case-insensitive in IMAP, so "inbox" names the same mailbox.
	if _, err := build(map[string]any{"mailbox": "inbox"}); err == nil {
		t.Fatal("a second source on INBOX should be refused")
	}
	other, err := build(map[string]any{"mailbox": "Done"})
	if err != nil {
		t.Fatalf("a source on another mailbox: %v", err)
	}
	if first.poller.leaderKey == other.poller.leaderKey {
		t.Errorf("mailboxes share leader key %q", first.poller.leaderKey)
	}
	if first.cursorKey != "email_imap_inbox/INBOX" || other.cursorKey != "email_imap_inbox/Done" {
		t.Errorf("cursor keys = %q, %q", first.cursorKey, other.cursorKey)
	}
}
