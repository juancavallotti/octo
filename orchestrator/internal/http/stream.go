package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// streamChunk is the copy buffer for a streamed response. Small on purpose: the
// point is to forward whatever has arrived, not to wait for a buffer to fill.
const streamChunk = 4096

// StreamText copies stream to the client as chunked plain text, flushing each chunk
// so lines arrive as they are produced.
//
// This is the shape of every log tail the orchestrator serves, and it is shared
// because four of its details are easy to get subtly wrong and would drift between
// copies: the header is flushed up front (a follow stream can sit idle before its
// first line, and net/http or a proxy would otherwise withhold the 200 until then,
// leaving the client unable to tell "connected, waiting" from "hung"), every chunk
// is flushed (or a tail is held indefinitely), a failed write ends the copy silently
// (the client hung up, which is how a follow stream normally ends), and io.EOF and a
// cancelled request are both normal endings while anything else is logged under
// label.
//
// It writes the status line itself, so the caller must not have written one, and it
// does not close stream, because the caller owns it.
func StreamText(w http.ResponseWriter, r *http.Request, stream io.Reader, label string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no") // don't let a proxy buffer the tail
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	// Push the header out now, before the first byte of body, so an idle follow
	// stream still reaches the client as a live 200 rather than nothing at all.
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, streamChunk)
	for {
		n, rerr := stream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) && r.Context().Err() == nil {
				slog.Error(label, "error", rerr)
			}
			return
		}
	}
}
