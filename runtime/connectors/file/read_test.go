package file

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// readProcess builds a file-read block over root and runs it against body.
func readProcess(t *testing.T, root string, settings types.Settings, body any) (*types.Message, error) {
	t.Helper()
	conn := startConnector(t, root, nil)
	proc, err := newRead(settings, blockDeps(conn))
	if err != nil {
		return nil, err
	}
	return proc.Process(context.Background(), blockMessage(t, body))
}

func TestReadIntoBody(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.yaml", "name: billing\n")

	out, err := readProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"config.yaml"`,
	}, nil)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	contentType, rawData, ok := out.RawBody()
	if !ok {
		t.Fatalf("message is not in raw-content mode: %#v", out.Body)
	}
	if rawData != "name: billing\n" {
		t.Errorf("rawData = %q, want %q", rawData, "name: billing\n")
	}
	// The extension named the type, so nothing had to be configured.
	if contentType != "application/yaml" {
		t.Errorf("contentType = %q, want %q", contentType, "application/yaml")
	}
}

// TestReadContentTypeIsHostIndependent pins the formats a flow is most likely to
// read. mime.TypeByExtension is extended from the host's mime.types, so leaving
// these to it would let the same flow record different types on a laptop and in
// a container — and .yaml is absent from its built-in table entirely.
func TestReadContentTypeIsHostIndependent(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ file, want string }{
		{file: "a.yaml", want: "application/yaml"},
		{file: "a.yml", want: "application/yaml"},
		{file: "a.env", want: "text/plain; charset=utf-8"},
		{file: "a.csv", want: "text/csv; charset=utf-8"},
		{file: "a.md", want: "text/markdown; charset=utf-8"},
		{file: "a.json", want: "application/json"},
		// Case is not significant in an extension.
		{file: "a.YAML", want: "application/yaml"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			writeFile(t, root, tc.file, "x")
			out, err := readProcess(t, root, types.Settings{
				"connector": connectorName,
				"path":      `"` + tc.file + `"`,
			}, nil)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if contentType, _, _ := out.RawBody(); contentType != tc.want {
				t.Errorf("contentType = %q, want %q", contentType, tc.want)
			}
		})
	}
}

// TestReadPathIsPerMessage is the reason path is a CEL expression rather than a
// string: a flow used as an agent tool is told which file to read at call time.
func TestReadPathIsPerMessage(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "one.txt", "first")
	writeFile(t, root, "nested/two.txt", "second")

	for _, tc := range []struct{ path, want string }{
		{path: "one.txt", want: "first"},
		{path: "nested/two.txt", want: "second"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			out, err := readProcess(t, root, types.Settings{
				"connector": connectorName,
				"path":      "body.path",
			}, map[string]any{"path": tc.path})
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if _, rawData, _ := out.RawBody(); rawData != tc.want {
				t.Errorf("rawData = %q, want %q", rawData, tc.want)
			}
		})
	}
}

func TestReadIntoVariable(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "note.txt", "hello")

	body := map[string]any{"keep": "me"}
	out, err := readProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"note.txt"`,
		"resultVar": "contents",
	}, body)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, _ := out.Variables.String("contents"); got != "hello" {
		t.Errorf("vars.contents = %q, want %q", got, "hello")
	}
	if out.RawContent {
		t.Error("a result variable must leave the body alone, but raw content was set")
	}
	if got, ok := out.Body.(map[string]any); !ok || got["keep"] != "me" {
		t.Errorf("body = %#v, want it untouched", out.Body)
	}
}

// TestReadBase64 covers the binary path: a file whose bytes are not valid UTF-8
// only survives a JSON boundary base64-encoded, so that is what the setting is
// for. The bytes here are deliberately invalid UTF-8.
func TestReadBase64(t *testing.T) {
	root := t.TempDir()
	binary := string([]byte{0xff, 0xfe, 0x00, 0x01})
	writeFile(t, root, "image.png", binary)

	out, err := readProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"image.png"`,
		"encoding":  "base64",
	}, nil)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	_, rawData, ok := out.RawBody()
	if !ok {
		t.Fatalf("message is not in raw-content mode: %#v", out.Body)
	}
	if want := base64.StdEncoding.EncodeToString([]byte(binary)); rawData != want {
		t.Errorf("rawData = %q, want %q", rawData, want)
	}
}

func TestReadContentTypeOverride(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "data.bin", "x")

	tests := []struct {
		name     string
		settings types.Settings
		want     string
	}{
		{
			name: "explicit wins",
			settings: types.Settings{
				"connector": connectorName, "path": `"data.bin"`, "contentType": "text/csv",
			},
			want: "text/csv",
		},
		{
			name:     "unknown extension falls back",
			settings: types.Settings{"connector": connectorName, "path": `"data.bin"`},
			want:     defaultContentType,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, err := readProcess(t, root, tc.settings, nil)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if contentType, _, _ := out.RawBody(); contentType != tc.want {
				t.Errorf("contentType = %q, want %q", contentType, tc.want)
			}
		})
	}
}

func TestReadRejectsBadSettings(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name     string
		settings types.Settings
		wantErr  string
	}{
		{
			name:     "no path",
			settings: types.Settings{"connector": connectorName},
			wantErr:  "requires a path expression",
		},
		{
			name:     "no connector",
			settings: types.Settings{"path": `"x"`},
			wantErr:  "name is required",
		},
		{
			name:     "unknown encoding",
			settings: types.Settings{"connector": connectorName, "path": `"x"`, "encoding": "hex"},
			wantErr:  "encoding must be",
		},
		{
			name:     "malformed expression",
			settings: types.Settings{"connector": connectorName, "path": "body."},
			wantErr:  "ERROR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := startConnector(t, root, nil)
			_, err := newRead(tc.settings, blockDeps(conn))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestReadMissingFileFailsTheMessage(t *testing.T) {
	root := t.TempDir()
	_, err := readProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"absent.txt"`,
	}, nil)
	if err == nil {
		t.Fatal("reading a missing file succeeded, want an error")
	}
	if !strings.Contains(err.Error(), readBlock) {
		t.Errorf("err = %v, want it to name the block", err)
	}
}
