// Multipart wire codec: the decode/encode pair behind the multipart CEL
// functions. It is exported because the same conversion is needed at the edges of
// a flow — the http source decoding an upload, the rest block sending one — and
// not only inside an expression.
package expr

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/textproto"
	"slices"
	"strings"
)

// The keys one decoded part carries. name and filename come from the part's
// Content-Disposition; contentType comes from the part's own Content-Type header,
// which is what distinguishes an uploaded PNG from an uploaded CSV independently
// of the request's outer content type. headers holds whatever else the part
// declared, so an unusual part header is reachable rather than silently dropped.
const (
	partNameKey        = "name"
	partFilenameKey    = "filename"
	partContentTypeKey = "contentType"
	partEncodingKey    = "encoding"
	partSizeKey        = "size"
	partDataKey        = "data"
	partHeadersKey     = "headers"
)

// How a part's bytes are carried in data. A part with a filename is treated as
// binary and is always base64; a plain form field is always text. The rule keys
// off the request's *shape* rather than its bytes, so an expression reading a part
// means the same thing on every request rather than flipping with the payload.
//
// Base64 is not decoration. rawData is a UTF-8 string, and a message that crosses
// a queue or lands in a trace is JSON-encoded, which replaces invalid UTF-8 with
// U+FFFD — so unencoded binary would corrupt silently. connectors/file/read.go
// carries the same rule for the same reason.
const (
	encodingText   = "text"
	encodingBase64 = "base64"
)

// multipartMediaType is the media type the http source decodes automatically.
const multipartMediaType = "multipart/form-data"

// maxMultipartParts bounds how many parts one payload may contribute. The body is
// already size-limited upstream, but a payload of many tiny parts turns into a map
// entry each, so the count is bounded on its own.
const maxMultipartParts = 256

// defaultMultipartBoundary is the boundary toMultipart uses when the caller names
// none. It is fixed rather than random because set-payload takes its contentType
// as static config, not CEL, so a flow serving a multipart body has to be able to
// write the matching boundary into that field by hand.
const defaultMultipartBoundary = "octo-multipart"

// quoteEscaper escapes a value going into a quoted Content-Disposition parameter.
// mime/multipart keeps its own copy of this unexported, and CreatePart takes a
// header we build ourselves, so we need one too.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, `\"`)

// errNoBoundary reports a multipart content type that names no boundary, which
// makes the payload undecodable however well-formed the rest of it is.
var errNoBoundary = errors.New("content type names no boundary")

// errCRLF marks a value that would break out of the header it is about to be
// written into. A part name or filename reaching the encoder is routinely
// attacker-controlled -- a flow that forwards body.parts is forwarding names
// chosen by whoever uploaded them -- and CR or LF in one turns a single part into
// several, with headers and a body of the attacker's choosing.
var errCRLF = errors.New("must not contain CR or LF")

// errNoDelimiter reports a payload that declares a boundary and then never uses
// it. mime/multipart reports that as a clean EOF, indistinguishable from a form
// with no fields, so without this check a body of unrelated bytes decodes to an
// empty parts map and the mismatch surfaces much later as a missing part.
var errNoDelimiter = errors.New("body contains no boundary delimiter")

// IsMultipart reports whether a content type names a multipart/form-data payload.
// It is what the http source gates on, so the decision lives here beside the codec
// rather than as a string comparison at the call site.
func IsMultipart(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == multipartMediaType
}

// DecodeMultipart decodes a multipart payload into the parts map: part name to
// decoded part, with a repeated name collecting into a list. That map is the one
// currency the multipart functions, the http source and the rest block all share.
func DecodeMultipart(rawData, contentType string) (map[string]any, error) {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, fmt.Errorf("parse content type: %w", err)
	}
	boundary := params["boundary"]
	if boundary == "" {
		return nil, errNoBoundary
	}

	// A genuinely empty form still writes its closing delimiter, so requiring the
	// delimiter rejects garbage without rejecting a form that carried no fields.
	if !strings.Contains(rawData, "--"+boundary) {
		return nil, errNoDelimiter
	}

	reader := multipart.NewReader(strings.NewReader(rawData), boundary)
	parts := make(map[string]any)
	for count := 0; ; count++ {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return parts, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read part: %w", err)
		}
		if count >= maxMultipartParts {
			_ = part.Close()
			return nil, fmt.Errorf("payload carries more than %d parts", maxMultipartParts)
		}
		name := part.FormName()
		decoded, err := decodePart(part)
		_ = part.Close()
		if err != nil {
			return nil, err
		}
		addNamed(parts, name, decoded)
	}
}

// decodePart reads one part into the decoded shape. Its encoding follows the
// filename rule above, and size is always the *decoded* byte length so it means
// the same thing whichever encoding the part ended up with.
func decodePart(part *multipart.Part) (map[string]any, error) {
	data, err := io.ReadAll(part)
	if err != nil {
		return nil, fmt.Errorf("read part %q: %w", part.FormName(), err)
	}
	filename := part.FileName()
	encoding := encodingText
	if filename != "" {
		encoding = encodingBase64
	}
	return map[string]any{
		partNameKey:        part.FormName(),
		partFilenameKey:    filename,
		partContentTypeKey: part.Header.Get("Content-Type"),
		partEncodingKey:    encoding,
		partSizeKey:        len(data),
		partDataKey:        encodePartData(data, encoding),
		partHeadersKey:     residualHeaders(part.Header),
	}, nil
}

// residualHeaders copies the part's headers minus the two whose contents are
// already promoted to their own keys, so nothing a part declared is lost without
// duplicating what the caller can read directly.
func residualHeaders(header textproto.MIMEHeader) map[string]any {
	out := make(map[string]any, len(header))
	for name, values := range header {
		if name == "Content-Disposition" || name == "Content-Type" {
			continue
		}
		if len(values) > 0 {
			out[name] = values[0]
		}
	}
	return out
}

// addNamed inserts a part under its name, collecting a repeated name into a list.
// It is the same single-vs-repeated rule fromFormData uses, so the two decoders
// stay predictable against each other.
func addNamed(parts map[string]any, name string, part any) {
	existing, ok := parts[name]
	if !ok {
		parts[name] = part
		return
	}
	if list, ok := existing.([]any); ok {
		parts[name] = append(list, part)
		return
	}
	parts[name] = []any{existing, part}
}

// encodePartData renders bytes for the data field under the given encoding.
func encodePartData(data []byte, encoding string) string {
	if encoding == encodingBase64 {
		return base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}

// decodePartData is encodePartData's inverse, turning a part's data field back
// into the bytes that go on the wire.
func decodePartData(data, encoding string) ([]byte, error) {
	if encoding != encodingBase64 {
		return []byte(data), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode base64 part data: %w", err)
	}
	return decoded, nil
}

// EncodeMultipart renders a parts map as a multipart body with the given
// boundary. Part names are written in sorted order, matching toFormData's
// deterministic output — which is what makes a rendered body assertable in a test
// rather than only comparable after re-parsing.
func EncodeMultipart(parts map[string]any, boundary string) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	if err := writer.SetBoundary(boundary); err != nil {
		return "", fmt.Errorf("set boundary: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(parts)) {
		for _, raw := range partList(parts[name]) {
			if err := writePart(writer, name, raw); err != nil {
				return "", err
			}
		}
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}
	return buf.String(), nil
}

// partList normalizes one map entry to the parts stored under it, so a repeated
// name written as a list and a single part take the same path out.
func partList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}

// writePart renders one part. The header is built by hand rather than through
// CreateFormFile so a part keeps its own content type and any residual headers it
// was decoded with, which is what lets a decoded upload be forwarded verbatim.
func writePart(writer *multipart.Writer, name string, raw any) error {
	part, err := NormalizePart(name, raw)
	if err != nil {
		return err
	}
	header, err := partHeader(name, part)
	if err != nil {
		return err
	}

	payload, _ := part[partDataKey].(string)
	encoding, _ := part[partEncodingKey].(string)
	data, err := decodePartData(payload, encoding)
	if err != nil {
		return err
	}
	target, err := writer.CreatePart(header)
	if err != nil {
		return fmt.Errorf("create part %q: %w", name, err)
	}
	if _, err := target.Write(data); err != nil {
		return fmt.Errorf("write part %q: %w", name, err)
	}
	return nil
}

// partHeader builds one part's MIME header.
//
// Order matters: the residual headers go on FIRST and the two promoted ones
// last, so a part carrying its own "Content-Disposition" cannot displace the
// name and filename this part is actually being written under. residualHeaders
// already excludes both on the way in, so a decoded part never takes this path —
// it exists for a part built by hand.
func partHeader(name string, part map[string]any) (textproto.MIMEHeader, error) {
	filename, _ := part[partFilenameKey].(string)
	contentType, _ := part[partContentTypeKey].(string)

	header := make(textproto.MIMEHeader)
	if extra, ok := part[partHeadersKey].(map[string]any); ok {
		for key, value := range extra {
			rendered := fmt.Sprint(value)
			// Reject before canonicalizing: CanonicalMIMEHeaderKey returns a key
			// holding invalid bytes unchanged, so a CRLF-bearing key would survive
			// the round trip and reach the wire.
			if err := rejectCRLF(name, "header "+key, key); err != nil {
				return nil, err
			}
			if err := rejectCRLF(name, "header "+key+" value", rendered); err != nil {
				return nil, err
			}
			// Compare in canonical form, because Set writes in canonical form. A
			// raw-key comparison lets "content-type" past the guard and then stores
			// it as "Content-Type" -- the promoted header, set through a side door.
			canonical := textproto.CanonicalMIMEHeaderKey(key)
			if canonical == "Content-Disposition" || canonical == "Content-Type" {
				continue
			}
			header.Set(canonical, rendered)
		}
	}
	header.Set("Content-Disposition", contentDisposition(name, filename))
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return header, nil
}

// rejectCRLF fails a value that would terminate the header line it is written
// into. %q renders CR and LF as escapes, so the error can name the offending
// part without the value escaping into a log line the same way.
func rejectCRLF(name, field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("part %q: %s %w", name, field, errCRLF)
	}
	return nil
}

// contentDisposition builds the form-data disposition for a part, adding the
// filename only when there is one — a field and a file differ by exactly that.
func contentDisposition(name, filename string) string {
	disposition := fmt.Sprintf(`form-data; name="%s"`, quoteEscaper.Replace(name))
	if filename == "" {
		return disposition
	}
	return disposition + fmt.Sprintf(`; filename="%s"`, quoteEscaper.Replace(filename))
}

// NormalizePart fills a part written by hand out to the full decoded shape: a
// scalar is shorthand for a text field, and an object may name only the keys it
// cares about. Both the builder and the encoder go through it, so a part built in
// CEL and a part decoded off the wire are indistinguishable downstream.
func NormalizePart(name string, raw any) (map[string]any, error) {
	if err := rejectCRLF(name, "name", name); err != nil {
		return nil, err
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return scalarPart(name, raw), nil
	}

	data, ok := stringField(fields, partDataKey)
	if !ok {
		return nil, fmt.Errorf("part %q: data must be a string", name)
	}
	encoding, ok := stringField(fields, partEncodingKey)
	if !ok || encoding == "" {
		encoding = encodingText
	}
	if encoding != encodingText && encoding != encodingBase64 {
		return nil, fmt.Errorf("part %q: encoding must be %q or %q, got %q",
			name, encodingText, encodingBase64, encoding)
	}
	filename, _ := stringField(fields, partFilenameKey)
	if err := rejectCRLF(name, "filename", filename); err != nil {
		return nil, err
	}
	contentType, _ := stringField(fields, partContentTypeKey)
	if err := rejectCRLF(name, "contentType", contentType); err != nil {
		return nil, err
	}
	headers, _ := fields[partHeadersKey].(map[string]any)

	return map[string]any{
		partNameKey:        name,
		partFilenameKey:    filename,
		partContentTypeKey: contentType,
		partEncodingKey:    encoding,
		partSizeKey:        decodedSize(data, encoding),
		partDataKey:        data,
		partHeadersKey:     headers,
	}, nil
}

// scalarPart renders a bare value as a text field, the shorthand that keeps
// addPart("caption", body.caption) from needing an object literal.
func scalarPart(name string, raw any) map[string]any {
	data := ""
	if raw != nil {
		data = fmt.Sprint(raw)
	}
	return map[string]any{
		partNameKey:        name,
		partFilenameKey:    "",
		partContentTypeKey: "",
		partEncodingKey:    encodingText,
		partSizeKey:        len(data),
		partDataKey:        data,
		partHeadersKey:     map[string]any{},
	}
}

// stringField reads a string-valued key, reporting whether it was present and of
// the right type so a caller can tell "absent" from "wrong".
func stringField(fields map[string]any, key string) (string, bool) {
	value, ok := fields[key]
	if !ok {
		return "", false
	}
	s, ok := value.(string)
	return s, ok
}

// decodedSize reports the byte length the part's data represents, so size means
// the same thing whether the data arrived encoded or not. A base64 value that does
// not decode is reported at its literal length; NormalizePart does not reject it
// here because the encoder surfaces the real error when it writes the part.
func decodedSize(data, encoding string) int {
	if encoding != encodingBase64 {
		return len(data)
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return len(data)
	}
	return len(decoded)
}
