package email

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"testing"
)

// fakeSMTP is a minimal SMTP server: enough of EHLO, STARTTLS, AUTH PLAIN, MAIL,
// RCPT and DATA to record what a client submitted.
type fakeSMTP struct {
	tls      *tls.Config
	startTLS bool

	mu     sync.Mutex
	auth   string
	from   string
	rcpts  []string
	data   string
	sawTLS bool
	helo   string
}

func (f *fakeSMTP) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go f.session(conn)
	}
}

func (f *fakeSMTP) session(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_, isTLS := conn.(*tls.Conn)
	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			f.record(func() { f.helo = strings.TrimSpace(strings.TrimPrefix(line, verb)) })
			if f.startTLS && !isTLS {
				_ = tp.PrintfLine("250-fake\r\n250-STARTTLS\r\n250 AUTH PLAIN")
			} else {
				_ = tp.PrintfLine("250-fake\r\n250 AUTH PLAIN")
			}
		case "STARTTLS":
			_ = tp.PrintfLine("220 go ahead")
			tlsConn := tls.Server(conn, f.tls)
			if tlsConn.HandshakeContext(context.Background()) != nil {
				return
			}
			conn, isTLS = tlsConn, true
			tp = textproto.NewConn(conn)
		case "AUTH":
			f.record(func() { f.auth = strings.TrimPrefix(line, "AUTH PLAIN ") })
			_ = tp.PrintfLine("235 ok")
		case "MAIL":
			f.record(func() { f.from = line; f.sawTLS = isTLS })
			_ = tp.PrintfLine("250 ok")
		case "RCPT":
			f.record(func() { f.rcpts = append(f.rcpts, line) })
			_ = tp.PrintfLine("250 ok")
		case "DATA":
			_ = tp.PrintfLine("354 go")
			lines, err := tp.ReadDotLines()
			if err != nil {
				return
			}
			f.record(func() { f.data = strings.Join(lines, "\n") })
			_ = tp.PrintfLine("250 queued")
		case "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("250 ok")
		}
	}
}

func (f *fakeSMTP) record(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fakeSMTP) snapshot() fakeSMTP {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeSMTP{helo: f.helo, auth: f.auth, from: f.from, rcpts: append([]string(nil), f.rcpts...), data: f.data, sawTLS: f.sawTLS}
}

// startFakeSMTP serves a fake SMTP server in the given security mode and returns
// it with connector settings pointing at it.
func startFakeSMTP(t *testing.T, security string) (*fakeSMTP, map[string]any) {
	t.Helper()
	cfg := selfSignedTLS(t)
	srv := &fakeSMTP{tls: cfg, startTLS: security == securityStartTLS}
	ln := listen(t, cfg, security == securityTLS)
	go srv.serve(ln)
	host, port := hostPort(t, ln)
	return srv, map[string]any{
		"host":          host,
		"port":          port,
		"security":      security,
		"tlsSkipVerify": true,
		"username":      "bot",
		"password":      "s3cret",
		"heloName":      "runner.example.net",
	}
}

func TestSMTPSendSecurityModes(t *testing.T) {
	for _, security := range []string{securityNone, securityStartTLS, securityTLS} {
		t.Run(security, func(t *testing.T) {
			srv, settings := startFakeSMTP(t, security)
			conn := &SMTPConnector{}
			startConnector(t, conn, settings)

			proc, err := newSMTPSend(map[string]any{
				"connector": "mail",
				"from":      `"Octo <octo@example.com>"`,
				"to":        "body.to",
				"bcc":       `["audit@example.com"]`,
				"subject":   `"Hello " + body.name`,
				"inReplyTo": `"<parent@example.com>"`,
				"text":      `"Hi " + body.name`,
			}, depsWith("mail", conn))
			if err != nil {
				t.Fatalf("newSMTPSend: %v", err)
			}
			msg := newMessage(t, map[string]any{"to": "Ann <ann@example.com>, bob@example.com", "name": "Ann"})
			out, err := proc.Process(t.Context(), msg)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}

			got := srv.snapshot()
			wantTLS := security != securityNone
			if got.sawTLS != wantTLS {
				t.Errorf("session encrypted = %v, want %v", got.sawTLS, wantTLS)
			}
			if got.helo != "runner.example.net" {
				t.Errorf("EHLO name = %q, want the configured heloName", got.helo)
			}
			wantAuth := base64.StdEncoding.EncodeToString([]byte("\x00bot\x00s3cret"))
			if got.auth != wantAuth {
				t.Errorf("auth = %q, want %q", got.auth, wantAuth)
			}
			if got.from != "MAIL FROM:<octo@example.com> BODY=8BITMIME" && got.from != "MAIL FROM:<octo@example.com>" {
				t.Errorf("from = %q", got.from)
			}
			if len(got.rcpts) != 3 {
				t.Fatalf("rcpts = %v, want ann, bob and the bcc", got.rcpts)
			}
			if strings.Contains(got.data, "audit@example.com") {
				t.Error("bcc address leaked into the message headers")
			}
			if !strings.Contains(got.data, "In-Reply-To: <parent@example.com>") {
				t.Errorf("message not threaded:\n%s", got.data)
			}
			if !strings.Contains(got.data, "Subject: Hello Ann") || !strings.Contains(got.data, "Hi Ann") {
				t.Errorf("message missing subject or body:\n%s", got.data)
			}
			if id, _ := out.Variables.String(messageIDVar); !strings.HasSuffix(id, "@example.com") {
				t.Errorf("message id %q should name the sender's domain", id)
			}
		})
	}
}

func TestSMTPSendRequiresStartTLSWhenConfigured(t *testing.T) {
	// The server is plain and never offers STARTTLS; a starttls connector must
	// refuse rather than send credentials in clear text.
	_, settings := startFakeSMTP(t, securityNone)
	settings["security"] = securityStartTLS
	conn := &SMTPConnector{}
	startConnector(t, conn, settings)

	err := conn.Send(t.Context(), "a@example.com", []string{"b@example.com"}, []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("err = %v, want a missing-STARTTLS error", err)
	}
}

func TestSMTPSendWithAttachmentsAndAlternatives(t *testing.T) {
	srv, settings := startFakeSMTP(t, securityTLS)
	conn := &SMTPConnector{}
	startConnector(t, conn, settings)

	proc, err := newSMTPSend(map[string]any{
		"connector":   "mail",
		"from":        `"octo@example.com"`,
		"to":          `"ann@example.com"`,
		"text":        `"plain"`,
		"html":        `"<p>rich</p>"`,
		"attachments": "body.files",
	}, depsWith("mail", conn))
	if err != nil {
		t.Fatalf("newSMTPSend: %v", err)
	}
	files := []any{
		map[string]any{"filename": "a.bin", "contentType": "application/pdf", "encoding": "base64", "data": base64.StdEncoding.EncodeToString([]byte{0, 1, 2})},
		map[string]any{"filename": "notes.txt", "encoding": "text", "data": "hello"},
	}
	if _, err := proc.Process(t.Context(), newMessage(t, map[string]any{"files": files})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	data := srv.snapshot().data
	for _, want := range []string{"multipart/mixed", "multipart/alternative", "text/plain", "text/html", `filename=a.bin`, `filename=notes.txt`, "application/pdf"} {
		if !strings.Contains(data, want) {
			t.Errorf("message missing %q:\n%s", want, data)
		}
	}
}

func TestSMTPSendFactoryValidation(t *testing.T) {
	conn := &SMTPConnector{}
	startConnector(t, conn, map[string]any{"host": "127.0.0.1"})
	deps := depsWith("mail", conn)
	base := map[string]any{"connector": "mail", "from": `"a@x.com"`, "to": `"b@x.com"`, "text": `"t"`}

	cases := map[string]func(map[string]any){
		"no body":        func(s map[string]any) { delete(s, "text") },
		"no from":        func(s map[string]any) { delete(s, "from") },
		"unknown conn":   func(s map[string]any) { s["connector"] = "nope" },
		"bad expression": func(s map[string]any) { s["subject"] = "body.(" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			settings := map[string]any{}
			for k, v := range base {
				settings[k] = v
			}
			mutate(settings)
			if _, err := newSMTPSend(settings, deps); err == nil {
				t.Fatal("expected a factory error")
			}
		})
	}
}

func TestEndpointDefaults(t *testing.T) {
	cases := []struct {
		security string
		wantPort int
	}{
		{"", 587},
		{securityNone, 25},
		{securityStartTLS, 587},
		{securityTLS, 465},
	}
	for _, tc := range cases {
		ep, err := newEndpoint(smtpProtocol, serverSettings{host: "mail.example.com", security: tc.security})
		if err != nil {
			t.Fatalf("security %q: %v", tc.security, err)
		}
		if ep.port != tc.wantPort {
			t.Errorf("security %q: port = %d, want %d", tc.security, ep.port, tc.wantPort)
		}
	}
	if _, err := newEndpoint(smtpProtocol, serverSettings{host: "h", security: "ssl"}); err == nil {
		t.Error("expected an unknown-security error")
	}
	if _, err := newEndpoint(smtpProtocol, serverSettings{}); err == nil {
		t.Error("expected a missing-host error")
	}
}
