package httpclient

import (
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// captured is what the server saw, already parsed back out of the wire format.
type captured struct {
	contentType string
	parts       map[string]capturedPart
}

type capturedPart struct {
	filename    string
	contentType string
	data        string
}

// multipartServer parses whatever it is sent as multipart, so the assertions are
// about a request a real server can read rather than about our own rendering.
func multipartServer(t *testing.T, got *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.contentType = r.Header.Get("Content-Type")
		got.parts = map[string]capturedPart{}

		_, params, err := mime.ParseMediaType(got.contentType)
		if err != nil {
			t.Errorf("server could not parse Content-Type %q: %v", got.contentType, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			data, _ := io.ReadAll(part)
			got.parts[part.FormName()] = capturedPart{
				filename:    part.FileName(),
				contentType: part.Header.Get("Content-Type"),
				data:        string(data),
			}
			_ = part.Close()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok": true}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRESTSendsAMultipartBody(t *testing.T) {
	var got captured
	srv := multipartServer(t, &got)

	proc, err := newREST(types.Settings{
		"connector": "api",
		"method":    "POST",
		"path":      "/media",
		"bodyType":  "multipart",
		"body": `multipart()
			.addPart("caption", body.caption)
			.addPart("report", {"data": "a,b", "filename": "r.csv", "contentType": "text/csv"})`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	if _, err := proc.Process(context.Background(),
		restMessage(t, map[string]any{"caption": "hello"})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.HasPrefix(got.contentType, "multipart/form-data; boundary=") {
		t.Errorf("Content-Type = %q, want multipart/form-data with a boundary", got.contentType)
	}
	if p := got.parts["caption"]; p.data != "hello" {
		t.Errorf("caption = %q, want hello", p.data)
	}
	report := got.parts["report"]
	if report.filename != "r.csv" || report.contentType != "text/csv" || report.data != "a,b" {
		t.Errorf("report part = %+v, want r.csv/text/csv/a,b", report)
	}
}

func TestRESTForwardsADecodedUpload(t *testing.T) {
	// The motivating case: a part decoded by the http source is already the shape
	// the block sends, so forwarding an upload keeps its filename, its content
	// type, and its bytes -- including bytes that are not valid UTF-8.
	var got captured
	srv := multipartServer(t, &got)

	proc, err := newREST(types.Settings{
		"connector": "api",
		"method":    "POST",
		"path":      "/media",
		"bodyType":  "multipart",
		"body":      "body.parts",
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	binary := "\x89PNG\r\n\x1a\n\xff"
	msg := restMessage(t, map[string]any{
		"parts": map[string]any{
			"avatar": map[string]any{
				"data":        "iVBORw==",
				"encoding":    "base64",
				"filename":    "photo.png",
				"contentType": "image/png",
			},
		},
	})
	// Encode the real bytes so the assertion is about a true round trip.
	msg.Body.(map[string]any)["parts"].(map[string]any)["avatar"].(map[string]any)["data"] =
		base64Of(binary)

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	avatar := got.parts["avatar"]
	if avatar.filename != "photo.png" {
		t.Errorf("filename = %q, want photo.png", avatar.filename)
	}
	if avatar.contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", avatar.contentType)
	}
	if avatar.data != binary {
		t.Errorf("the forwarded bytes did not survive: got %q", avatar.data)
	}
}

func TestRESTMultipartOwnsItsContentTypeHeader(t *testing.T) {
	// The boundary is generated per request, so a Content-Type written by hand
	// cannot describe this body. Honouring it would send a request no server can
	// parse, so the block's own header has to win.
	var got captured
	srv := multipartServer(t, &got)

	proc, err := newREST(types.Settings{
		"connector": "api",
		"method":    "POST",
		"path":      "/media",
		"bodyType":  "multipart",
		"headers":   map[string]any{"Content-Type": `"multipart/form-data; boundary=wrong"`},
		"body":      `multipart().addPart("a", "1")`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if strings.Contains(got.contentType, "wrong") {
		t.Errorf("configured Content-Type overrode the generated boundary: %q", got.contentType)
	}
	if got.parts["a"].data != "1" {
		t.Error("the server could not read the body the block sent")
	}
}

func TestRESTRawBodyIsUnchanged(t *testing.T) {
	// The default has to stay exactly what it was.
	var gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotContentType, gotBody = r.Header.Get("Content-Type"), string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api", "method": "POST", "path": "/orders", "body": "body",
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	if _, err := proc.Process(context.Background(),
		restMessage(t, map[string]any{"item": "widget"})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.Contains(gotBody, `"item":"widget"`) {
		t.Errorf("body = %q, want the JSON-encoded message body", gotBody)
	}
}

func TestBodyTypeValidation(t *testing.T) {
	cases := []struct {
		name     string
		bodyType string
		wantErr  bool
	}{
		{"default", "", false},
		{"raw", "raw", false},
		{"multipart", "multipart", false},
		{"typo fails at startup", "multi-part", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newREST(types.Settings{
				"connector": "api", "path": "/x", "bodyType": tc.bodyType,
			}, restDeps(t, "http://example.invalid"))
			if tc.wantErr && err == nil {
				t.Error("newREST succeeded, want an error naming the bad bodyType")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("newREST: %v", err)
			}
		})
	}
}

func TestRESTMultipartRejectsANonPartsBody(t *testing.T) {
	proc, err := newREST(types.Settings{
		"connector": "api", "method": "POST", "path": "/media",
		"bodyType": "multipart", "body": `"just a string"`,
	}, restDeps(t, "http://example.invalid"))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	_, err = proc.Process(context.Background(), restMessage(t, map[string]any{}))
	if err == nil {
		t.Fatal("Process succeeded, want an error")
	}
	if !strings.Contains(err.Error(), "parts map") {
		t.Errorf("error %v does not explain what a multipart body must be", err)
	}
}

// base64Of is the encoding the parts shape uses for binary data.
func base64Of(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
