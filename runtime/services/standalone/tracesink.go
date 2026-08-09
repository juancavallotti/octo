package standalone

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	// traceFilePerm keeps the trace file readable only by the user who ran the
	// runtime. With payload capture on it holds message bodies and request
	// headers, which is not something to leave world-readable in a working
	// directory.
	traceFilePerm = 0o600

	// traceWriteBuffer is how much is batched before a syscall. Records are
	// small and arrive in bursts, so this turns a block-by-block trickle into
	// one write per burst.
	traceWriteBuffer = 64 << 10

	// traceFileTimeLayout stamps the default filename. Sortable, no separators
	// that need quoting in a shell.
	traceFileTimeLayout = "20060102-150405"

	// traceDirPerm is the mode for a directory --traces-file asked for that does
	// not exist yet. It matches the file: nobody outside the owner needs in.
	traceDirPerm = 0o750
)

// defaultTraceFile is where traces go when nothing chose a path: the working
// directory, named for when the run started.
//
// The pid is in the name because the obvious thing to do with tracing is turn it
// on for two runs at once — an integration under test and the service it calls —
// and two runtimes started in the same directory in the same second would
// otherwise interleave their records into one file, which reads as a single
// impossible trace rather than as two.
func defaultTraceFile(now time.Time) string {
	return fmt.Sprintf("octo-trace-%s-%d.jsonl", now.Format(traceFileTimeLayout), os.Getpid())
}

// traceSink writes trace records to a file as JSON Lines: one object per line.
//
// Not a JSON array, which is the other obvious choice and the wrong one. A
// runtime is usually killed rather than stopped — Ctrl-C in a terminal, a pod
// evicted — and an array whose closing bracket was never written is not a
// document any parser will read, so the whole trace of the run is lost at exactly
// the moment it became interesting. JSONL truncated anywhere is still valid up to
// its last complete line. It is also what `tail -f | jq -c` wants, and it matches
// the one-JSON-record-per-publish shape the central log sink already uses.
type traceSink struct {
	file *os.File
	buf  *bufio.Writer
	enc  *json.Encoder
}

// newTraceSink opens path for appending, creating it if needed. An existing file
// is appended to rather than truncated: two runtimes pointed at one path by
// --traces-file have made their own bed, but a rerun that silently erased the
// trace you were about to read would be worse.
func newTraceSink(path string) (*traceSink, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, traceDirPerm); err != nil {
			return nil, fmt.Errorf("standalone: create trace directory %q: %w", dir, err)
		}
	}
	// The path comes from --traces-file (or this module's own default), which is
	// the operator's to choose: the runtime is being asked to write a file where
	// they said, exactly as --config reads one from where they said.
	//nolint:gosec // G304: operator-supplied path, which is the point of the flag
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, traceFilePerm)
	if err != nil {
		return nil, fmt.Errorf("standalone: open trace file %q: %w", path, err)
	}
	buf := bufio.NewWriterSize(file, traceWriteBuffer)
	return &traceSink{file: file, buf: buf, enc: json.NewEncoder(buf)}, nil
}

// Write appends one record. json.Encoder terminates each value with a newline,
// which is the whole of the JSONL format.
func (s *traceSink) Write(event types.TraceEvent) error {
	if err := s.enc.Encode(event); err != nil {
		return fmt.Errorf("standalone: encode trace record: %w", err)
	}
	return nil
}

// Flush pushes buffered records to the file. The publisher calls it whenever it
// runs out of queued work, so an idle runtime's trace is readable immediately
// rather than a buffer-full later — which is most of what makes this usable
// while a local run is still going.
func (s *traceSink) Flush() error {
	if err := s.buf.Flush(); err != nil {
		return fmt.Errorf("standalone: flush trace file: %w", err)
	}
	return nil
}

// Close flushes, syncs and closes.
//
// The sync is here and nowhere else. Per-record durability would put an fsync
// between every block and the next record, making the drain goroutine the
// slowest thing in the process and turning every burst into guaranteed drops —
// the loss the buffer exists to avoid, paid for a guarantee nobody asked tracing
// for. One sync on a graceful stop is what makes the file complete when the
// runtime was actually allowed to finish.
func (s *traceSink) Close() error {
	flushErr := s.Flush()
	if err := s.file.Sync(); err != nil && flushErr == nil {
		flushErr = fmt.Errorf("standalone: sync trace file: %w", err)
	}
	if err := s.file.Close(); err != nil && flushErr == nil {
		flushErr = fmt.Errorf("standalone: close trace file: %w", err)
	}
	return flushErr
}

// newTracePublisher builds the module's trace publisher: nothing at all when
// tracing is off, so a run that did not ask for traces never creates a file.
//
//nolint:ireturn // returns the TracePublisher interface Services.Traces exposes
func newTracePublisher(opts core.TraceOptions, now time.Time) (core.TracePublisher, error) {
	if !opts.Enabled {
		return core.NoopTracer(), nil
	}
	path := opts.File
	if path == "" {
		path = defaultTraceFile(now)
	}
	sink, err := newTraceSink(path)
	if err != nil {
		return nil, err
	}
	return core.NewBufferedTracer(sink, opts), nil
}
