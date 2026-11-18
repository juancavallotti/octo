// This file provides the "smtp-send" block: it composes a message from CEL
// expressions evaluated per flow message and submits it through an smtp
// connector. The flow message passes through unchanged; the sent message's
// Message-ID is folded into a variable so a later step can thread a reply or
// record what was sent.
package email

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	smtpSendType = "smtp-send"
	// messageIDVar names the variable the sent message's Message-ID lands in.
	messageIDVar = "emailMessageId"
	// textPlain and textHTML are the media types of the two body alternatives.
	textPlain = "text/plain"
	textHTML  = "text/html"
)

func registerSMTPSend() {
	core.MustRegisterBlock(smtpSendType, newSMTPSend)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        smtpSendType,
		Label:       "Send Email",
		Category:    core.CategoryProcessor,
		Icon:        iconSend,
		Description: "Compose an email and send it through an smtp connector.",
		Config:      reflect.TypeFor[sendSettings](),
	})
}

// sendSettings is the smtp-send block's configuration. Every field but the
// connector is a CEL expression evaluated per message.
type sendSettings struct {
	// Name of the smtp connector to send through.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:smtp"`
	// CEL expression for the sender address, e.g. "Ops <ops@example.com>".
	From string `json:"from" octo:"label=From,type=cel,required"`
	// CEL expression for the recipients: an address list string or a list of addresses.
	To string `json:"to" octo:"label=To,type=cel,required"`
	// CEL expression for carbon-copy recipients.
	Cc string `json:"cc" octo:"label=Cc,type=cel"`
	// CEL expression for blind-copy recipients; they receive the message but
	// appear in no header.
	Bcc string `json:"bcc" octo:"label=Bcc,type=cel"`
	// CEL expression for the Reply-To address.
	ReplyTo string `json:"replyTo" octo:"label=Reply-To,type=cel"`
	// CEL expression for the subject line.
	Subject string `json:"subject" octo:"label=Subject,type=cel"`
	// CEL expression for the plain-text body (text or html is required).
	Text string `json:"text" octo:"label=Text body,type=cel"`
	// CEL expression for the HTML body; sent as an alternative to text when both are set.
	HTML string `json:"html" octo:"label=HTML body,type=cel"`
	// CEL expression for a list of attachments, each {filename, contentType,
	// data, encoding}: data is base64 unless encoding is "text". A received
	// email's attachments fit as they are.
	Attachments string `json:"attachments" octo:"label=Attachments,type=cel"`
}

// sendProcessor composes and sends one message per flow message.
type sendProcessor struct {
	conn        *SMTPConnector
	from        *expr.Program
	to          *expr.Program
	cc          *expr.Program
	bcc         *expr.Program
	replyTo     *expr.Program
	subject     *expr.Program
	text        *expr.Program
	html        *expr.Program
	attachments *expr.Program
	env         expr.Env
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSMTPSend(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg sendSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector[*SMTPConnector](smtpSendType, cfg.Connector, deps)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Text) == "" && strings.TrimSpace(cfg.HTML) == "" {
		return nil, errors.New("smtp-send requires \"text\" or \"html\"")
	}

	p := &sendProcessor{conn: conn, env: expr.EnvActivation(deps.Env)}
	c := compiler{res: deps.Resources, block: smtpSendType}
	p.from = c.required("from", cfg.From)
	p.to = c.required("to", cfg.To)
	p.cc = c.optional("cc", cfg.Cc)
	p.bcc = c.optional("bcc", cfg.Bcc)
	p.replyTo = c.optional("replyTo", cfg.ReplyTo)
	p.subject = c.optional("subject", cfg.Subject)
	p.text = c.optional("text", cfg.Text)
	p.html = c.optional("html", cfg.HTML)
	p.attachments = c.optional("attachments", cfg.Attachments)
	if c.err != nil {
		return nil, c.err
	}
	return p, nil
}

// Process composes the message, submits it, and records its Message-ID.
func (p *sendProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	out, err := p.compose(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, err
	}
	if err := p.conn.Send(ctx, out.from, out.recipients, out.data); err != nil {
		return nil, fmt.Errorf("smtp-send: %w", err)
	}
	msg.Variables.Set(messageIDVar, out.messageID)
	return msg, nil
}

// outgoing is a composed message ready for the SMTP envelope.
type outgoing struct {
	from       string
	recipients []string
	messageID  string
	data       []byte
}

// compose evaluates the block's expressions and renders the MIME message.
func (p *sendProcessor) compose(act map[string]any) (outgoing, error) {
	e := evaluator{act: act}
	from := e.addresses("from", p.from)
	to := e.addresses("to", p.to)
	cc := e.addresses("cc", p.cc)
	bcc := e.addresses("bcc", p.bcc)
	replyTo := e.addresses("replyTo", p.replyTo)
	subject := e.text("subject", p.subject)
	body := mimeBody{text: e.text("text", p.text), html: e.text("html", p.html)}
	attachments := e.attachments(p.attachments)
	if e.err != nil {
		return outgoing{}, e.err
	}
	if len(from) != 1 {
		return outgoing{}, fmt.Errorf("smtp-send: from must be exactly one address, got %d", len(from))
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		return outgoing{}, errors.New("smtp-send: no recipients")
	}

	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", from)
	h.SetAddressList("To", to)
	if len(cc) > 0 {
		h.SetAddressList("Cc", cc)
	}
	if len(replyTo) > 0 {
		h.SetAddressList("Reply-To", replyTo)
	}
	h.SetSubject(subject)
	if err := h.GenerateMessageID(); err != nil {
		return outgoing{}, fmt.Errorf("smtp-send: message id: %w", err)
	}
	messageID, _ := h.MessageID()

	data, err := render(h, body, attachments)
	if err != nil {
		return outgoing{}, fmt.Errorf("smtp-send: %w", err)
	}

	recipients := make([]string, 0, len(to)+len(cc)+len(bcc))
	for _, list := range [][]*mail.Address{to, cc, bcc} {
		for _, a := range list {
			recipients = append(recipients, a.Address)
		}
	}
	return outgoing{from: from[0].Address, recipients: recipients, messageID: messageID, data: data}, nil
}

// mimeBody is the text and HTML alternatives of a message; either may be empty.
type mimeBody struct {
	text string
	html string
}

// render writes the message: a single inline part when there is one body and no
// attachment, multipart/alternative for text and HTML, and multipart/mixed
// around those when attachments are present.
func render(h mail.Header, body mimeBody, attachments []attachment) ([]byte, error) {
	var buf bytes.Buffer
	if len(attachments) == 0 && (body.text == "" || body.html == "") {
		mediaType, content := textPlain, body.text
		if body.text == "" {
			mediaType, content = textHTML, body.html
		}
		h.SetContentType(mediaType, map[string]string{"charset": "utf-8"})
		w, err := mail.CreateSingleInlineWriter(&buf, h)
		if err != nil {
			return nil, fmt.Errorf("compose: %w", err)
		}
		if err := writeAndClose(w, []byte(content)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	if len(attachments) == 0 {
		w, err := mail.CreateInlineWriter(&buf, h)
		if err != nil {
			return nil, fmt.Errorf("compose: %w", err)
		}
		if err := writeAlternatives(w, body); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	if err := renderMixed(&buf, h, body, attachments); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderMixed writes a multipart/mixed message: the body alternatives followed
// by one part per attachment.
func renderMixed(buf *bytes.Buffer, h mail.Header, body mimeBody, attachments []attachment) error {
	w, err := mail.CreateWriter(buf, h)
	if err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	inline, err := w.CreateInline()
	if err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	if err := writeAlternatives(inline, body); err != nil {
		return err
	}
	for _, a := range attachments {
		var ah mail.AttachmentHeader
		ah.SetContentType(a.contentType, nil)
		ah.SetFilename(a.filename)
		part, err := w.CreateAttachment(ah)
		if err != nil {
			return fmt.Errorf("compose attachment %q: %w", a.filename, err)
		}
		if err := writeAndClose(part, a.data); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	return nil
}

// writeAlternatives writes the non-empty body alternatives, text first so a
// client that understands HTML picks the later part, and closes the writer.
func writeAlternatives(w *mail.InlineWriter, body mimeBody) error {
	for _, alt := range []struct{ mediaType, content string }{{textPlain, body.text}, {textHTML, body.html}} {
		if alt.content == "" {
			continue
		}
		var ph mail.InlineHeader
		ph.SetContentType(alt.mediaType, map[string]string{"charset": "utf-8"})
		part, err := w.CreatePart(ph)
		if err != nil {
			return fmt.Errorf("compose %s part: %w", alt.mediaType, err)
		}
		if err := writeAndClose(part, []byte(alt.content)); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	return nil
}

func writeAndClose(w io.WriteCloser, data []byte) error {
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return fmt.Errorf("compose: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("compose: %w", err)
	}
	return nil
}
