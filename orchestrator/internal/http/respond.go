// Package httpx provides generic HTTP scaffolding shared by the orchestrator's
// feature handlers: JSON request/response helpers and a server constructor. It
// stays free of feature-specific types so handlers depend on it, not the other
// way round.
package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// maxRequestBytes caps request bodies so a malformed or hostile client cannot
// force unbounded reads.
const maxRequestBytes = 1 << 20 // 1 MiB

// DecodeJSON decodes the request body into dst, bounding its size and rejecting
// unknown fields so typos in client payloads surface as errors.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status/headers are already written, so we can only log.
		slog.Error("httpx: encode response", "error", err)
	}
}

// WriteError writes a JSON error envelope with the given status code.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}
