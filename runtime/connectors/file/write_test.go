package file

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// writeProcess builds a file-write block over root and runs it against msg.
func writeProcess(t *testing.T, root string, settings types.Settings, msg *types.Message) (*types.Message, error) {
	t.Helper()
	conn := startConnector(t, root, types.Settings{"root": root, "createDirs": true})
	proc, err := newWrite(settings, blockDeps(conn))
	if err != nil {
		return nil, err
	}
	return proc.Process(context.Background(), msg)
}

// readBack returns the contents of a file written under root.
//
//nolint:gosec // G304: the path is this test's own t.TempDir plus a literal
func readBack(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func TestWriteContentExpression(t *testing.T) {
	root := t.TempDir()
	msg := blockMessage(t, map[string]any{"name": "billing"})

	out, err := writeProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"out/" + body.name + ".txt"`,
		"content":   `"hello " + body.name`,
	}, msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := readBack(t, root, "out/billing.txt"); got != "hello billing" {
		t.Errorf("file = %q, want %q", got, "hello billing")
	}
	// The block is a pass-through, so the body reaches the next block intact.
	if body, ok := out.Body.(map[string]any); !ok || body["name"] != "billing" {
		t.Errorf("body = %#v, want it unchanged", out.Body)
	}
}

// TestWriteDefaultsToTheBody covers the fallback order: raw content is written
// as-is, and a JSON body is written as JSON. Writing a raw body through the JSON
// encoder would add the quotes and escapes to the file.
func TestWriteDefaultsToTheBody(t *testing.T) {
	t.Run("raw content is written verbatim", func(t *testing.T) {
		root := t.TempDir()
		msg := blockMessage(t, nil)
		msg.SetRawBody("text/plain", "line one\nline two\n")

		if _, err := writeProcess(t, root, types.Settings{
			"connector": connectorName,
			"path":      `"out.txt"`,
		}, msg); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if got := readBack(t, root, "out.txt"); got != "line one\nline two\n" {
			t.Errorf("file = %q, want the raw content verbatim", got)
		}
	})

	t.Run("a JSON body is written as JSON", func(t *testing.T) {
		root := t.TempDir()
		msg := blockMessage(t, map[string]any{"a": float64(1)})

		if _, err := writeProcess(t, root, types.Settings{
			"connector": connectorName,
			"path":      `"out.json"`,
		}, msg); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if got := readBack(t, root, "out.json"); got != `{"a":1}` {
			t.Errorf("file = %q, want %q", got, `{"a":1}`)
		}
	})
}

// TestWriteBase64RoundTripsBinary is the pair to TestReadBase64: bytes that are
// not valid UTF-8 come back byte-for-byte when both blocks agree on base64.
func TestWriteBase64RoundTripsBinary(t *testing.T) {
	root := t.TempDir()
	binary := []byte{0xff, 0xfe, 0x00, 0x01}
	encoded := base64.StdEncoding.EncodeToString(binary)

	msg := blockMessage(t, nil)
	msg.SetRawBody("image/png", encoded)

	if _, err := writeProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"copy.png"`,
		"encoding":  "base64",
	}, msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := readBack(t, root, "copy.png"); got != string(binary) {
		t.Errorf("file = %x, want %x", got, binary)
	}
}

func TestWriteResultVar(t *testing.T) {
	root := t.TempDir()
	msg := blockMessage(t, nil)

	out, err := writeProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"receipt.txt"`,
		"content":   `"12345"`,
		"resultVar": "written",
	}, msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	receipt, ok := out.Variables["written"].(map[string]any)
	if !ok {
		t.Fatalf("vars.written = %#v, want a map", out.Variables["written"])
	}
	if receipt[pathKey] != "receipt.txt" {
		t.Errorf("path = %v, want receipt.txt", receipt[pathKey])
	}
	if receipt[bytesWrittenKey] != 5 {
		t.Errorf("bytes = %v, want 5", receipt[bytesWrittenKey])
	}
}

func TestWriteRejectsBadSettings(t *testing.T) {
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
			name:     "malformed content expression",
			settings: types.Settings{"connector": connectorName, "path": `"x"`, "content": "body."},
			wantErr:  "ERROR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := startConnector(t, root, nil)
			_, err := newWrite(tc.settings, blockDeps(conn))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestWriteRejectsMalformedBase64(t *testing.T) {
	root := t.TempDir()
	msg := blockMessage(t, nil)

	_, err := writeProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      `"out.bin"`,
		"content":   `"not base64!"`,
		"encoding":  "base64",
	}, msg)
	if err == nil || !strings.Contains(err.Error(), "not base64") {
		t.Fatalf("err = %v, want it to name the bad encoding", err)
	}
}

// TestWritePathEscapeFailsTheMessage is the block-level half of the containment
// contract: a path chosen per message is exactly the one an agent tool supplies.
func TestWritePathEscapeFailsTheMessage(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	msg := blockMessage(t, map[string]any{"path": "../escaped.txt"})

	_, err := writeProcess(t, root, types.Settings{
		"connector": connectorName,
		"path":      "body.path",
		"content":   `"nope"`,
	}, msg)
	if err == nil {
		t.Fatal("writing outside the root succeeded, want an error")
	}
	if _, statErr := os.Stat(filepath.Join(base, "escaped.txt")); statErr == nil {
		t.Fatal("a file was written outside the root")
	}
}
