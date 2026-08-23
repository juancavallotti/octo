package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

const testBoundary = "sourceboundary"

// multipartRequestBody is a two-part payload whose file part holds bytes that are
// not valid UTF-8 — the case the base64 rule exists for.
func multipartRequestBody() string {
	return "--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"username\"\r\n\r\n" +
		"ann\r\n" +
		"--" + testBoundary + "\r\n" +
		"Content-Disposition: form-data; name=\"avatar\"; filename=\"photo.png\"\r\n" +
		"Content-Type: image/png\r\n\r\n" +
		"\x89PNG\r\n\x1a\n\xff\r\n" +
		"--" + testBoundary + "--\r\n"
}

func multipartRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/upload", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+testBoundary)
	return req
}

// rawBodyOf asserts the message is in raw-content mode and returns its body map.
func rawBodyOf(t *testing.T, msg *types.Message) map[string]any {
	t.Helper()
	if !msg.RawContent {
		t.Fatal("message is not in raw-content mode")
	}
	body, ok := msg.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body is %T, want map[string]any", msg.Body)
	}
	return body
}

func partOf(t *testing.T, msg *types.Message, name string) map[string]any {
	t.Helper()
	parts, ok := rawBodyOf(t, msg)[types.RawPartsKey].(map[string]any)
	if !ok {
		t.Fatal("body carries no parts map")
	}
	part, ok := parts[name].(map[string]any)
	if !ok {
		t.Fatalf("no part named %q", name)
	}
	return part
}

func TestSourceDecodesMultipart(t *testing.T) {
	body := multipartRequestBody()
	src := &source{maxBody: defaultMaxBodyBytes}

	msg, err := src.buildMessage(multipartRequest(t, body), []byte(body))
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	// The property everything else rests on: rawData is exactly what arrived, so a
	// signature computed over the request body still verifies.
	_, rawData, ok := msg.RawBody()
	if !ok {
		t.Fatal("RawBody ok = false, want true")
	}
	if rawData != body {
		t.Error("rawData is not byte-for-byte the request body")
	}

	if got := partOf(t, msg, "username")["data"]; got != "ann" {
		t.Errorf("username.data = %v, want ann", got)
	}
	avatar := partOf(t, msg, "avatar")
	if got := avatar["filename"]; got != "photo.png" {
		t.Errorf("avatar.filename = %v, want photo.png", got)
	}
	if got := avatar["contentType"]; got != "image/png" {
		t.Errorf("avatar.contentType = %v, want image/png", got)
	}
	if got := avatar["encoding"]; got != "base64" {
		t.Errorf("avatar.encoding = %v, want base64", got)
	}
}

func TestSourceDecodesMultipartInRawMode(t *testing.T) {
	// rawBody is not an opt-out. The two paths converge: raw mode keeps its exact
	// bytes and gains parts, so a flow verifying a signature and a flow reading a
	// part are the same flow.
	body := multipartRequestBody()
	src := &source{maxBody: defaultMaxBodyBytes, rawBody: true}

	msg, err := src.buildMessage(multipartRequest(t, body), []byte(body))
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if _, rawData, _ := msg.RawBody(); rawData != body {
		t.Error("raw mode did not preserve the exact body")
	}
	if got := partOf(t, msg, "username")["data"]; got != "ann" {
		t.Errorf("username.data = %v, want ann", got)
	}
}

func TestSourceRejectsMalformedMultipart(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
	}{
		{"no boundary", "multipart/form-data", multipartRequestBody()},
		{"body is not multipart at all", "multipart/form-data; boundary=" + testBoundary, "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/upload",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			src := &source{maxBody: defaultMaxBodyBytes}

			_, err := src.buildMessage(req, []byte(tc.body))
			if err == nil {
				t.Fatal("buildMessage succeeded, want an error")
			}
			// The sentinel is what turns this into a 400 rather than a 500.
			if !errors.Is(err, errBadMultipart) {
				t.Errorf("error %v does not wrap errBadMultipart", err)
			}
		})
	}
}

func TestReadBodyLetsMultipartPastTheJSONGate(t *testing.T) {
	// Without this a multipart request never reaches a flow: it is not JSON, so
	// the gate answers 400 before buildMessage ever runs.
	body := multipartRequestBody()
	src := &source{maxBody: defaultMaxBodyBytes}

	raw, status, ok := src.readBody(httptest.NewRecorder(), multipartRequest(t, body))
	if !ok {
		t.Fatalf("readBody rejected a multipart body with status %d", status)
	}
	if string(raw) != body {
		t.Error("readBody altered the body")
	}
}

func TestSourceLeavesNonMultipartAlone(t *testing.T) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/orders",
		strings.NewReader(`{"id":1}`))
	req.Header.Set("Content-Type", "application/json")
	src := &source{maxBody: defaultMaxBodyBytes}

	msg, err := src.buildMessage(req, []byte(`{"id":1}`))
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if msg.RawContent {
		t.Error("a JSON request became a raw-content body")
	}
	body, ok := msg.Body.(map[string]any)
	if !ok {
		t.Fatalf("Body is %T, want map[string]any", msg.Body)
	}
	if body["id"] != float64(1) {
		t.Errorf("body.id = %v, want 1", body["id"])
	}
}
