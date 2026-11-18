package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakePOP3 is a minimal POP3 drop: USER/PASS, STLS, UIDL, RETR, DELE and QUIT,
// with deletions applied only on QUIT as the protocol requires.
type fakePOP3 struct {
	tls      *tls.Config
	startTLS bool

	mu       sync.Mutex
	messages []fakeDropMessage
	nextID   int
}

type fakeDropMessage struct {
	uidl    string
	subject string
	// raw replaces the generated message when set.
	raw []byte
}

func (f *fakePOP3) add(subject string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.messages = append(f.messages, fakeDropMessage{uidl: "uid-" + strconv.Itoa(f.nextID), subject: subject})
}

// addRaw drops a message whose bytes are exactly raw, named subject in subjects().
func (f *fakePOP3) addRaw(subject string, raw []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.messages = append(f.messages, fakeDropMessage{uidl: "uid-" + strconv.Itoa(f.nextID), subject: subject, raw: raw})
}

func (f *fakePOP3) subjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.messages))
	for _, m := range f.messages {
		out = append(out, m.subject)
	}
	return out
}

func (f *fakePOP3) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

//nolint:gocyclo // a test server's command switch
func (f *fakePOP3) session(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("+OK fake POP3")
	f.mu.Lock()
	view := slices.Clone(f.messages)
	f.mu.Unlock()
	deleted := map[int]bool{}
	authed := false

	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb, arg, _ := strings.Cut(line, " ")
		switch strings.ToUpper(verb) {
		case "STLS":
			if !f.startTLS {
				_ = tp.PrintfLine("-ERR no tls")
				continue
			}
			_ = tp.PrintfLine("+OK begin tls")
			tlsConn := tls.Server(conn, f.tls)
			if tlsConn.HandshakeContext(context.Background()) != nil {
				return
			}
			conn, tp = tlsConn, textproto.NewConn(tlsConn)
		case "USER":
			_ = tp.PrintfLine("+OK")
		case "PASS":
			authed = arg == "s3cret"
			if authed {
				_ = tp.PrintfLine("+OK logged in")
			} else {
				_ = tp.PrintfLine("-ERR bad password")
			}
		case "UIDL":
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			for i, m := range view {
				if !deleted[i+1] {
					_, _ = fmt.Fprintf(w, "%d %s\r\n", i+1, m.uidl)
				}
			}
			_ = w.Close()
		case "RETR":
			n, _ := strconv.Atoi(arg)
			if !authed || n < 1 || n > len(view) || deleted[n] {
				_ = tp.PrintfLine("-ERR no such message")
				continue
			}
			_ = tp.PrintfLine("+OK")
			w := tp.DotWriter()
			if view[n-1].raw != nil {
				_, _ = w.Write(view[n-1].raw)
			} else {
				_, _ = w.Write(crlf(fmt.Sprintf("From: a@example.com\nSubject: %s\n\nbody\n", view[n-1].subject)))
			}
			_ = w.Close()
		case "DELE":
			n, _ := strconv.Atoi(arg)
			deleted[n] = true
			_ = tp.PrintfLine("+OK")
		case "QUIT":
			f.commit(view, deleted)
			_ = tp.PrintfLine("+OK bye")
			return
		default:
			_ = tp.PrintfLine("-ERR unknown")
		}
	}
}

// commit removes the messages a session deleted, by UIDL.
func (f *fakePOP3) commit(view []fakeDropMessage, deleted map[int]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = slices.DeleteFunc(f.messages, func(m fakeDropMessage) bool {
		for n := range deleted {
			if view[n-1].uidl == m.uidl {
				return true
			}
		}
		return false
	})
}

// pop3Fixture is a fake drop and a started connector pointing at it.
type pop3Fixture struct {
	srv  *fakePOP3
	conn *POP3Connector
}

func startPOP3(t *testing.T, security string) *pop3Fixture {
	t.Helper()
	cfg := selfSignedTLS(t)
	srv := &fakePOP3{tls: cfg, startTLS: security == securityStartTLS}
	ln := listen(t, cfg, security == securityTLS)
	go srv.serve(ln)
	host, port := hostPort(t, ln)
	conn := &POP3Connector{}
	startConnector(t, conn, map[string]any{
		"host": host, "port": port, "security": security,
		"tlsSkipVerify": true, "username": "bot", "password": "s3cret",
	})
	return &pop3Fixture{srv: srv, conn: conn}
}

func TestPOP3FetchSecurityModes(t *testing.T) {
	for _, security := range []string{securityNone, securityStartTLS, securityTLS} {
		t.Run(security, func(t *testing.T) {
			f := startPOP3(t, security)
			f.srv.add("one")
			proc, err := newPOP3Fetch(map[string]any{"connector": "drop"}, depsWith("drop", f.conn))
			if err != nil {
				t.Fatalf("newPOP3Fetch: %v", err)
			}
			out, err := proc.Process(t.Context(), newMessage(t, nil))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if got := subjects(t, out.Body); !slices.Equal(got, []string{"one"}) {
				t.Errorf("subjects = %v", got)
			}
			if uidl := out.Body.([]any)[0].(map[string]any)[keyUIDL]; uidl != "uid-1" {
				t.Errorf("uidl = %v", uidl)
			}
		})
	}
}

func TestPOP3FetchLimitAndDelete(t *testing.T) {
	cases := []struct {
		name     string
		settings map[string]any
		wantRead []string
		wantLeft []string
	}{
		{"keep", map[string]any{}, []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"limit keeps the newest", map[string]any{"limit": 2}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{"delete", map[string]any{"limit": 2, "onFetch": "delete"}, []string{"b", "c"}, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := startPOP3(t, securityTLS)
			for _, s := range []string{"a", "b", "c"} {
				f.srv.add(s)
			}
			settings := map[string]any{"connector": "drop"}
			for k, v := range tc.settings {
				settings[k] = v
			}
			proc, err := newPOP3Fetch(settings, depsWith("drop", f.conn))
			if err != nil {
				t.Fatalf("newPOP3Fetch: %v", err)
			}
			out, err := proc.Process(t.Context(), newMessage(t, nil))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if got := subjects(t, out.Body); !slices.Equal(got, tc.wantRead) {
				t.Errorf("read %v, want %v", got, tc.wantRead)
			}
			if got := f.srv.subjects(); !slices.Equal(got, tc.wantLeft) {
				t.Errorf("left on server %v, want %v", got, tc.wantLeft)
			}
		})
	}
}

func TestPOP3FailedSessionDeletesNothing(t *testing.T) {
	f := startPOP3(t, securityTLS)
	f.srv.add("a")
	err := f.conn.session(t.Context(), func(client *pop3Client) error {
		if err := removeByUIDL(client, []string{"uid-1"}); err != nil {
			return err
		}
		return fmt.Errorf("later step failed")
	})
	if err == nil {
		t.Fatal("expected the session error")
	}
	if got := f.srv.subjects(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("a failed session deleted mail: %v", got)
	}
}

func TestPOP3Validation(t *testing.T) {
	f := startPOP3(t, securityTLS)
	if _, err := newPOP3Fetch(map[string]any{"connector": "drop", "onFetch": "seen"}, depsWith("drop", f.conn)); err == nil {
		t.Error("expected an unknown-action error")
	}
	if err := (&POP3Connector{}).Start(t.Context(), typesConfig(map[string]any{"host": "h"})); err == nil {
		t.Error("expected a missing-username error")
	}

	bad := &POP3Connector{}
	startConnector(t, bad, map[string]any{
		"host": f.conn.ep.host, "port": f.conn.ep.port, "tlsSkipVerify": true,
		"username": "bot", "password": "wrong",
	})
	if err := bad.session(t.Context(), func(*pop3Client) error { return nil }); err == nil {
		t.Error("expected a login error")
	}
}

func TestPOP3FetchDeletesNothingWhenAMessageCannotBeParsed(t *testing.T) {
	f := startPOP3(t, securityTLS)
	f.srv.add("good")
	f.srv.addRaw("broken", crlf("this is not a header line\n\nbody\n"))
	proc, err := newPOP3Fetch(map[string]any{"connector": "drop", "onFetch": "delete"}, depsWith("drop", f.conn))
	if err != nil {
		t.Fatalf("newPOP3Fetch: %v", err)
	}
	if _, err := proc.Process(t.Context(), newMessage(t, nil)); err == nil {
		t.Fatal("expected the unparseable message to fail the fetch")
	}
	if got := f.srv.subjects(); !slices.Equal(got, []string{"good", "broken"}) {
		t.Errorf("left on server %v, want both messages", got)
	}
}
