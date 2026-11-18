// This file turns a received message — the raw RFC 5322 bytes a server hands
// back — into the JSON shape a flow sees as its body. The imap and pop3 sources
// and fetch blocks all build their messages here, so a flow reads mail the same
// way whichever protocol delivered it.
//
// The shape:
//
//	messageId, inReplyTo  string
//	references            list of strings
//	date                  RFC 3339 string ("" when the header is missing or unreadable)
//	subject               string
//	from                  {name, address}, or null
//	to, cc, replyTo       lists of {name, address}
//	text, html            the first plain-text and HTML body parts
//	headers               every top-level header, name → list of values
//	attachments           parts carrying a filename, in the multipart part shape
package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-message"
	// Registers decoders for the non-UTF-8 charsets real mail still arrives in.
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

// The keys of a parsed message. Attachment keys are shared with smtp-send's
// attachment decoding (expressions.go).
const (
	keyMessageID   = "messageId"
	keyInReplyTo   = "inReplyTo"
	keyReferences  = "references"
	keyDate        = "date"
	keySubject     = "subject"
	keyFrom        = "from"
	keyTo          = "to"
	keyCc          = "cc"
	keyReplyTo     = "replyTo"
	keyText        = "text"
	keyHTML        = "html"
	keyHeaders     = "headers"
	keyAttachments = "attachments"
	keyName        = "name"
	keyAddress     = "address"
)

// parseMessage parses raw message bytes into the body shape. A message whose
// MIME structure is broken still yields its headers and every part read before
// the break; the error is returned only when not even the header can be read.
func parseMessage(raw []byte) (map[string]any, error) {
	entity, err := message.Read(bytes.NewReader(raw))
	if entity == nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	// A body in a transfer encoding go-message cannot decode still has a
	// readable header: keep the header, and skip the body it cannot read.
	if message.IsUnknownEncoding(err) {
		out := parseHeader(mail.Header{Header: entity.Header})
		out[keyText], out[keyHTML], out[keyAttachments] = "", "", []any{}
		return out, nil
	}
	r := mail.NewReader(entity)
	defer func() { _ = r.Close() }()

	out := parseHeader(r.Header)
	text, html, attachments := parseParts(r)
	out[keyText] = text
	out[keyHTML] = html
	out[keyAttachments] = attachments
	return out, nil
}

// parseHeader extracts the top-level header fields. A field that is present but
// malformed degrades to its empty value rather than failing the message: one
// sender's odd Date header should not stop a mailbox from being read.
func parseHeader(h mail.Header) map[string]any {
	out := map[string]any{}
	out[keyMessageID], _ = h.MessageID()
	inReplyTo, _ := h.MsgIDList("In-Reply-To")
	out[keyInReplyTo] = firstOr(inReplyTo, "")
	refs, _ := h.MsgIDList("References")
	out[keyReferences] = toAnyList(refs)
	out[keySubject], _ = h.Subject()

	out[keyDate] = ""
	if date, err := h.Date(); err == nil && !date.IsZero() {
		out[keyDate] = date.UTC().Format(time.RFC3339)
	}

	from := addressList(h, "From")
	out[keyFrom] = nil
	if len(from) > 0 {
		out[keyFrom] = from[0]
	}
	out[keyTo] = addressList(h, "To")
	out[keyCc] = addressList(h, "Cc")
	out[keyReplyTo] = addressList(h, "Reply-To")

	headers := map[string]any{}
	fields := h.Fields()
	for fields.Next() {
		key := textproto.CanonicalMIMEHeaderKey(fields.Key())
		value, err := fields.Text()
		if err != nil {
			value = fields.Value()
		}
		list, _ := headers[key].([]any)
		headers[key] = append(list, value)
	}
	out[keyHeaders] = headers
	return out
}

// addressList reads an address header as a list of {name, address}; a malformed
// list reads as empty.
func addressList(h mail.Header, key string) []any {
	list, err := h.AddressList(key)
	if err != nil {
		return []any{}
	}
	out := make([]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{keyName: a.Name, keyAddress: a.Address})
	}
	return out
}

// maxParts bounds the parts one message contributes, so a pathological message
// cannot keep the walk going indefinitely.
const maxParts = 256

// parseParts walks the body: the first text/plain and text/html inline parts
// become the bodies, and every part with a filename becomes an attachment —
// including an inline image that names one, since a flow wanting the file
// should not have to know how the sender's client chose to display it.
//
// The walk keeps what it can. A part in a charset go-message cannot decode
// keeps its undecoded bytes; a part in a transfer encoding it cannot decode is
// skipped, the walk carrying on past it; any other break — a truncated part, a
// malformed boundary — ends the walk with what was read before it.
func parseParts(r *mail.Reader) (text, html string, attachments []any) {
	attachments = []any{}
	for range maxParts {
		part, body, step := readPart(r)
		if step == partStop {
			break
		}
		if step == partSkip {
			continue
		}
		mediaType, params, filename := partInfo(part.Header)
		if filename != "" {
			contentType := attachmentType(mediaType, params, step == partTranscoded)
			attachments = append(attachments, attachmentPart(filename, contentType, body))
			continue
		}
		switch {
		case mediaType == textPlain && text == "":
			text = string(body)
		case mediaType == textHTML && html == "":
			html = string(body)
		}
	}
	return text, html, attachments
}

// partStep says what the walk does after readPart.
type partStep int

const (
	partRead partStep = iota
	// partTranscoded is a part go-message converted from its declared charset
	// to UTF-8. Its bytes are no longer in that charset, so an attachment
	// read this way is relabelled charset=utf-8 to keep label and bytes in
	// agreement.
	partTranscoded
	partSkip
	partStop
)

// readPart reads the next part and its bytes, deciding per the rules on
// parseParts whether the walk uses it, skips it, or ends.
func readPart(r *mail.Reader) (*mail.Part, []byte, partStep) {
	part, err := r.NextPart()
	switch {
	case errors.Is(err, io.EOF):
		return nil, nil, partStop
	case part == nil && message.IsUnknownEncoding(err):
		return nil, nil, partSkip
	case err != nil && !message.IsUnknownCharset(err):
		return nil, nil, partStop
	}
	undecodedCharset := message.IsUnknownCharset(err)
	body, err := io.ReadAll(part.Body)
	if err != nil {
		return nil, nil, partStop
	}
	if !undecodedCharset && transcodes(part.Header) {
		return part, body, partTranscoded
	}
	return part, body, partRead
}

// transcodes reports whether go-message converts a part with this header to
// UTF-8 as it reads it: a text part declaring a charset other than UTF-8 or
// its ASCII subset.
func transcodes(h mail.PartHeader) bool {
	typed, ok := h.(interface {
		ContentType() (string, map[string]string, error)
	})
	if !ok {
		return false
	}
	mediaType, params, err := typed.ContentType()
	if err != nil || !strings.HasPrefix(mediaType, "text/") {
		return false
	}
	switch strings.ToLower(params[charsetParam]) {
	case "", charsetUTF8, "us-ascii":
		return false
	}
	return true
}

// partInfo returns a part's media type, its Content-Type parameters, and the
// filename it carries, if any.
func partInfo(h mail.PartHeader) (mediaType string, params map[string]string, filename string) {
	switch ph := h.(type) {
	case *mail.InlineHeader:
		mediaType, params, _ = ph.ContentType()
		if _, disposition, err := ph.ContentDisposition(); err == nil {
			filename = disposition["filename"]
		}
		if filename == "" {
			filename = decodeWord(params["name"])
		}
	case *mail.AttachmentHeader:
		mediaType, params, _ = ph.ContentType()
		filename, _ = ph.Filename()
		if filename == "" {
			filename = "attachment"
		}
	}
	if mediaType == "" {
		mediaType = textPlain
	}
	return mediaType, params, filename
}

// attachmentType is the content type an attachment keeps: its media type and
// parameters — a text file's charset matters to whoever opens it — except name,
// which the filename already carries. A part go-message transcoded is relabelled
// charset=utf-8, since that is what its bytes now are.
func attachmentType(mediaType string, params map[string]string, transcoded bool) string {
	kept := map[string]string{}
	for k, v := range params {
		if k != "name" {
			kept[k] = v
		}
	}
	if transcoded {
		kept[charsetParam] = charsetUTF8
	}
	if formatted := mime.FormatMediaType(mediaType, kept); formatted != "" {
		return formatted
	}
	return mediaType
}

// attachmentPart builds an attachment in the multipart part shape, its bytes
// base64 encoded so they survive the message's JSON round trips.
func attachmentPart(filename, contentType string, body []byte) map[string]any {
	return map[string]any{
		partFilename:    filename,
		partContentType: contentType,
		partEncoding:    encodingBase64,
		partSize:        len(body),
		partData:        base64.StdEncoding.EncodeToString(body),
	}
}

var wordDecoder mime.WordDecoder

// decodeWord decodes an RFC 2047 encoded word, returning s unchanged when it is
// not one.
func decodeWord(s string) string {
	decoded, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

func firstOr(list []string, fallback string) string {
	if len(list) == 0 {
		return fallback
	}
	return list[0]
}

func toAnyList(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}
