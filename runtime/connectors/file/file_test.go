package file

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// connectorName is the name blockDeps resolves the test connector under.
const connectorName = "files"

// startConnector starts a file connector rooted at dir and stops it on cleanup.
func startConnector(t *testing.T, dir string, settings types.Settings) *Connector {
	t.Helper()
	if settings == nil {
		settings = types.Settings{"root": dir}
	}
	conn := &Connector{}
	if err := conn.Start(context.Background(), types.ConnectorConfig{
		Name: connectorName, Type: connectorType, Settings: settings,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = conn.Stop(context.Background()) })
	return conn
}

// blockDeps returns BlockDeps resolving conn under connectorName, the way the
// runtime resolves a block's declared connector.
func blockDeps(conn *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == connectorName {
			return conn, true
		}
		return nil, false
	}}
}

// blockMessage builds a message carrying body.
func blockMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

// writeFile puts a file at a path relative to root, creating parents.
func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestConnectorRegistered(t *testing.T) {
	if _, err := core.DefaultRegistry().New(connectorType); err != nil {
		t.Fatalf("New(%q): %v", connectorType, err)
	}
	registered := core.DefaultBlockRegistry().Names()
	for _, block := range []string{readBlock, writeBlock} {
		if !slices.Contains(registered, block) {
			t.Errorf("block %q is not registered", block)
		}
	}
}

func TestConnectorStartRejectsBadRoots(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a-file", "x")

	tests := []struct {
		name     string
		settings types.Settings
		wantErr  string
	}{
		{name: "no root", settings: types.Settings{}, wantErr: "requires a root directory"},
		{name: "missing directory", settings: types.Settings{"root": filepath.Join(dir, "nope")}, wantErr: "no such file"},
		{name: "root is a file", settings: types.Settings{"root": filepath.Join(dir, "a-file")}, wantErr: "not a directory"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conn := &Connector{}
			err := conn.Start(context.Background(), types.ConnectorConfig{
				Name: connectorName, Type: connectorType, Settings: tc.settings,
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// TestConnectorStartResolvesRelativeRoot covers the reason Start calls Abs: a
// relative root would make resolve fold the "./" away, and every path under it
// would then read as an escape.
func TestConnectorStartResolvesRelativeRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hello.txt", "hi")
	t.Chdir(dir)

	conn := startConnector(t, ".", types.Settings{"root": "."})
	if !filepath.IsAbs(conn.root) {
		t.Errorf("root = %q, want an absolute path", conn.root)
	}
	data, err := conn.ReadFile("hello.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("ReadFile = %q, want %q", data, "hi")
	}
}

// TestReadFileRefusesEscapes is the containment contract. The lexical guard and
// os.Root answer different halves of it: "../secret" never names a path inside
// the root, while "link/secret" does — it only leaves once the kernel walks the
// symlink — so neither check alone is enough.
func TestReadFileRefusesEscapes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	writeFile(t, root, "inside.txt", "inside")
	writeFile(t, base, "secret", "top secret")
	writeFile(t, filepath.Join(base, "outside"), "secret", "top secret")

	if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(base, "secret"), filepath.Join(root, "direct-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	conn := startConnector(t, root, nil)
	tests := []struct {
		name string
		path string
		// wantErr asserts *why* the path failed. Without it a case can pass for
		// the wrong reason — an absolute path used to fail only because the
		// re-rooted file it named did not happen to exist.
		wantErr string
	}{
		{name: "parent traversal", path: "../secret", wantErr: "escapes the connector root"},
		{name: "traversal mid-path", path: "sub/../../secret", wantErr: "escapes the connector root"},
		{name: "absolute path", path: filepath.Join(base, "secret"), wantErr: "must be relative"},
		{name: "through a directory symlink", path: "link/secret", wantErr: "read file"},
		{name: "a file symlink out of the root", path: "direct-link", wantErr: "read file"},
		{name: "empty path", path: "", wantErr: "is empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := conn.ReadFile(tc.path)
			if err == nil {
				t.Fatalf("ReadFile(%q) succeeded, want an error", tc.path)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}

	// An absolute path is refused outright, not folded into the root: the file it
	// would have named there must not be reachable either.
	writeFile(t, root, "mirrored.txt", "mirrored")
	if _, err := conn.ReadFile(filepath.Join(root, "mirrored.txt")); err == nil {
		t.Error("an absolute path was re-rooted instead of refused")
	}

	// A ".." that stays inside the root is folded away, not refused.
	if _, err := conn.ReadFile("sub/../inside.txt"); err != nil {
		t.Errorf("ReadFile of a contained path: %v", err)
	}
}

func TestWriteFileRefusesEscapes(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	writeFile(t, root, "keep", "x")

	conn := startConnector(t, root, nil)
	if err := conn.WriteFile("../escaped.txt", []byte("nope")); err == nil {
		t.Fatal("WriteFile outside the root succeeded, want an error")
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.txt")); err == nil {
		t.Fatal("a file was written outside the root")
	}
}

func TestWriteFileCreateDirs(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		root := t.TempDir()
		conn := startConnector(t, root, nil)
		if err := conn.WriteFile("nested/deep/out.txt", []byte("x")); err == nil {
			t.Fatal("writing into a missing directory succeeded, want an error")
		}
	})

	t.Run("creates parents when set", func(t *testing.T) {
		root := t.TempDir()
		conn := startConnector(t, root, types.Settings{"root": root, "createDirs": true})
		if err := conn.WriteFile("nested/deep/out.txt", []byte("x")); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		data, err := conn.ReadFile("nested/deep/out.txt")
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(data) != "x" {
			t.Errorf("ReadFile = %q, want %q", data, "x")
		}
	})
}

func TestWriteFileReplaces(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "out.txt", "a much longer original")

	conn := startConnector(t, root, nil)
	if err := conn.WriteFile("out.txt", []byte("short")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, err := conn.ReadFile("out.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "short" {
		t.Errorf("ReadFile = %q, want %q — the previous contents were not truncated", data, "short")
	}
}

func TestConnectorNotStarted(t *testing.T) {
	conn := &Connector{}
	if _, err := conn.ReadFile("x"); err == nil {
		t.Error("ReadFile on an unstarted connector succeeded, want an error")
	}
	if err := conn.WriteFile("x", nil); err == nil {
		t.Error("WriteFile on an unstarted connector succeeded, want an error")
	}
}

func TestResolveConnectorErrors(t *testing.T) {
	conn := startConnector(t, t.TempDir(), nil)
	tests := []struct {
		name    string
		conn    string
		deps    core.BlockDeps
		wantErr string
	}{
		{name: "no name", conn: "", deps: blockDeps(conn), wantErr: "name is required"},
		{name: "no connectors available", conn: connectorName, deps: core.BlockDeps{}, wantErr: "no connectors are available"},
		{name: "not configured", conn: "other", deps: blockDeps(conn), wantErr: "is not configured"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveConnector(tc.conn, tc.deps); err == nil ||
				!strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
