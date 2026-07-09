package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestStatusFor(t *testing.T) {
	tests := []struct {
		name string
		set  any
		want int
	}{
		{"absent", nil, http.StatusOK},
		{"valid float", float64(201), http.StatusCreated},
		{"valid int", 404, http.StatusNotFound},
		{"below range", float64(99), http.StatusOK},
		{"above range", float64(600), http.StatusOK},
		{"not a number", "nope", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := types.NewMessage("")
			if err != nil {
				t.Fatalf("NewMessage: %v", err)
			}
			if tt.set != nil {
				msg.Variables.Set(httpStatusVar, tt.set)
			}
			if got := statusFor(msg); got != tt.want {
				t.Errorf("statusFor = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWriteResultUsesHTTPStatusVar(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"ok": false}
	msg.Variables.Set(httpStatusVar, float64(http.StatusBadRequest))

	rec := httptest.NewRecorder()
	(&source{}).writeResult(rec, result{kind: types.FlowEventCompleted, msg: msg})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWriteResultPropagatesResponseHeaders(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"error": "unauthorized"}
	msg.Variables.Set(httpStatusVar, float64(http.StatusUnauthorized))
	msg.Variables.Set("WWW-Authenticate", `Bearer realm="x"`)
	msg.Variables.Set("X-Absent", "") // empty values are skipped
	msg.Variables.Set("X-Unlisted", "nope")
	msg.Variables.Set("Content-Type", "text/evil") // must not override the source's

	rec := httptest.NewRecorder()
	src := &source{respHeaders: []string{"WWW-Authenticate", "X-Absent", "Content-Type"}}
	src.writeResult(rec, result{kind: types.FlowEventCompleted, msg: msg})

	if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="x"` {
		t.Errorf("WWW-Authenticate = %q, want the challenge", got)
	}
	if _, ok := rec.Header()["X-Absent"]; ok {
		t.Error("empty-valued header should be skipped")
	}
	if _, ok := rec.Header()["X-Unlisted"]; ok {
		t.Error("unlisted header must not be propagated")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json (source-managed)", ct)
	}
}

// TestWriteResultPropagatesNonStringHeaders locks the fix for CEL-typed header
// values: a variable set to a number (as a CEL expression yields, e.g. an int64
// Retry-After) must still be emitted as a header rather than silently dropped.
func TestWriteResultPropagatesNonStringHeaders(t *testing.T) {
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = map[string]any{"error": "slow down"}
	msg.Variables.Set(httpStatusVar, float64(http.StatusTooManyRequests))
	msg.Variables.Set("Retry-After", int64(30)) // as CEL "30" would produce
	msg.Variables.Set("X-Ratio", float64(1.5))
	msg.Variables.Set("X-Flag", true)

	rec := httptest.NewRecorder()
	src := &source{respHeaders: []string{"Retry-After", "X-Ratio", "X-Flag"}}
	src.writeResult(rec, result{kind: types.FlowEventCompleted, msg: msg})

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want 30", got)
	}
	if got := rec.Header().Get("X-Ratio"); got != "1.5" {
		t.Errorf("X-Ratio = %q, want 1.5", got)
	}
	if got := rec.Header().Get("X-Flag"); got != "true" {
		t.Errorf("X-Flag = %q, want true", got)
	}
}

func TestHeaderValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
		ok   bool
	}{
		{"string", "abc", "abc", true},
		{"empty string", "", "", true},
		{"int", 7, "7", true},
		{"int64", int64(30), "30", true},
		{"integer float", float64(30), "30", true},
		{"fractional float", float64(1.5), "1.5", true},
		{"bool", true, "true", true},
		{"map is not a header", map[string]any{"a": 1}, "", false},
		{"slice is not a header", []any{1, 2}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := headerValue(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("value = %q, want %q", got, tt.want)
			}
		})
	}
}
