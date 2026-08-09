package standalone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// closeTracer drains and closes a publisher the module built, which is what
// Services.Close does in a real run.
func closeTracer(t *testing.T, publisher core.TracePublisher) {
	t.Helper()
	closer, ok := publisher.(interface{ Close() error })
	if !ok {
		t.Fatalf("publisher %T cannot be closed", publisher)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Disabled tracing must leave nothing behind. A run that did not ask for traces
// finding a file in its working directory afterwards is the kind of surprise
// that gets a feature turned off for good.
func TestTracingDisabledCreatesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "should-not-exist.jsonl")

	publisher, err := newTracePublisher(core.TraceOptions{Enabled: false, File: path}, time.Now())
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	if publisher.Enabled() {
		t.Fatal("a disabled module returned an enabled publisher")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled tracing created %q", path)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("disabled tracing left %d files behind", len(entries))
	}
}

func TestTraceSinkWritesOneJSONObjectPerLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")

	publisher, err := newTracePublisher(core.TraceOptions{Enabled: true, File: path}, time.Now())
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	publisher.Publish(types.TraceEvent{Kind: types.TraceFlowStarted, Flow: "orders", TraceID: "t1"})
	publisher.Publish(types.TraceEvent{
		Kind: types.TraceBlockPostInvoke, Flow: "orders", Path: "orders.charge", TraceID: "t1",
	})
	publisher.Publish(types.TraceEvent{Kind: types.TraceFlowCompleted, Flow: "orders", TraceID: "t1"})
	closeTracer(t, publisher)

	raw, err := os.ReadFile(path) //nolint:gosec // the path is this test's own temp dir
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3:\n%s", len(lines), raw)
	}

	// Every line must parse on its own. That is the whole point of the format:
	// a file cut short anywhere is still readable up to its last full line.
	kinds := make([]types.TraceKind, 0, len(lines))
	for i, line := range lines {
		var record types.TraceEvent
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\n%s", i, err, line)
		}
		if record.TraceID != "t1" {
			t.Fatalf("line %d TraceID = %q, want t1", i, record.TraceID)
		}
		if record.Seq != uint64(i+1) {
			t.Fatalf("line %d Seq = %d, want %d", i, record.Seq, i+1)
		}
		kinds = append(kinds, record.Kind)
	}
	want := []types.TraceKind{types.TraceFlowStarted, types.TraceBlockPostInvoke, types.TraceFlowCompleted}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", kinds, want)
		}
	}
}

// The trace file holds message payloads and request headers when payload capture
// is on, so it is not something to leave world-readable in a working directory.
func TestTraceFileIsNotWorldReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")

	publisher, err := newTracePublisher(core.TraceOptions{Enabled: true, File: path}, time.Now())
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	closeTracer(t, publisher)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != traceFilePerm {
		t.Fatalf("trace file mode = %#o, want %#o", perm, traceFilePerm)
	}
}

// The mode passed to OpenFile applies only when it creates the file, and the
// sink deliberately appends to an existing one. So a path that was already
// readable by others has to be narrowed on the way in, or the guarantee above
// holds only for the first run against that path.
func TestTraceFileTightensAnExistingPermissiveFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	// A permissive file is the whole premise: the test exists to prove the sink
	// narrows one.
	//nolint:gosec // G306: deliberately world-readable, which is what is being fixed
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("seed the file: %v", err)
	}

	publisher, err := newTracePublisher(core.TraceOptions{Enabled: true, File: path}, time.Now())
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	closeTracer(t, publisher)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != traceFilePerm {
		t.Fatalf("existing trace file mode = %#o, want it narrowed to %#o", perm, traceFilePerm)
	}
}

// A named pipe is a legitimate --traces-file: it is how someone watches a run
// live without a file to clean up afterwards. Records must reach it, its
// permissions are not the runtime's to rewrite, and closing it must report no
// error — there is no disk behind a pipe to sync to, and Linux answers fsync on
// one with EINVAL where macOS quietly succeeds. closeTracer is what asserts that.
func TestTraceSinkWritesToANonRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}

	// A fifo blocks on open until a reader arrives, so hold one for the test.
	opened := make(chan *os.File, 1)
	go func() {
		//nolint:gosec // G304: the path is this test's own t.TempDir fifo
		reader, openErr := os.OpenFile(path, os.O_RDONLY, 0)
		if openErr != nil {
			opened <- nil
			return
		}
		opened <- reader
	}()

	publisher, err := newTracePublisher(core.TraceOptions{Enabled: true, File: path}, time.Now())
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}
	publisher.Publish(types.TraceEvent{Kind: types.TraceFlowStarted})
	closeTracer(t, publisher)

	reader := <-opened
	if reader == nil {
		t.Fatal("could not open the fifo for reading")
	}
	defer func() { _ = reader.Close() }()
}

// TestTraceSinkSyncsOnlyRegularFiles pins the mechanism the test above depends on,
// on every platform rather than only the one whose fsync refuses a pipe: the sink
// decides what it may sync when it opens the file, not by trying and seeing.
func TestTraceSinkSyncsOnlyRegularFiles(t *testing.T) {
	dir := t.TempDir()

	sink, err := newTraceSink(filepath.Join(dir, "trace.jsonl"))
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if !sink.regular {
		t.Error("an ordinary trace file was not treated as syncable")
	}
	if err := sink.Close(); err != nil {
		t.Errorf("close: %v", err)
	}

	fifo := filepath.Join(dir, "trace.fifo")
	if mkErr := syscall.Mkfifo(fifo, 0o600); mkErr != nil {
		t.Skipf("mkfifo unavailable: %v", mkErr)
	}
	opened := make(chan *os.File, 1)
	go func() {
		//nolint:gosec // G304: the path is this test's own t.TempDir fifo
		reader, openErr := os.OpenFile(fifo, os.O_RDONLY, 0)
		opened <- reader
		_ = openErr
	}()

	pipeSink, err := newTraceSink(fifo)
	if err != nil {
		t.Fatalf("new fifo sink: %v", err)
	}
	if pipeSink.regular {
		t.Error("a fifo was treated as syncable, which fsync refuses on Linux")
	}
	if err := pipeSink.Close(); err != nil {
		t.Errorf("close fifo sink: %v", err)
	}

	if reader := <-opened; reader != nil {
		_ = reader.Close()
	}
}

// An empty File is the module's cue to name the file itself: the format and the
// location are its business, not the shared service's.
func TestDefaultTraceFileNamesTheRun(t *testing.T) {
	at := time.Date(2026, 8, 8, 14, 30, 22, 0, time.UTC)
	name := defaultTraceFile(at)

	if !strings.HasPrefix(name, "octo-trace-20260808-143022-") {
		t.Fatalf("default name = %q, want it stamped with the start time", name)
	}
	if !strings.HasSuffix(name, ".jsonl") {
		t.Fatalf("default name = %q, want a .jsonl suffix", name)
	}
	// The pid is what keeps two runtimes started in one directory in the same
	// second from interleaving into a single impossible trace.
	if !strings.Contains(name, "-"+strconv.Itoa(os.Getpid())+".") {
		t.Fatalf("default name = %q, want it to carry the pid", name)
	}
}

// A file that cannot be opened is an error the module reports rather than a
// panic or a silent no-op; New turns it into a logged degrade.
func TestTraceSinkReportsAnUnopenableFile(t *testing.T) {
	// A path under a regular file cannot be a directory, so neither MkdirAll nor
	// the open can succeed.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	_, err := newTracePublisher(
		core.TraceOptions{Enabled: true, File: filepath.Join(blocker, "trace.jsonl")}, time.Now(),
	)
	if err == nil {
		t.Fatal("opening a trace file under a regular file succeeded")
	}
}

// Close must leave a complete file: whatever was still buffered when the runtime
// stopped is exactly what someone is about to go and read.
func TestTraceSinkCloseFlushes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	sink, err := newTraceSink(path)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	if err := sink.Write(types.TraceEvent{Kind: types.TraceFlowStarted}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	raw, err := os.ReadFile(path) //nolint:gosec // the path is this test's own temp dir
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("Close left the buffered record unwritten")
	}
}
