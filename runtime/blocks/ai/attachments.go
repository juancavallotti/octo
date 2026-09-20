// Attachments: reading the non-text content an AI block sends, out of the CEL
// expression that names it.
//
// The expression yields a list of maps because that is the only shape bytes can
// take through CEL. Program.Eval converts its result through structpb.Value,
// which has no bytes type at all, so `data` is a base64 string on the way in and
// becomes []byte here. This file is that boundary, and it is the only place in
// the AI package that knows the encoding.
package ai

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
)

// The keys one attachment is written with, in both directions: the list a block
// reads its attachments out of, and the list it writes the model's own files to.
// One set, so what the runtime writes can be fed straight back into what it
// reads.
const (
	attachmentMimeType = "mimeType"
	attachmentData     = "data"
	attachmentName     = "name"
	// attachmentSize is written but never read: it lets a flow decide what to do
	// with a file — store it, refuse it — without decoding the base64 first.
	attachmentSize = "size"
)

// mediaFields renders one file the model produced, in the shape a message
// variable carries it: JSON-native, with the bytes base64 because a variable is
// copied, traced and read from CEL, none of which can hold a []byte.
func mediaFields(file core.LLMAttachment) map[string]any {
	return map[string]any{
		attachmentName:     file.Name,
		attachmentMimeType: file.MimeType,
		attachmentSize:     len(file.Data),
		attachmentData:     base64.StdEncoding.EncodeToString(file.Data),
	}
}

// evalAttachments evaluates an attachments expression into the DTOs the
// providers take. A nil program is a setting nobody set, and yields nothing.
//
// Every failure is an error naming the offending entry rather than a skipped
// one. An attachment that quietly does not arrive produces the worst outcome
// available here: the model answers about a file it never saw, and the answer
// does not say so.
func evalAttachments(program *expr.Program, activation map[string]any) ([]core.LLMAttachment, error) {
	if program == nil {
		return nil, nil
	}
	value, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	// Absent rather than empty: an expression guarded with has() returns null for a
	// message that carries no attachments, which is the ordinary case and not a
	// mistake.
	if value == nil {
		return nil, nil
	}
	entries, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expression produced %T, want a list of attachments", value)
	}
	out := make([]core.LLMAttachment, 0, len(entries))
	for i, entry := range entries {
		attachment, err := decodeAttachment(entry, i)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// decodeAttachment reads one entry of the list.
func decodeAttachment(entry any, i int) (core.LLMAttachment, error) {
	fields, ok := entry.(map[string]any)
	if !ok {
		return core.LLMAttachment{}, fmt.Errorf("attachment %d is %T, want a map", i, entry)
	}
	raw, ok := fields[attachmentData].(string)
	if !ok {
		return core.LLMAttachment{}, fmt.Errorf(
			"attachment %d: %s must be a base64 string", i, attachmentData)
	}
	mimeType, _ := fields[attachmentMimeType].(string)
	// A data URL states its own type, and it is the shape a browser hands out, so
	// one is unwrapped here rather than in every flow's CEL. Its type wins over a
	// stated one only when nothing was stated: an author who wrote both meant the
	// one they wrote.
	payload, urlMime := splitDataURL(raw)
	if mimeType == "" {
		mimeType = urlMime
	}
	if mimeType == "" {
		return core.LLMAttachment{}, fmt.Errorf(
			"attachment %d: %s is required, and the data is not a data: URL that states one",
			i, attachmentMimeType)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return core.LLMAttachment{}, fmt.Errorf("attachment %d: decoding %s: %w", i, attachmentData, err)
	}
	name, _ := fields[attachmentName].(string)
	return core.LLMAttachment{MimeType: mimeType, Data: data, Name: name}, nil
}

// splitDataURL separates a `data:<mime>;base64,<payload>` URL into its payload
// and the type it states. Anything else is returned unchanged with no type,
// because a bare base64 string is the other half of what this accepts.
func splitDataURL(raw string) (payload, mimeType string) {
	const scheme = "data:"
	const marker = ";base64,"
	if !strings.HasPrefix(raw, scheme) {
		return raw, ""
	}
	at := strings.Index(raw, marker)
	if at < 0 {
		return raw, ""
	}
	return raw[at+len(marker):], raw[len(scheme):at]
}
