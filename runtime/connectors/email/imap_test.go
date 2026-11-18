package email

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

// imapFixture is an in-process IMAP server with one user and an INBOX and Done
// mailbox, plus a started connector pointing at it.
type imapFixture struct {
	user     *imapmemserver.User
	conn     *IMAPConnector
	settings map[string]any
}

func startIMAP(t *testing.T, security string) *imapFixture {
	t.Helper()
	return startIMAPWithCaps(t, security, imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}})
}

// startIMAPWithCaps is startIMAP with the capabilities the server advertises,
// to exercise servers that lack MOVE or UIDPLUS.
func startIMAPWithCaps(t *testing.T, security string, caps imap.CapSet) *imapFixture {
	t.Helper()
	cfg := selfSignedTLS(t)
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("bot", "s3cret")
	for _, name := range []string{defaultMailbox, "Done"} {
		if err := user.Create(name, nil); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	mem.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         caps,
		TLSConfig:    cfg,
		InsecureAuth: true,
		Logger:       discardLogger{},
	})
	ln := listen(t, cfg, security == securityTLS)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	host, port := hostPort(t, ln)
	settings := map[string]any{
		"host": host, "port": port, "security": security,
		"tlsSkipVerify": true, "username": "bot", "password": "s3cret",
	}
	conn := &IMAPConnector{}
	startConnector(t, conn, settings)
	return &imapFixture{user: user, conn: conn, settings: settings}
}

type discardLogger struct{}

func (discardLogger) Printf(string, ...any) {}

// deliver appends a minimal message with the given subject to the INBOX.
func (f *imapFixture) deliver(t *testing.T, subject string, flags ...imap.Flag) {
	t.Helper()
	raw := crlf(fmt.Sprintf("From: a@example.com\nTo: bot@example.com\nSubject: %s\nContent-Type: text/plain\n\nbody of %s\n", subject, subject))
	if _, err := f.user.Append(defaultMailbox, bytes.NewReader(raw), &imap.AppendOptions{Flags: flags}); err != nil {
		t.Fatalf("append: %v", err)
	}
}

// state returns each message in mailbox as subject → whether it is seen.
func (f *imapFixture) state(t *testing.T, mailbox string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	err := f.conn.session(context.Background(), func(client *imapclient.Client) error {
		sel, err := client.Select(mailbox, nil).Wait()
		if err != nil || sel.NumMessages == 0 {
			return err
		}
		msgs, err := client.Fetch(imap.SeqSet{imap.SeqRange{Start: 1}}, &imap.FetchOptions{Flags: true, Envelope: true}).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			out[m.Envelope.Subject] = slices.Contains(m.Flags, imap.FlagSeen)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect %s: %v", mailbox, err)
	}
	return out
}

func subjects(t *testing.T, body any) []string {
	t.Helper()
	list, ok := body.([]any)
	if !ok {
		t.Fatalf("body = %T, want a list", body)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, item.(map[string]any)[keySubject].(string))
	}
	return out
}

func TestIMAPFetchSecurityModes(t *testing.T) {
	for _, security := range []string{securityNone, securityStartTLS, securityTLS} {
		t.Run(security, func(t *testing.T) {
			f := startIMAP(t, security)
			f.deliver(t, "one")

			proc, err := newIMAPFetch(map[string]any{"connector": "inbox"}, depsWith("inbox", f.conn))
			if err != nil {
				t.Fatalf("newIMAPFetch: %v", err)
			}
			out, err := proc.Process(t.Context(), newMessage(t, nil))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			got := subjects(t, out.Body)
			if !slices.Equal(got, []string{"one"}) {
				t.Errorf("subjects = %v", got)
			}
			item := out.Body.([]any)[0].(map[string]any)
			if item[keyUID] != int64(1) || item[keyMailbox] != defaultMailbox {
				t.Errorf("uid/mailbox = %v/%v", item[keyUID], item[keyMailbox])
			}
		})
	}
}

func TestIMAPFetchSearchLimitAndActions(t *testing.T) {
	cases := []struct {
		name         string
		settings     map[string]any
		wantSubjects []string
		wantInbox    map[string]bool
		wantDone     map[string]bool
	}{
		{
			name:         "unseen, none leaves the mailbox alone",
			settings:     map[string]any{},
			wantSubjects: []string{"b", "c", "d"},
			wantInbox:    map[string]bool{"a": true, "b": false, "c": false, "d": false},
			wantDone:     map[string]bool{},
		},
		{
			name:         "all with a limit keeps the newest",
			settings:     map[string]any{"search": "all", "limit": 2},
			wantSubjects: []string{"c", "d"},
			wantInbox:    map[string]bool{"a": true, "b": false, "c": false, "d": false},
			wantDone:     map[string]bool{},
		},
		{
			name:         "seen",
			settings:     map[string]any{"onFetch": "seen", "limit": 2},
			wantSubjects: []string{"c", "d"},
			wantInbox:    map[string]bool{"a": true, "b": false, "c": true, "d": true},
			wantDone:     map[string]bool{},
		},
		{
			name:         "move",
			settings:     map[string]any{"onFetch": "move", "moveTo": "Done"},
			wantSubjects: []string{"b", "c", "d"},
			wantInbox:    map[string]bool{"a": true},
			wantDone:     map[string]bool{"b": false, "c": false, "d": false},
		},
		{
			name:         "delete",
			settings:     map[string]any{"onFetch": "delete", "search": "all"},
			wantSubjects: []string{"a", "b", "c", "d"},
			wantInbox:    map[string]bool{},
			wantDone:     map[string]bool{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := startIMAP(t, securityTLS)
			f.deliver(t, "a", imap.FlagSeen)
			for _, s := range []string{"b", "c", "d"} {
				f.deliver(t, s)
			}

			settings := map[string]any{"connector": "inbox"}
			for k, v := range tc.settings {
				settings[k] = v
			}
			proc, err := newIMAPFetch(settings, depsWith("inbox", f.conn))
			if err != nil {
				t.Fatalf("newIMAPFetch: %v", err)
			}
			out, err := proc.Process(t.Context(), newMessage(t, nil))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if got := subjects(t, out.Body); !slices.Equal(got, tc.wantSubjects) {
				t.Errorf("subjects = %v, want %v", got, tc.wantSubjects)
			}
			assertState(t, "INBOX", f.state(t, defaultMailbox), tc.wantInbox)
			assertState(t, "Done", f.state(t, "Done"), tc.wantDone)
		})
	}
}

func assertState(t *testing.T, mailbox string, got, want map[string]bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", mailbox, got, want)
		return
	}
	for k, v := range want {
		if seen, ok := got[k]; !ok || seen != v {
			t.Errorf("%s = %v, want %v", mailbox, got, want)
			return
		}
	}
}

func TestIMAPFetchEmptyMailboxIsAnEmptyList(t *testing.T) {
	f := startIMAP(t, securityTLS)
	proc, err := newIMAPFetch(map[string]any{"connector": "inbox"}, depsWith("inbox", f.conn))
	if err != nil {
		t.Fatalf("newIMAPFetch: %v", err)
	}
	out, err := proc.Process(t.Context(), newMessage(t, nil))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if list, ok := out.Body.([]any); !ok || len(list) != 0 {
		t.Errorf("body = %#v, want an empty list", out.Body)
	}
}

func TestIMAPFetchFactoryValidation(t *testing.T) {
	f := startIMAP(t, securityTLS)
	deps := depsWith("inbox", f.conn)
	cases := map[string]map[string]any{
		"move without target": {"connector": "inbox", "onFetch": "move"},
		"unknown action":      {"connector": "inbox", "onFetch": "archive"},
		"unknown search":      {"connector": "inbox", "search": "flagged"},
		"bad since":           {"connector": "inbox", "since": "body.("},
	}
	for name, settings := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := newIMAPFetch(settings, deps); err == nil {
				t.Fatal("expected a factory error")
			}
		})
	}
}

func TestIMAPLoginFailureSurfaces(t *testing.T) {
	f := startIMAP(t, securityTLS)
	settings := map[string]any{}
	for k, v := range f.settings {
		settings[k] = v
	}
	settings["password"] = "wrong"
	conn := &IMAPConnector{}
	startConnector(t, conn, settings)
	proc, err := newIMAPFetch(map[string]any{"connector": "inbox"}, depsWith("inbox", conn))
	if err != nil {
		t.Fatalf("newIMAPFetch: %v", err)
	}
	if _, err := proc.Process(t.Context(), newMessage(t, nil)); err == nil {
		t.Fatal("expected a login error")
	}
}

func TestIMAPActionsTouchOnlyHandledMessages(t *testing.T) {
	rev1 := imap.CapSet{imap.CapIMAP4rev1: {}}
	uidPlus := imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapUIDPlus: {}}

	t.Run("delete without UIDPLUS is refused before anything is read", func(t *testing.T) {
		f := startIMAPWithCaps(t, securityTLS, rev1)
		f.deliver(t, "a")
		proc, err := newIMAPFetch(map[string]any{"connector": "inbox", "onFetch": "delete"}, depsWith("inbox", f.conn))
		if err != nil {
			t.Fatalf("newIMAPFetch: %v", err)
		}
		if _, err := proc.Process(t.Context(), newMessage(t, nil)); err == nil || !strings.Contains(err.Error(), "UIDPLUS") {
			t.Fatalf("err = %v, want a UIDPLUS refusal", err)
		}
		assertState(t, "INBOX", f.state(t, defaultMailbox), map[string]bool{"a": false})
	})

	t.Run("move without MOVE copies, then removes only the handled message", func(t *testing.T) {
		f := startIMAPWithCaps(t, securityTLS, uidPlus)
		f.deliver(t, "keep-me", imap.FlagSeen, imap.FlagDeleted) // read, and pending deletion by someone else
		f.deliver(t, "b")
		proc, err := newIMAPFetch(map[string]any{"connector": "inbox", "onFetch": "move", "moveTo": "Done"},
			depsWith("inbox", f.conn))
		if err != nil {
			t.Fatalf("newIMAPFetch: %v", err)
		}
		if _, err := proc.Process(t.Context(), newMessage(t, nil)); err != nil {
			t.Fatalf("Process: %v", err)
		}
		inbox := f.state(t, defaultMailbox)
		if _, ok := inbox["keep-me"]; !ok || len(inbox) != 1 {
			t.Errorf("INBOX = %v, want only the message flagged by someone else", inbox)
		}
		assertState(t, "Done", f.state(t, "Done"), map[string]bool{"b": false})
	})

	t.Run("a failed copy removes nothing", func(t *testing.T) {
		f := startIMAPWithCaps(t, securityTLS, uidPlus)
		f.deliver(t, "c")
		proc, err := newIMAPFetch(map[string]any{"connector": "inbox", "onFetch": "move", "moveTo": "Missing"},
			depsWith("inbox", f.conn))
		if err != nil {
			t.Fatalf("newIMAPFetch: %v", err)
		}
		if _, err := proc.Process(t.Context(), newMessage(t, nil)); err == nil {
			t.Fatal("expected the copy to a missing mailbox to fail")
		}
		assertState(t, "INBOX", f.state(t, defaultMailbox), map[string]bool{"c": false})
	})
}
