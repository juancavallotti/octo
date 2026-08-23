package standalone

import (
	"encoding/gob"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/juancavallotti/octo/runtime/core"
)

// snapshot serializes the standalone store to a directory, one file per namespace.
//
// One file per namespace rather than one file for everything: a namespace is the
// store's unit of partitioning, so a write touches exactly the namespace it changed
// and a file that fails to decode costs only that namespace. The encoding is gob —
// it is in the standard library, it round-trips arbitrary []byte values without the
// base64 inflation JSON would impose, and nothing outside this package reads these
// files.
//
// Nothing named "*_secrets" is ever handed to this type; the store keeps secret
// namespaces in memory. The reason lives with the store's persists predicate, but
// it bears repeating next to the code that actually creates files: the standalone
// module has no encryption key, so persisting a secret here would mean writing
// credentials to the working directory in cleartext.
type snapshot struct{ dir string }

// snapshotExt is the extension every namespace file carries, so a stray file in the
// storage directory is not mistaken for one.
const snapshotExt = ".gob"

// dirPerm and filePerm keep the store readable only by the user running the
// runtime. The values are not secrets, but they are application state that nothing
// else on the machine has a reason to read.
const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// newSnapshot returns a snapshot rooted at dir, or nil when dir is empty — the
// caller reads nil as "keep everything in memory".
func newSnapshot(dir string) *snapshot {
	if dir == "" {
		return nil
	}
	return &snapshot{dir: dir}
}

// load reads every namespace file in the directory. A file that cannot be read or
// decoded is logged and skipped rather than failing the load: the store is a cache
// of state a run left behind, and a corrupt one must not be the reason a runtime
// that was perfectly startable refuses to start. An absent directory is not an
// error — it is what the first run looks like.
func (s *snapshot) load() map[string]map[string]core.Entry {
	out := make(map[string]map[string]core.Entry)
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Error("standalone: reading the storage directory, starting with an empty store",
				"dir", s.dir, "error", err)
		}
		return out
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), snapshotExt) {
			continue
		}
		namespace := decodeName(strings.TrimSuffix(entry.Name(), snapshotExt))
		keys, readErr := s.read(filepath.Join(s.dir, entry.Name()))
		if readErr != nil {
			slog.Error("standalone: skipping an unreadable namespace file",
				"file", entry.Name(), "namespace", namespace, "error", readErr)
			continue
		}
		out[namespace] = keys
	}
	return out
}

// read decodes one namespace file.
func (s *snapshot) read(path string) (map[string]core.Entry, error) {
	f, err := os.Open(path) //nolint:gosec // the path is one this package wrote
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	keys := make(map[string]core.Entry)
	if err := gob.NewDecoder(f).Decode(&keys); err != nil {
		return nil, fmt.Errorf("decoding %s: %w", path, err)
	}
	return keys, nil
}

// write replaces the file for namespace with keys, or removes it when keys is
// empty so a namespace whose last key was deleted leaves nothing behind.
//
// The write goes to a temporary file in the same directory and is renamed over the
// target. Rename within a directory is atomic, so a process that dies mid-write
// leaves the previous good file rather than a half-encoded one. The Sync before it
// is what makes that promise survive a machine crash and not just a process one.
func (s *snapshot) write(namespace string, keys map[string]core.Entry) error {
	path := filepath.Join(s.dir, encodeName(namespace)+snapshotExt)
	if len(keys) == 0 {
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("removing %s: %w", path, err)
		}
		return s.syncDir()
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("creating %s: %w", s.dir, err)
	}

	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file in %s: %w", s.dir, err)
	}
	// Remove the temporary file on every path that does not rename it away.
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := writeSnapshotFile(tmp, keys); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return s.syncDir()
}

// syncDir flushes the directory itself, which is a separate thing from flushing
// the file. Sync on the temporary file persists its contents; the rename that puts
// it in place is a change to the *directory*, and until that is flushed a power
// loss can leave the directory entry pointing at the old file — or at a file that
// was deleted. Losing recent writes is the bargain this store already makes; a
// resurrected older namespace, whose versions have rewound under keys a caller
// already advanced, is not.
//
// A filesystem that refuses to sync a directory handle (Windows, and some network
// filesystems) reports EINVAL, ENOTSUP or EPERM. There is nothing to do about that
// and nothing gained by failing the write over it, so those are tolerated; any
// other error is a real one and is returned.
func (s *snapshot) syncDir() error {
	d, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("opening %s to flush it: %w", s.dir, err)
	}
	defer func() { _ = d.Close() }()

	if err := d.Sync(); err != nil && !unsupportedDirSync(err) {
		return fmt.Errorf("flushing %s: %w", s.dir, err)
	}
	return nil
}

// unsupportedDirSync reports whether an error means "this filesystem does not sync
// directories" rather than "the sync failed".
func unsupportedDirSync(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EPERM)
}

// writeSnapshotFile encodes keys into f, tightens its permissions and flushes it to
// disk. Split out so the caller's cleanup stays one deferred Remove.
func writeSnapshotFile(f *os.File, keys map[string]core.Entry) error {
	if err := gob.NewEncoder(f).Encode(keys); err != nil {
		return fmt.Errorf("encoding %s: %w", f.Name(), err)
	}
	// CreateTemp makes the file 0600 already; set it explicitly so the guarantee is
	// stated here rather than inherited from another package's choice.
	if err := f.Chmod(filePerm); err != nil {
		return fmt.Errorf("setting permissions on %s: %w", f.Name(), err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("flushing %s: %w", f.Name(), err)
	}
	return nil
}

// encodeName turns a namespace into a filename. The namespaces in practice are
// plain identifiers ("user", "system"), and those pass through unchanged so the
// directory is readable at a glance; anything else is percent-escaped byte by byte,
// which keeps the mapping reversible for arbitrary namespaces.
func encodeName(namespace string) string {
	var b strings.Builder
	for i := range len(namespace) {
		c := namespace[i]
		if isPlainNameByte(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%")
		const hex = "0123456789ABCDEF"
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0F])
	}
	return b.String()
}

// decodeName reverses encodeName. An escape that does not parse is left verbatim:
// the name is only used for logging and for the in-memory key, so a mangled
// filename should surface as an odd namespace rather than as a dropped one.
func decodeName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] != '%' || i+2 >= len(name) {
			b.WriteByte(name[i])
			continue
		}
		v, err := strconv.ParseUint(name[i+1:i+3], 16, 8)
		if err != nil {
			b.WriteByte(name[i])
			continue
		}
		b.WriteByte(byte(v))
		i += 2
	}
	return b.String()
}

// isPlainNameByte reports whether a byte may appear in a filename unescaped.
func isPlainNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-':
		return true
	default:
		return false
	}
}
