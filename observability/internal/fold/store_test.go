package fold

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/juancavallotti/octo/observability/internal/ingest"
	"github.com/redis/go-redis/v9"
)

// storeFor returns a Store over a real Redis, or skips.
//
// A real server rather than a fake, because what is under test here is not the
// arithmetic — fold_test.go covers that against no dependency at all — but the two
// Lua scripts and their atomicity. A fake Redis would prove that the Go around
// them compiles.
func storeFor(t *testing.T, window time.Duration, maxBytes, minRun int) (*Store, *redis.Client) {
	t.Helper()
	url := os.Getenv("REDIS_TEST_URL")
	if url == "" {
		t.Skip("REDIS_TEST_URL is not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("REDIS_TEST_URL: %v", err)
	}
	client := redis.NewClient(opts)
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return NewStore(client, window, time.Minute, maxBytes, minRun), client
}

func mustAppend(t *testing.T, s *Store, r Record, now time.Time) []Record {
	t.Helper()
	out, err := s.Append(context.Background(), r, now)
	if err != nil {
		t.Fatalf("Append seq %d: %v", r.Record.Seq, err)
	}
	return out
}

func mustExpire(t *testing.T, s *Store, now time.Time) []Record {
	t.Helper()
	out, err := s.Expire(context.Background(), now, 100)
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	return out
}

// The shape of the whole thing: a run of frames goes in, one record comes out,
// and the text is the stream reassembled.
func TestStoreFoldsARunOnExpiry(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 4)
	now := base

	for i, word := range []string{"Checking ", "the ", "namespace", "."} {
		if held := mustAppend(t, s, frame(int64(i+1), time.Duration(i)*time.Millisecond, word), now); len(held) != 0 {
			t.Fatalf("seq %d returned %d records, want none while the run is open", i+1, len(held))
		}
	}

	// Nothing is due until the window has passed: a stream still producing frames
	// keeps its run open.
	if out := mustExpire(t, s, now); len(out) != 0 {
		t.Fatalf("expired %d runs before the window, want none", len(out))
	}

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 1 {
		t.Fatalf("expired %d records, want 1 folded run", len(out))
	}
	var body map[string]any
	if err := json.Unmarshal(out[0].Record.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["text"] != "Checking the namespace." {
		t.Errorf("text = %q, want the run merged", body["text"])
	}
}

// The records of a run too short to fold are still owed back. Nothing else stored
// them — Append holds every record it is given.
func TestStoreReturnsTheRecordsOfAShortRun(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 4)
	now := base

	mustAppend(t, s, frame(1, 0, "a"), now)
	mustAppend(t, s, frame(2, time.Millisecond, "b"), now)

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 2 {
		t.Fatalf("expired %d records, want both of a two-record run", len(out))
	}
	if out[0].Record.Seq != 1 || out[1].Record.Seq != 2 {
		t.Errorf("seqs = %d, %d — want them in order", out[0].Record.Seq, out[1].Record.Seq)
	}
	// Unfolded, so nothing was added to them.
	if len(out[0].Record.Attrs) != 0 {
		t.Errorf("attrs = %s, want an unfolded record left alone", out[0].Record.Attrs)
	}
}

// The boundary between an agent's thinking and its answer. The run in progress
// comes back the moment the next one starts, rather than waiting for the window.
func TestStoreClosesARunWhenTheShapeChanges(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 2)
	now := base

	for i, word := range []string{"think", "ing", " hard"} {
		mustAppend(t, s, frame(int64(i+1), time.Duration(i)*time.Millisecond, word), now)
	}

	answer, _ := json.Marshal(map[string]any{"text": "here", "type": "text", "index": 0})
	closed := mustAppend(t, s, rec(4, 3*time.Millisecond, string(answer)), now)

	if len(closed) != 1 {
		t.Fatalf("got %d records back, want the thinking run closed", len(closed))
	}
	var body map[string]any
	if err := json.Unmarshal(closed[0].Record.Body, &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["text"] != "thinking hard" {
		t.Errorf("text = %q, want only the thinking", body["text"])
	}
	if body["type"] != "thinking" {
		t.Errorf("type = %v, want the run's own", body["type"])
	}

	// And the answer is now the open run, on its own.
	rest := mustExpire(t, s, now.Add(2*time.Second))
	if len(rest) != 1 {
		t.Fatalf("expired %d records, want the answer's run", len(rest))
	}
}

// Two replicas sweeping at the same moment must not both write the same run. The
// pop is one script for exactly this.
func TestExpirePopsARunExactlyOnce(t *testing.T) {
	s, client := storeFor(t, time.Second, 1<<20, 2)
	now := base

	for i := range 6 {
		mustAppend(t, s, frame(int64(i+1), time.Duration(i)*time.Millisecond, "x"), now)
	}

	// A second Store over the same server is the other replica.
	other := NewStore(client, time.Second, time.Minute, 1<<20, 2)

	first := mustExpire(t, s, now.Add(2*time.Second))
	second := mustExpire(t, other, now.Add(2*time.Second))

	if len(first)+len(second) != 1 {
		t.Errorf("two sweeps produced %d records between them, want exactly 1",
			len(first)+len(second))
	}
}

// Records arrive out of order across replicas, so the text must be assembled by
// sequence rather than by arrival.
func TestStoreJoinsOutOfOrderArrivals(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 4)
	now := base

	mustAppend(t, s, frame(3, 2*time.Millisecond, "three "), now)
	mustAppend(t, s, frame(1, 0, "one "), now)
	mustAppend(t, s, frame(4, 3*time.Millisecond, "four"), now)
	mustAppend(t, s, frame(2, time.Millisecond, "two "), now)

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 1 {
		t.Fatalf("expired %d records, want 1", len(out))
	}
	var body map[string]any
	_ = json.Unmarshal(out[0].Record.Body, &body)
	if body["text"] != "one two three four" {
		t.Errorf("text = %q, want it joined by seq", body["text"])
	}
	// The run's end is the greatest stamp seen, not the last one to turn up.
	if !out[0].Record.Time.Equal(base.Add(3 * time.Millisecond)) {
		t.Errorf("ts = %v, want the latest frame's stamp", out[0].Record.Time)
	}
}

// The two block kinds alternate, so they have to accumulate as separate runs or
// nothing folds at all.
func TestStoreFoldsThePreAndPostRunsSeparately(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 3)
	now := base

	for i := range 4 {
		post := frame(int64(2*i+2), time.Duration(i)*time.Millisecond, "p")
		pre := frame(int64(2*i+1), time.Duration(i)*time.Millisecond, "p")
		pre.Record.Kind = ingest.KindBlockPreInvoke
		mustAppend(t, s, pre, now)
		mustAppend(t, s, post, now)
	}

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 2 {
		t.Fatalf("expired %d records, want one per kind", len(out))
	}
	kinds := map[string]bool{out[0].Record.Kind: true, out[1].Record.Kind: true}
	if !kinds[ingest.KindBlockPreInvoke] || !kinds[ingest.KindBlockPostInvoke] {
		t.Errorf("kinds = %v, want one fold of each", kinds)
	}
}

// Past the cap the text stops growing and the row says so.
func TestStoreCapsTheMergedText(t *testing.T) {
	s, _ := storeFor(t, time.Second, 8, 3)
	now := base

	for i, word := range []string{"12345", "67890", "abcde"} {
		mustAppend(t, s, frame(int64(i+1), time.Duration(i)*time.Millisecond, word), now)
	}

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 1 {
		t.Fatalf("expired %d records, want 1", len(out))
	}
	if !out[0].Record.Truncated {
		t.Error("want the folded record marked truncated")
	}
	var body map[string]any
	_ = json.Unmarshal(out[0].Record.Body, &body)
	if body["text"] != "12345" {
		t.Errorf("text = %q, want only what fit under the cap", body["text"])
	}
}

// Two traces streaming at once must not merge into each other.
func TestStoreKeepsTracesApart(t *testing.T) {
	s, _ := storeFor(t, time.Second, 1<<20, 2)
	now := base

	for i := range 3 {
		a := frame(int64(i+1), time.Duration(i)*time.Millisecond, "a")
		b := frame(int64(i+1), time.Duration(i)*time.Millisecond, "b")
		b.Record.TraceID = "tr-2"
		mustAppend(t, s, a, now)
		mustAppend(t, s, b, now)
	}

	out := mustExpire(t, s, now.Add(2*time.Second))
	if len(out) != 2 {
		t.Fatalf("expired %d records, want one per trace", len(out))
	}
	for _, r := range out {
		var body map[string]any
		_ = json.Unmarshal(r.Record.Body, &body)
		want := map[string]string{"tr-1": "aaa", "tr-2": "bbb"}[r.Record.TraceID]
		if body["text"] != want {
			t.Errorf("%s text = %q, want %q", r.Record.TraceID, body["text"], want)
		}
	}
}
