package standalone

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// reopen closes a store and returns a fresh one over the same directory, which is
// what "survives a restart" means for this module.
func reopen(t *testing.T, s *store, dir string) *store {
	t.Helper()
	s.close()
	return newStore(dir)
}

func TestPersistentNamespaceSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s := newStore(dir)
	if _, err := s.Set(ctx, core.NamespaceUser, "k", []byte("kept"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	s = reopen(t, s, dir)
	defer s.close()

	entry, ok, err := s.Get(ctx, core.NamespaceUser, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !ok {
		t.Fatal("the key did not survive a restart")
	}
	if string(entry.Value) != "kept" {
		t.Fatalf("value = %q, want \"kept\"", entry.Value)
	}
	// The version has to survive too: a reloaded entry whose version restarted at
	// zero would make every optimistic write conflict until someone wrote twice.
	if entry.Version != 1 {
		t.Fatalf("version = %d, want 1", entry.Version)
	}
}

func TestVolatileNamespaceDoesNotSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s := newStore(dir)
	if _, err := s.Set(ctx, core.NamespaceUserVolatile, "k", []byte("dropped"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	s = reopen(t, s, dir)
	defer s.close()

	if _, ok, _ := s.Get(ctx, core.NamespaceUserVolatile, "k"); ok {
		t.Fatal("a volatile key survived a restart")
	}
	assertNoFileMentions(t, dir, "dropped")
}

// TestSecretsAreNeverWrittenToDisk is the load-bearing one. The standalone module
// has no encryption key, so a secret that reached the storage directory would be a
// credential sitting in the working directory in cleartext. Asserting on the
// directory's contents rather than only on the read-back is deliberate: a refactor
// that started persisting secrets would still pass a read-back test.
func TestSecretsAreNeverWrittenToDisk(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	svc := New("", dir, core.TraceOptions{})
	if _, err := svc.Secrets().Set(ctx, core.NamespaceUser, "token", []byte("s3cr3t-refresh-token"), 0); err != nil {
		t.Fatalf("Secrets Set: %v", err)
	}
	if _, err := svc.Secrets().Set(ctx, core.NamespaceSystem, "token", []byte("s3cr3t-refresh-token"), 0); err != nil {
		t.Fatalf("Secrets Set: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	assertNoFileMentions(t, dir, "s3cr3t-refresh-token")
	for _, name := range []string{"user_secrets.gob", "system_secrets.gob"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Fatalf("%s was written; secret namespaces must stay in memory", name)
		}
	}

	// And it really is gone rather than merely unwritten.
	reopened := New("", dir, core.TraceOptions{})
	defer func() { _ = reopened.Close() }()
	if _, ok, _ := reopened.Secrets().Get(ctx, core.NamespaceUser, "token"); ok {
		t.Fatal("a secret survived a restart")
	}
}

func TestEmptyStorageDirKeepsEverythingInMemory(t *testing.T) {
	ctx := context.Background()
	s := newStore("")
	defer s.close()

	if _, err := s.Set(ctx, core.NamespaceUser, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if s.snap != nil {
		t.Fatal("an empty storage dir must not create a snapshot")
	}
	if s.persists(core.NamespaceUser) {
		t.Fatal("nothing persists without a storage dir")
	}
}

func TestDeletingTheLastKeyRemovesTheFile(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s := newStore(dir)
	if _, err := s.Set(ctx, core.NamespaceUser, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := s.Delete(ctx, core.NamespaceUser, "k", 0); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	s.close()

	if _, err := os.Stat(filepath.Join(dir, "user.gob")); err == nil {
		t.Fatal("an emptied namespace left its file behind")
	}
}

// TestCorruptNamespaceFileIsSkipped: a store is state a previous run left behind,
// so a file that will not decode must cost that namespace and nothing else — never
// the ability to start.
func TestCorruptNamespaceFileIsSkipped(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s := newStore(dir)
	if _, err := s.Set(ctx, core.NamespaceUser, "k", []byte("v"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := s.Set(ctx, core.NamespaceSystem, "k", []byte("intact"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	s.close()

	if err := os.WriteFile(filepath.Join(dir, "user.gob"), []byte("not gob at all"), filePerm); err != nil {
		t.Fatalf("corrupting the file: %v", err)
	}

	reopened := newStore(dir)
	defer reopened.close()

	if _, ok, _ := reopened.Get(ctx, core.NamespaceUser, "k"); ok {
		t.Fatal("the corrupt namespace should have been skipped")
	}
	entry, ok, _ := reopened.Get(ctx, core.NamespaceSystem, "k")
	if !ok || string(entry.Value) != "intact" {
		t.Fatalf("an unrelated namespace was lost with the corrupt one: %q ok=%v", entry.Value, ok)
	}
}

func TestBinaryValuesAndOddNamespacesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	// A namespace with characters a filename cannot hold verbatim, and a value that
	// is not valid UTF-8 — gob carries both, and the name escaping has to reverse.
	const odd = "tenant/acme:1"
	binary := []byte{0x00, 0xff, 0xfe, 0x10}

	s := newStore(dir)
	if _, err := s.Set(ctx, odd, "k", binary, 0); err != nil {
		t.Fatalf("Set: %v", err)
	}

	s = reopen(t, s, dir)
	defer s.close()

	entry, ok, _ := s.Get(ctx, odd, "k")
	if !ok {
		t.Fatalf("namespace %q did not round trip", odd)
	}
	if !bytes.Equal(entry.Value, binary) {
		t.Fatalf("value = %v, want %v", entry.Value, binary)
	}
}

func TestNameEncodingRoundTrips(t *testing.T) {
	for _, name := range []string{"user", "system_secrets", "a/b", "%", "", "üñî", "a b-c_d"} {
		if got := decodeName(encodeName(name)); got != name {
			t.Errorf("decodeName(encodeName(%q)) = %q", name, got)
		}
	}
}

// assertNoFileMentions fails when want appears in any file under dir, so a value
// that should never be written cannot slip through under an unexpected filename.
func assertNoFileMentions(t *testing.T, dir, want string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // a temp dir the test made
		if readErr != nil {
			t.Fatalf("reading %s: %v", entry.Name(), readErr)
		}
		if strings.Contains(string(body), want) {
			t.Fatalf("%s contains %q, which must never reach the disk", entry.Name(), want)
		}
	}
}
