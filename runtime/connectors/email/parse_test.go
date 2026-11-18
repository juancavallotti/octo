package email

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"
)

// crlf turns a readable fixture into wire form.
func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.TrimLeft(s, "\n"), "\n", "\r\n"))
}

func TestParseMessage(t *testing.T) {
	cases := []struct {
		name  string
		raw   []byte
		check func(t *testing.T, got map[string]any)
	}{
		{
			name: "text only with encoded subject and threading headers",
			raw: crlf(`
From: =?utf-8?q?Jos=C3=A9?= <jose@example.com>
To: Ann <ann@example.com>, bob@example.com
Subject: =?utf-8?q?Caf=C3=A9_order?=
Date: Tue, 17 Nov 2026 10:00:00 +0100
Message-ID: <m1@example.com>
In-Reply-To: <m0@example.com>
References: <a@example.com> <m0@example.com>
Content-Type: text/plain; charset=utf-8

Two flat whites.
`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				expectEq(t, got[keySubject], "Café order")
				expectEq(t, got[keyMessageID], "m1@example.com")
				expectEq(t, got[keyInReplyTo], "m0@example.com")
				expectEq(t, len(got[keyReferences].([]any)), 2)
				expectEq(t, got[keyDate], "2026-11-17T09:00:00Z")
				from := got[keyFrom].(map[string]any)
				expectEq(t, from[keyName], "José")
				expectEq(t, from[keyAddress], "jose@example.com")
				expectEq(t, len(got[keyTo].([]any)), 2)
				expectEq(t, strings.TrimSpace(got[keyText].(string)), "Two flat whites.")
				expectEq(t, got[keyHTML], "")
				subjects := got[keyHeaders].(map[string]any)["Subject"].([]any)
				expectEq(t, subjects[0], "Café order")
			},
		},
		{
			name: "alternative with attachment",
			raw: crlf(`
From: a@example.com
To: b@example.com
Subject: report
Content-Type: multipart/mixed; boundary=outer

--outer
Content-Type: multipart/alternative; boundary=inner

--inner
Content-Type: text/plain; charset=utf-8

plain body
--inner
Content-Type: text/html; charset=utf-8

<p>html body</p>
--inner--
--outer
Content-Type: application/pdf
Content-Disposition: attachment; filename="q3.pdf"
Content-Transfer-Encoding: base64

AAEC
--outer--
`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				expectEq(t, strings.TrimSpace(got[keyText].(string)), "plain body")
				expectEq(t, strings.TrimSpace(got[keyHTML].(string)), "<p>html body</p>")
				atts := got[keyAttachments].([]any)
				if len(atts) != 1 {
					t.Fatalf("attachments = %v", atts)
				}
				a := atts[0].(map[string]any)
				expectEq(t, a[partFilename], "q3.pdf")
				expectEq(t, a[partContentType], "application/pdf")
				expectEq(t, a[partEncoding], encodingBase64)
				expectEq(t, a[partSize], 3)
				expectEq(t, a[partData], base64.StdEncoding.EncodeToString([]byte{0, 1, 2}))
			},
		},
		{
			name: "latin-1 body and malformed date",
			raw: crlf(`
From: a@example.com
Subject: hola
Date: not a date
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

ma=F1ana
`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				expectEq(t, strings.TrimSpace(got[keyText].(string)), "mañana")
				expectEq(t, got[keyDate], "")
				expectEq(t, len(got[keyTo].([]any)), 0)
			},
		},
		{
			name: "undecodable parts are skipped or kept raw, and a truncated part keeps what came before",
			raw: crlf(`
From: a@example.com
Subject: messy
Content-Type: multipart/mixed; boundary=b

--b
Content-Type: text/plain; charset=x-made-up

kept as written
--b
Content-Type: application/octet-stream
Content-Transfer-Encoding: x-uuencode-ish
Content-Disposition: attachment; filename="odd.bin"

????
--b
Content-Type: text/html; charset=utf-8

<p>still here</p>
--b
Content-Type: application/pdf
Content-Disposition: attachment; filename="cut.pdf"
Content-Transfer-Encoding: base64

AAEC`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				expectEq(t, got[keySubject], "messy")
				expectEq(t, strings.TrimSpace(got[keyText].(string)), "kept as written")
				expectEq(t, strings.TrimSpace(got[keyHTML].(string)), "<p>still here</p>")
				for _, a := range got[keyAttachments].([]any) {
					if a.(map[string]any)[partFilename] == "odd.bin" {
						t.Error("a part in an unknown transfer encoding should be skipped")
					}
				}
			},
		},
		{
			name: "an unknown root transfer encoding keeps the header",
			raw: crlf(`
From: a@example.com
Subject: odd encoding
Content-Type: text/plain
Content-Transfer-Encoding: x-uuencode-ish

????
`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				expectEq(t, got[keySubject], "odd encoding")
				expectEq(t, got[keyText], "")
				expectEq(t, len(got[keyAttachments].([]any)), 0)
			},
		},
		{
			name: "a text attachment converted to UTF-8 is labelled UTF-8",
			raw: crlf(`
From: a@example.com
Subject: latin
Content-Type: multipart/mixed; boundary=b

--b
Content-Type: text/plain; charset=utf-8

see attached
--b
Content-Type: text/plain; charset=iso-8859-1
Content-Disposition: attachment; filename="menu.txt"
Content-Transfer-Encoding: quoted-printable

caf=E9
--b--
`),
			check: func(t *testing.T, got map[string]any) {
				t.Helper()
				a := got[keyAttachments].([]any)[0].(map[string]any)
				expectEq(t, a[partContentType], "text/plain; charset=utf-8")
				data, _ := base64.StdEncoding.DecodeString(a[partData].(string))
				expectEq(t, strings.TrimSpace(string(data)), "café")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMessage(tc.raw)
			if err != nil {
				t.Fatalf("parseMessage: %v", err)
			}
			tc.check(t, got)
		})
	}
}

// TestParseReadsWhatSendWrites pins the round trip a forwarding flow relies on:
// an attachment parsed from one message composes into the next unchanged.
func TestParseReadsWhatSendWrites(t *testing.T) {
	var h mail.Header
	h.SetDate(time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC))
	h.SetAddressList("From", []*mail.Address{{Address: "a@example.com"}})
	h.SetSubject("fwd")
	att := attachment{filename: "data.bin", contentType: "application/octet-stream", data: []byte{0xff, 0x00, 0x10}}
	txt := attachment{filename: "menu.txt", contentType: "text/plain; charset=utf-8", data: []byte("café")}
	raw, err := render(h, mimeBody{text: "see attached"}, []attachment{att, txt})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	got, err := parseMessage(raw)
	if err != nil {
		t.Fatalf("parseMessage: %v", err)
	}
	parts := got[keyAttachments].([]any)
	if len(parts) != 2 {
		t.Fatalf("attachments = %v", parts)
	}
	back, err := toAttachment(parts[0])
	if err != nil {
		t.Fatalf("toAttachment: %v", err)
	}
	if back.filename != att.filename || string(back.data) != string(att.data) {
		t.Errorf("round trip = %+v, want %+v", back, att)
	}
	expectEq(t, strings.TrimSpace(got[keyText].(string)), "see attached")
	text, err := toAttachment(parts[1])
	if err != nil {
		t.Fatalf("toAttachment: %v", err)
	}
	expectEq(t, string(text.data), "café")
	// A UTF-8 text attachment keeps its charset label through the round trip.
	expectEq(t, parts[1].(map[string]any)[partContentType], "text/plain; charset=utf-8")
}

func expectEq[T comparable](t *testing.T, got any, want T) {
	t.Helper()
	if g, ok := got.(T); !ok || g != want {
		t.Errorf("got %#v, want %#v", got, want)
	}
}
