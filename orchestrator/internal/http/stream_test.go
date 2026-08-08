package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// chunkedReader yields one chunk per Read, so "a chunk arrived" and "the stream ended"
// are separate events — which is the whole thing a log tail has to get right.
type chunkedReader struct {
	chunks []string
	reads  int
	err    error
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.reads >= len(r.chunks) {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	n := copy(p, r.chunks[r.reads])
	r.reads++
	return n, nil
}

func TestStreamTextForwardsEveryChunk(t *testing.T) {
	stream := &chunkedReader{chunks: []string{"first line\n", "second line\n"}}
	w := httptest.NewRecorder()
	StreamText(w, httptest.NewRequest(http.MethodGet, "/logs", nil), stream, "test stream")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if got, want := w.Body.String(), "first line\nsecond line\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if !w.Flushed {
		t.Error("the response was never flushed; a proxy would hold the tail")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	if w.Header().Get("X-Accel-Buffering") != "no" {
		t.Error("X-Accel-Buffering is not disabled")
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Error("Cache-Control is unset; an intermediary may cache a log tail")
	}
}

// TestStreamTextFlushesHeaderBeforeAnyBody is the idle-follow case: a stream that has
// produced nothing yet must still flush its 200 and headers, or net/http (and any
// proxy) holds them back and the client sees no response at all until the first line.
func TestStreamTextFlushesHeaderBeforeAnyBody(t *testing.T) {
	stream := &chunkedReader{} // immediate EOF: nothing has been logged yet
	w := httptest.NewRecorder()
	StreamText(w, httptest.NewRequest(http.MethodGet, "/logs", nil), stream, "test stream")

	if !w.Flushed {
		t.Error("an idle stream never flushed its header; the client would see nothing")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty for an idle stream", w.Body.String())
	}
}

// failingWriter fails every write, standing in for a client that hung up mid-tail —
// which is how a follow stream normally ends.
type failingWriter struct{ header http.Header }

func (f *failingWriter) Header() http.Header {
	if f.header == nil {
		f.header = http.Header{}
	}
	return f.header
}
func (f *failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func (f *failingWriter) WriteHeader(int)           {}

// TestStreamTextStopsWhenTheClientGoesAway: the copy must end at the first failed write
// rather than reading the rest of the stream into a socket nobody is holding.
func TestStreamTextStopsWhenTheClientGoesAway(t *testing.T) {
	stream := &chunkedReader{chunks: []string{"one\n", "two\n", "three\n"}}
	StreamText(&failingWriter{}, httptest.NewRequest(http.MethodGet, "/logs", nil), stream, "test stream")

	if stream.reads != 1 {
		t.Errorf("read %d chunks after the write failed, want 1", stream.reads)
	}
}

// TestStreamTextEndsOnAnError: a mid-stream failure ends the response rather than
// truncating it into a partial body and then hanging.
func TestStreamTextEndsOnAnError(t *testing.T) {
	stream := &chunkedReader{chunks: []string{"partial\n"}, err: errors.New("stream broke")}
	w := httptest.NewRecorder()
	StreamText(w, httptest.NewRequest(http.MethodGet, "/logs", nil), stream, "test stream")

	if got := w.Body.String(); got != "partial\n" {
		t.Errorf("body = %q, want the chunk that did arrive", got)
	}
}
