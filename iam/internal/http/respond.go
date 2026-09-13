// Package httpx provides generic HTTP scaffolding shared by the iam service's
// feature handlers: JSON request/response helpers and a server constructor. It
// stays free of feature-specific types so handlers depend on it, not the other
// way round.
//
// The error envelope is `{"error": "..."}`, the shape every service in this
// repository answers failures with; it is that shape, and not this code, that is
// the shared thing.
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

// ErrorResponse is the envelope WriteError produces, and so the failure body of
// every route that reports through it.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WriteError writes a JSON error envelope with the given status code.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, ErrorResponse{Error: msg})
}
