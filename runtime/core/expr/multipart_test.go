package expr

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// A two-part payload: one plain field and one file whose bytes are deliberately
// not valid UTF-8, which is the case the base64 rule exists for.
const (
	testBoundary    = "testboundary"
	testFileName    = "photo.png"
	testFileType    = "image/png"
	testContentType = "multipart/form-data; boundary=" + testBoundary
)

// testFileBytes starts with the PNG magic number, whose 0x89 byte is invalid
// UTF-8 on its own. A part carrying it must come back base64 or the round trip
// through JSON that any queue hop performs would replace it with U+FFFD.
var testFileBytes = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0xFF, 0x00}

func multipartBody() string {
	return "--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"username\"\r\n\r\n" +
		"ann\r\n" +
		"--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"avatar\"; filename=\"" + testFileName + "\"\r\n" +
		"Content-Type: " + testFileType + "\r\n\r\n" +
		string(testFileBytes) + "\r\n" +
		"--" + testBoundary + "--\r\n"
}

// rawBody is the raw-content body shape a source puts on a message.
func rawBody() map[string]any {
	return map[string]any{
		"contentType": testContentType,
		"rawData":     multipartBody(),
	}
}

// evalWith compiles an expression and evaluates it against the given body.
func evalWith(t *testing.T, expression string, body any) any {
	t.Helper()
	prog, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	out, err := prog.Eval(messageActivation(body))
	if err != nil {
		t.Fatalf("eval %q: %v", expression, err)
	}
	return out
}

func evalStringWith(t *testing.T, expression string, body any) string {
	t.Helper()
	out, ok := evalWith(t, expression, body).(string)
	if !ok {
		t.Fatalf("eval %q did not return a string", expression)
	}
	return out
}

func TestFromMultipartDecodesFields(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"text field data", `fromMultipart(body).username.data`, "ann"},
		{"text field encoding", `fromMultipart(body).username.encoding`, encodingText},
		{"text field has no filename", `fromMultipart(body).username.filename`, ""},
		{"file filename", `fromMultipart(body).avatar.filename`, testFileName},
		{"file keeps its own content type", `fromMultipart(body).avatar.contentType`, testFileType},
		{"file encoding", `fromMultipart(body).avatar.encoding`, encodingBase64},
		{"file data is base64", `fromMultipart(body).avatar.data`,
			base64.StdEncoding.EncodeToString(testFileBytes)},
		{"name is carried", `fromMultipart(body).avatar.name`, "avatar"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalStringWith(t, tc.expr, rawBody()); got != tc.want {
				t.Errorf("%s = %q, want %q", tc.expr, got, tc.want)
			}
		})
	}
}

func TestFromMultipartSizeIsDecodedLength(t *testing.T) {
	// size must mean the same thing regardless of encoding, so the base64 part
	// reports its decoded length rather than the length of its encoding. The
	// comparison is done in CEL because that is where a flow author does it, and
	// it is indifferent to which numeric kind the bridge hands back.
	if got := evalWith(t, `fromMultipart(body).avatar.size == 10`, rawBody()); got != true {
		raw := evalWith(t, `fromMultipart(body).avatar.size`, rawBody())
		t.Errorf("avatar.size = %v (%T), want %d", raw, raw, len(testFileBytes))
	}
	if got := evalWith(t, `fromMultipart(body).username.size == 3`, rawBody()); got != true {
		t.Error("username.size is not the length of its text data")
	}
}

func TestFromMultipartExplicitContentType(t *testing.T) {
	got := evalStringWith(t,
		`fromMultipart(body.rawData, "`+testContentType+`").username.data`, rawBody())
	if got != "ann" {
		t.Errorf("data = %q, want ann", got)
	}
}

func TestFromMultipartRepeatedNameBecomesAList(t *testing.T) {
	body := "--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"tag\"\r\n\r\na\r\n" +
		"--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"tag\"\r\n\r\nb\r\n" +
		"--" + testBoundary + "--\r\n"
	raw := map[string]any{"contentType": testContentType, "rawData": body}

	if got := evalStringWith(t, `fromMultipart(body).tag[0].data`, raw); got != "a" {
		t.Errorf("tag[0].data = %q, want a", got)
	}
	if got := evalStringWith(t, `fromMultipart(body).tag[1].data`, raw); got != "b" {
		t.Errorf("tag[1].data = %q, want b", got)
	}
}

func TestFromMultipartErrors(t *testing.T) {
	cases := []struct {
		name string
		body any
		want string
	}{
		{"no boundary", map[string]any{
			"contentType": "multipart/form-data",
			"rawData":     multipartBody(),
		}, "boundary"},
		{"unparsable content type", map[string]any{
			"contentType": "multipart/form-data; boundary",
			"rawData":     multipartBody(),
		}, "content type"},
		{"not a raw body", "just a string", "raw-content body"},
		{"body never uses the boundary", map[string]any{
			"contentType": testContentType,
			"rawData":     `{"notMultipart": true}`,
		}, "delimiter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := CompileMessage(nil, `fromMultipart(body)`)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if _, err := prog.Eval(messageActivation(tc.body)); err == nil {
				t.Fatal("Eval succeeded, want an error")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestFromMultipartEmptyFormIsNotAnError(t *testing.T) {
	// A form with no fields still writes its closing delimiter. It is valid, and
	// must stay distinguishable from a body that is not multipart at all.
	raw := map[string]any{
		"contentType": testContentType,
		"rawData":     "--" + testBoundary + "--\r\n",
	}
	if got := evalWith(t, `size(fromMultipart(body)) == 0`, raw); got != true {
		t.Error("an empty multipart form did not decode to an empty parts map")
	}
}

func TestMultipartBuilder(t *testing.T) {
	// A scalar is shorthand for a text field; an object gives full control.
	expr := `toMultipart(
		multipart()
			.addPart("caption", "hello")
			.addPart("report", {"data": "a,b", "filename": "r.csv", "contentType": "text/csv"}),
		"` + testBoundary + `")`
	got := evalStringWith(t, expr, nil)

	for _, want := range []string{
		`name="caption"`,
		"hello",
		`name="report"; filename="r.csv"`,
		"Content-Type: text/csv",
		"a,b",
		"--" + testBoundary + "--",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered body missing %q:\n%s", want, got)
		}
	}
}

func TestAddPartDoesNotMutateItsReceiver(t *testing.T) {
	// CEL values are immutable and one expression may reference the same
	// intermediate twice. If addPart inserted in place, the first branch of this
	// comparison would see the part the second branch added.
	got := evalWith(t, `
		size(multipart().addPart("a", "1")) == 1
		&& size(multipart().addPart("a", "1").addPart("b", "2")) == 2`, nil)
	if got != true {
		t.Errorf("addPart mutated its receiver")
	}
}

func TestAddPartAcceptsADecodedPart(t *testing.T) {
	// Forwarding an upload is the motivating case: a part read off the wire is
	// already the shape addPart takes, filename and content type included.
	expr := `toMultipart(multipart().addPart("avatar", fromMultipart(body).avatar), "` + testBoundary + `")`
	got := evalStringWith(t, expr, rawBody())

	if !strings.Contains(got, `filename="`+testFileName+`"`) {
		t.Errorf("forwarded part lost its filename:\n%s", got)
	}
	if !strings.Contains(got, "Content-Type: "+testFileType) {
		t.Errorf("forwarded part lost its content type:\n%s", got)
	}
	if !strings.Contains(got, string(testFileBytes)) {
		t.Error("forwarded part did not decode back to the original bytes")
	}
}

func TestMultipartRoundTrip(t *testing.T) {
	// Decode, re-encode, decode again: the parts must be identical, which is what
	// makes "forward an upload untouched" a real guarantee rather than a hope.
	expr := `fromMultipart(toMultipart(fromMultipart(body), "` + testBoundary + `"), "` + testContentType + `")`

	if got := evalStringWith(t, expr+`.username.data`, rawBody()); got != "ann" {
		t.Errorf("round-tripped username.data = %q, want ann", got)
	}
	if got := evalStringWith(t, expr+`.avatar.filename`, rawBody()); got != testFileName {
		t.Errorf("round-tripped avatar.filename = %q, want %q", got, testFileName)
	}
	want := base64.StdEncoding.EncodeToString(testFileBytes)
	if got := evalStringWith(t, expr+`.avatar.data`, rawBody()); got != want {
		t.Errorf("round-tripped avatar.data = %q, want %q", got, want)
	}
}

func TestToMultipartDefaultBoundary(t *testing.T) {
	// The 1-arg form has to be deterministic: set-payload names the boundary in a
	// static contentType field, so a flow author has to be able to write it down.
	got := evalStringWith(t, `toMultipart(multipart().addPart("a", "1"))`, nil)
	if !strings.Contains(got, "--"+defaultMultipartBoundary) {
		t.Errorf("default boundary %q not used:\n%s", defaultMultipartBoundary, got)
	}
}

func TestToMultipartSortsPartNames(t *testing.T) {
	got := evalStringWith(t,
		`toMultipart(multipart().addPart("b", "2").addPart("a", "1"), "`+testBoundary+`")`, nil)
	if strings.Index(got, `name="a"`) > strings.Index(got, `name="b"`) {
		t.Errorf("parts are not sorted by name:\n%s", got)
	}
}

func TestToMultipartRejectsABadPart(t *testing.T) {
	prog, err := CompileMessage(nil, `toMultipart(multipart().addPart("a", {"encoding": "rot13"}))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := prog.Eval(messageActivation(nil)); err == nil {
		t.Fatal("Eval succeeded, want an error naming the bad encoding")
	}
}

// A part name or filename reaching the encoder is routinely attacker-controlled:
// a flow that forwards body.parts forwards names chosen by whoever uploaded them.
// CR or LF in one used to terminate the Content-Disposition line and turn a
// single part into several, with headers and a body of the attacker's choosing.
func TestEncodeRejectsHeaderInjection(t *testing.T) {
	smuggle := "\r\nContent-Type: text/html\r\n\r\n<script>alert(1)</script>\r\n--probe\r\n"
	cases := []struct {
		name  string
		parts map[string]any
	}{
		{"in a part name", map[string]any{"a" + smuggle: "v"}},
		{"in a filename", map[string]any{"f": map[string]any{
			"data": "v", "filename": "a" + smuggle}}},
		{"in a content type", map[string]any{"f": map[string]any{
			"data": "v", "contentType": "text/plain" + smuggle}}},
		{"in a residual header value", map[string]any{"f": map[string]any{
			"data": "v", "headers": map[string]any{"X-Note": "a" + smuggle}}}},
		{"in a residual header name", map[string]any{"f": map[string]any{
			"data": "v", "headers": map[string]any{"X-Note" + smuggle: "v"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := EncodeMultipart(tc.parts, "probe")
			if err == nil {
				t.Fatalf("encoded without error, producing:\n%s", out)
			}
			if !errors.Is(err, errCRLF) {
				t.Errorf("error %v does not report the CR/LF cause", err)
			}
			if strings.Contains(out, "<script>") {
				t.Error("attacker content reached the rendered body")
			}
		})
	}
}

// A part's own headers must not displace the name and filename it is being
// written under. residualHeaders drops both on the way in, so this can only
// arrive from a part built by hand -- which is exactly when it would be a way to
// claim a different identity than the one addPart was called with.
func TestResidualHeadersCannotOverridePromotedOnes(t *testing.T) {
	parts := map[string]any{"real": map[string]any{
		"data":        "v",
		"filename":    "real.txt",
		"contentType": "text/plain",
		"headers": map[string]any{
			"Content-Disposition": `form-data; name="spoofed"`,
			"Content-Type":        "text/html",
			// Lower case on purpose: Set writes in canonical form, so a guard that
			// compares the raw key lets this one through as "Content-Type".
			"content-type": "text/html",
			"X-Kept":       "yes",
		},
	}}
	out, err := EncodeMultipart(parts, "b")
	if err != nil {
		t.Fatalf("EncodeMultipart: %v", err)
	}
	if strings.Contains(out, "spoofed") {
		t.Errorf("a residual header displaced the part's identity:\n%s", out)
	}
	if !strings.Contains(out, `name="real"; filename="real.txt"`) {
		t.Errorf("the part lost its real identity:\n%s", out)
	}
	if !strings.Contains(out, "Content-Type: text/plain") || strings.Contains(out, "text/html") {
		t.Errorf("a residual header displaced the content type:\n%s", out)
	}
	if !strings.Contains(out, "X-Kept: yes") {
		t.Errorf("an ordinary residual header was dropped:\n%s", out)
	}
}

// The same guard, for a part that declares no content type of its own. Without
// canonical comparison the residual header is the only thing setting it, so the
// promoted value never gets a chance to win.
func TestResidualHeaderCannotSetAnAbsentContentType(t *testing.T) {
	parts := map[string]any{"f": map[string]any{
		"data":    "v",
		"headers": map[string]any{"content-type": "text/html"},
	}}
	out, err := EncodeMultipart(parts, "b")
	if err != nil {
		t.Fatalf("EncodeMultipart: %v", err)
	}
	if strings.Contains(out, "text/html") {
		t.Errorf("a residual header set the content type of a part that declared none:\n%s", out)
	}
}

func TestIsMultipart(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{testContentType, true},
		{"multipart/form-data", true},
		{"application/json", false},
		{"application/x-www-form-urlencoded", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsMultipart(tc.contentType); got != tc.want {
			t.Errorf("IsMultipart(%q) = %v, want %v", tc.contentType, got, tc.want)
		}
	}
}
