package fold

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/ingest"
)

// A check against real records, exported from a production trace with:
//
//	psql -At -F'\t' -c "SELECT seq, extract(epoch from ts)*1000, duration_ns, body::text
//	  FROM traces WHERE trace_id=… AND block_type='sse-event' AND kind='block.post-invoke'
//	  ORDER BY seq" > frames.tsv
//
// It exists because every other test in this package uses records this package's
// own author invented, and the failure mode worth guarding against is the shape
// of the real thing differing from what was imagined about it. Skipped unless
// both a Redis and a file are given, so it is a tool rather than a gate.
func TestAgainstRealFrames(t *testing.T) {
	path := os.Getenv("FOLD_FRAMES_FILE")
	if path == "" {
		t.Skip("FOLD_FRAMES_FILE is not set")
	}
	s, _ := storeFor(t, time.Second, 1<<20, 4)

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open frames: %v", err)
	}
	defer func() { _ = f.Close() }()

	ctx := context.Background()
	now := base
	var closed []Record
	lines := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		cols := strings.SplitN(scanner.Text(), "\t", 4)
		if len(cols) != 4 {
			continue
		}
		// psql renders a NULL body as an empty field. That is a record the runtime
		// captured no payload for, which is nil rather than empty JSON.
		var body json.RawMessage
		if cols[3] != "" {
			body = json.RawMessage(cols[3])
		}
		lines++
		seq, _ := strconv.ParseInt(cols[0], 10, 64)
		ms, _ := strconv.ParseFloat(cols[1], 64)
		dur, _ := strconv.ParseInt(cols[2], 10, 64)

		out, err := s.Append(ctx, Record{Record: ingest.TraceRecord{
			TraceID:      "real",
			Seq:          seq,
			Kind:         ingest.KindBlockPostInvoke,
			Path:         "chat.dr-octo[events].sse-event",
			BlockType:    "sse-event",
			DeploymentID: "dep",
			Time:         time.UnixMilli(int64(ms)).UTC(),
			DurationNs:   dur,
			Body:         body,
		}}, now)
		if err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
		closed = append(closed, out...)
	}
	closed = append(closed, mustExpire(t, s, now.Add(2*time.Second))...)

	t.Logf("%d records -> %d rows", lines, len(closed))
	for i, r := range closed {
		var body map[string]any
		_ = json.Unmarshal(r.Record.Body, &body)
		text, _ := body["text"].(string)
		t.Logf("  row %d: type=%v count=%v len(text)=%d start=%q",
			i, body["type"], foldedCountOf(r), len(text), first(text, 60))
	}

	if len(closed) >= lines {
		t.Errorf("%d rows from %d records — folding achieved nothing", len(closed), lines)
	}
}

func foldedCountOf(r Record) any {
	var attrs map[string]any
	if json.Unmarshal(r.Record.Attrs, &attrs) != nil {
		return nil
	}
	folded, _ := attrs[AttrFolded].(map[string]any)
	return folded["count"]
}

func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
