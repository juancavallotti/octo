// This file holds the CEL plumbing the email blocks share: compiling a block's
// expressions with the first error kept, and evaluating them into the shapes an
// email needs — text, address lists, and attachments.
package email

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/emersion/go-message/mail"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
)

// Attachment keys, matching the part shape of the multipart CEL functions.
const (
	partFilename    = "filename"
	partContentType = "contentType"
	partEncoding    = "encoding"
	partSize        = "size"
	partData        = "data"
	encodingBase64  = "base64"
	encodingText    = "text"
	// defaultAttachmentType is used for an attachment that names no content type.
	defaultAttachmentType = "application/octet-stream"
)

// compiler compiles a block's expressions, keeping the first error so a factory
// can compile every field and check once.
type compiler struct {
	res   core.ResourceLoader
	block string
	err   error
}

// required compiles src, recording an error when it is empty.
func (c *compiler) required(field, src string) *expr.Program {
	if c.err != nil {
		return nil
	}
	if strings.TrimSpace(src) == "" {
		c.err = fmt.Errorf("%s requires a %q expression", c.block, field)
		return nil
	}
	return c.optional(field, src)
}

// optional compiles src, returning nil for an empty source so the caller can
// treat unset as skipped.
func (c *compiler) optional(field, src string) *expr.Program {
	if c.err != nil || strings.TrimSpace(src) == "" {
		return nil
	}
	program, err := expr.CompileMessage(c.res, src)
	if err != nil {
		c.err = fmt.Errorf("%s: compile %s: %w", c.block, field, err)
		return nil
	}
	return program
}

// evaluator evaluates expressions against one activation, keeping the first
// error. A nil program evaluates to the zero value.
type evaluator struct {
	act map[string]any
	err error
}

func (e *evaluator) eval(field string, p *expr.Program) any {
	if e.err != nil || p == nil {
		return nil
	}
	v, err := p.Eval(e.act)
	if err != nil {
		e.err = fmt.Errorf("evaluate %s: %w", field, err)
		return nil
	}
	return v
}

// text evaluates to a string; null is the empty string.
func (e *evaluator) text(field string, p *expr.Program) string {
	switch v := e.eval(field, p).(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// addresses evaluates to an address list. A string is parsed as an RFC 5322
// address list ("a@x, B <b@y>"); a list contributes each element the same way.
func (e *evaluator) addresses(field string, p *expr.Program) []*mail.Address {
	value := e.eval(field, p)
	if e.err != nil || value == nil {
		return nil
	}
	var sources []string
	switch v := value.(type) {
	case string:
		sources = []string{v}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				e.err = fmt.Errorf("%s: list elements must be strings, got %T", field, item)
				return nil
			}
			sources = append(sources, s)
		}
	default:
		e.err = fmt.Errorf("%s must be a string or a list of strings, got %T", field, value)
		return nil
	}

	var out []*mail.Address
	for _, s := range sources {
		if strings.TrimSpace(s) == "" {
			continue
		}
		list, err := mail.ParseAddressList(s)
		if err != nil {
			e.err = fmt.Errorf("%s: parse %q: %w", field, s, err)
			return nil
		}
		out = append(out, list...)
	}
	return out
}

// attachment is one file to attach, its bytes decoded.
type attachment struct {
	filename    string
	contentType string
	data        []byte
}

// attachments evaluates to a list of attachments in the multipart part shape.
func (e *evaluator) attachments(p *expr.Program) []attachment {
	value := e.eval("attachments", p)
	if e.err != nil || value == nil {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		e.err = fmt.Errorf("attachments must be a list, got %T", value)
		return nil
	}
	out := make([]attachment, 0, len(list))
	for i, item := range list {
		a, err := toAttachment(item)
		if err != nil {
			e.err = fmt.Errorf("attachments[%d]: %w", i, err)
			return nil
		}
		out = append(out, a)
	}
	return out
}

// toAttachment decodes one part-shaped map.
func toAttachment(item any) (attachment, error) {
	part, ok := item.(map[string]any)
	if !ok {
		return attachment{}, fmt.Errorf("must be a map, got %T", item)
	}
	filename, _ := part[partFilename].(string)
	if strings.TrimSpace(filename) == "" {
		return attachment{}, fmt.Errorf("requires a %q", partFilename)
	}
	contentType, _ := part[partContentType].(string)
	if contentType == "" {
		contentType = defaultAttachmentType
	}
	data, _ := part[partData].(string)
	encoding, _ := part[partEncoding].(string)

	a := attachment{filename: filename, contentType: contentType}
	switch encoding {
	case encodingText:
		a.data = []byte(data)
	case encodingBase64, "":
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return attachment{}, fmt.Errorf("%q: decode base64 data: %w", filename, err)
		}
		a.data = decoded
	default:
		return attachment{}, fmt.Errorf("%q: unknown encoding %q (want base64 or text)", filename, encoding)
	}
	return a, nil
}
